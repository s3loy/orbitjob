//go:build integration

package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"reflect"
	"strconv"
	"testing"
	"time"

	"orbitjob/internal/admin/bootstrap"
	"orbitjob/internal/core/domain/check"
	"orbitjob/internal/platform/scan"
)

func TestMonitoringHTTPAuditUsesAuthenticatedActor(t *testing.T) {
	for _, kind := range []string{"user", "api_key"} {
		t.Run(kind, func(t *testing.T) {
			server, db, bootstrapKey, stores := newIntegrationServerWithIdentity(t, kind == "user")
			defer server.Close()
			client := server.Client()
			var actorID, apiKey, csrf string
			spoofedActor := scan.GenerateID()
			request := func(method, path string, body any) *http.Response {
				t.Helper()
				var raw []byte
				if body != nil {
					if object, ok := body.(map[string]any); ok {
						object["actor_id"], object["actor_type"] = spoofedActor, "system"
					}
					var err error
					raw, err = json.Marshal(body)
					if err != nil {
						t.Fatal(err)
					}
				}
				req, err := http.NewRequest(method, server.URL+"/api/v1"+path, bytes.NewReader(raw))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Content-Type", "application/json")
				if kind == "user" {
					req.Header.Set("Origin", server.URL)
					if csrf != "" {
						req.Header.Set("X-CSRF-Token", csrf)
					}
				} else {
					req.Header.Set("Authorization", "Bearer "+apiKey)
				}
				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				return resp
			}
			if kind == "user" {
				jar, err := cookiejar.New(nil)
				if err != nil {
					t.Fatal(err)
				}
				client = &http.Client{Jar: jar}
				capability, err := stores.accounts.IssueInstallationCapability(t.Context(), "setup", "", 15*time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				resp := request("POST", "/auth/setup", SetupAccountRequest{Token: capability.Token,
					Username: "monitoring-auditor", Password: "Monitoring account passphrase 42", DisplayName: "Monitoring Auditor"})
				requireStatus(t, resp, http.StatusOK)
				var session SessionSnapshot
				decodeJSON(t, resp, &session)
				if session.User == nil || session.CSRFToken == "" {
					t.Fatal("setup did not establish human identity")
				}
				actorID, csrf = session.User.ID, session.CSRFToken
			} else {
				actorID, apiKey = createKey(t, server, bootstrapKey, bootstrap.DefaultTenantID, []string{tenantAdminPolicyID}, "")
			}
			selector := "?tenant_id=" + bootstrap.DefaultTenantID
			create := func(path string, body map[string]any) int64 {
				t.Helper()
				resp := request("POST", path+selector, body)
				requireStatus(t, resp, http.StatusCreated)
				var created struct {
					ID int64 `json:"id"`
				}
				decodeJSON(t, resp, &created)
				if created.ID <= 0 {
					t.Fatal("creation did not return identity")
				}
				return created.ID
			}
			checkID := create("/checks", map[string]any{"name": "audit-health", "check_type": "http_health",
				"check_config":    map[string]any{"url": "http://localhost/healthz", "headers": map[string]any{"Authorization": "private-check-token"}},
				"assertion_rules": []map[string]any{{"metric": "status_code", "operator": "==", "threshold": 200, "severity": "warning"}},
				"cron_expr":       "*/5 * * * *"})
			sliID := create("/slis", map[string]any{"name": "audit-availability", "sli_type": "availability", "source_type": "job_run",
				"source_config": map[string]any{"source_uid": check.CheckSourceUID(checkID)}, "aggregation": "ratio",
				"good_event_criteria": map[string]any{"status": "success"}})
			sloID := create("/slos", map[string]any{"name": "audit-objective", "sli_id": sliID, "target": .99,
				"window_type": "rolling", "window_duration": "1h"})
			for _, resource := range []struct {
				collection string
				id         int64
			}{{"checks", checkID}, {"slos", sloID}} {
				for version, action := range []string{"pause", "resume"} {
					resp := request("POST", fmt.Sprintf("/%s/%d/%s%s", resource.collection, resource.id, action, selector), map[string]any{"version": version + 1})
					requireStatus(t, resp, http.StatusOK)
					resp.Body.Close()
				}
			}
			for _, resource := range []struct {
				collection string
				id         int64
				version    int
			}{{"slos", sloID, 3}, {"slis", sliID, 1}, {"checks", checkID, 3}} {
				resp := request("DELETE", fmt.Sprintf("/%s/%d%s", resource.collection, resource.id, selector), map[string]any{"version": resource.version})
				requireStatus(t, resp, http.StatusOK)
				resp.Body.Close()
			}
			rows, err := db.QueryContext(t.Context(), `SELECT actor_type,actor_id,event_type,resource_type,resource_id,diff FROM audit_events WHERE tenant_id=$1 AND resource_type IN ('check','sli','slo') ORDER BY id`, bootstrap.DefaultTenantID)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			events := map[string]int{}
			ids := map[string]int64{"check": checkID, "sli": sliID, "slo": sloID}
			for rows.Next() {
				var actorType, writtenID, event, resourceType, resourceID string
				var diff []byte
				if err := rows.Scan(&actorType, &writtenID, &event, &resourceType, &resourceID, &diff); err != nil {
					t.Fatal(err)
				}
				if actorType != kind || writtenID != actorID || resourceID != strconv.FormatInt(ids[resourceType], 10) {
					t.Fatalf("audit identity for %s = %s/%s/%s, expected %s/%s/%d", event, actorType, writtenID, resourceID, kind, actorID, ids[resourceType])
				}
				if bytes.Contains(diff, []byte("private-check-token")) || bytes.Contains(diff, []byte(spoofedActor)) {
					t.Fatalf("audit contains private or untrusted value: %s", diff)
				}
				events[event]++
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			want := map[string]int{"check.created": 1, "check.status_changed": 2, "check.deleted": 1,
				"sli.created": 1, "sli.deleted": 1, "slo.created": 1, "slo.status_changed": 2, "slo.deleted": 1}
			if !reflect.DeepEqual(events, want) {
				t.Fatalf("audit events = %v, want %v", events, want)
			}
		})
	}
}
