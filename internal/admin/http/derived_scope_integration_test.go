//go:build integration

package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	checkrunquery "orbitjob/internal/admin/app/checkrun/query"
	adminpostgres "orbitjob/internal/admin/store/postgres"
	"orbitjob/internal/core/domain/check"
)

// All source and parent IDs come from actual successful writes. This proves
// visibility before testing filtering, rather than relying on absent fixtures.
func createScopedAvailabilitySLO(t *testing.T, server *httptest.Server, key string) (checkID, sliID, sloID int64) {
	t.Helper()
	response := postJSON(t, server.Client(), server.URL+"/api/v1/checks", key, map[string]any{
		"name": "health", "check_type": "http_health", "check_config": map[string]any{"url": "http://localhost/healthz"},
		"assertion_rules": []map[string]any{{"metric": "status_code", "operator": "==", "threshold": 200, "severity": "warning"}},
		"cron_expr":       "*/5 * * * *",
	})
	var created struct {
		ID int64 `json:"id"`
	}
	requireStatus(t, response, http.StatusCreated)
	decodeJSON(t, response, &created)
	checkID = created.ID
	response = postJSON(t, server.Client(), server.URL+"/api/v1/slis", key, map[string]any{
		"name": "availability", "sli_type": "availability", "source_type": "job_run",
		"source_config": map[string]any{"source_uid": check.CheckSourceUID(checkID)},
		"aggregation":   "ratio", "good_event_criteria": map[string]any{"status": "success"},
	})
	requireStatus(t, response, http.StatusCreated)
	decodeJSON(t, response, &created)
	sliID = created.ID
	response = postJSON(t, server.Client(), server.URL+"/api/v1/slos", key, map[string]any{
		"name": "availability-slo", "sli_id": sliID, "target": 0.999,
		"window_type": "rolling", "window_duration": "24h",
	})
	requireStatus(t, response, http.StatusCreated)
	decodeJSON(t, response, &created)
	return checkID, sliID, created.ID
}

func TestDerivedReadRoutesRespectResourceGroup(t *testing.T) {
	server, db, bootstrapKey := newIntegrationServer(t)
	defer server.Close()
	tenantID := createTenant(t, server, bootstrapKey, "derived-scope")
	_, adminKey := createKey(t, server, bootstrapKey, tenantID, []string{tenantAdminPolicyID}, "")
	ci := createGroup(t, server, adminKey, "ci")
	prod := createGroup(t, server, adminKey, "prod")
	_, ciKey := createKeyScoped(t, server, adminKey, tenantID, ci)
	_, prodKey := createKeyScoped(t, server, adminKey, tenantID, prod)
	type fixture struct{ check, slo, run, budget, alert int64 }
	seed := func(key string) fixture {
		check, _, slo := createScopedAvailabilitySLO(t, server, key)
		f := fixture{check: check, slo: slo}
		queries := []struct {
			sql  string
			args []any
			id   *int64
		}{
			{`INSERT INTO check_runs (tenant_id, check_id, scheduled_at) VALUES ($1,$2,now()) RETURNING id`, []any{tenantID, check}, &f.run},
			{`INSERT INTO budgets (tenant_id,slo_id,window_start,window_end,budget_total,budget_remaining,burn_rate) VALUES ($1,$2,now(),now()+interval '24h',1,1,0) RETURNING id`, []any{tenantID, slo}, &f.budget},
		}
		for _, query := range queries {
			if err := db.QueryRowContext(t.Context(), query.sql, query.args...).Scan(query.id); err != nil {
				t.Fatal(err)
			}
		}
		if err := db.QueryRowContext(t.Context(), `INSERT INTO budget_alerts (tenant_id,slo_id,budget_id,alert_type,burn_rate) VALUES ($1,$2,$3,'fast_burn',14.4) RETURNING id`, tenantID, slo, f.budget).Scan(&f.alert); err != nil {
			t.Fatal(err)
		}
		return f
	}
	owned, other := seed(ciKey), seed(prodKey)
	for _, path := range []string{
		fmt.Sprintf("/check-runs/%d", owned.run), fmt.Sprintf("/slos/%d/budget", owned.slo), fmt.Sprintf("/slo-alerts/%d", owned.alert),
		fmt.Sprintf("/slos/%d/budgets", owned.slo),
	} {
		for _, caller := range []struct {
			key    string
			status int
		}{{ciKey, 200}, {adminKey, 200}, {prodKey, 404}} {
			response := get(t, server.Client(), server.URL+"/api/v1"+path, caller.key)
			requireStatus(t, response, caller.status)
			_ = response.Body.Close()
		}
	}
	for _, tc := range []struct {
		path, key string
		total     int
		id        int64
	}{
		{"/check-runs", ciKey, 1, owned.run}, {"/check-runs", prodKey, 1, other.run}, {"/check-runs", adminKey, 2, 0},
		{fmt.Sprintf("/check-runs?check_id=%d", other.check), ciKey, 0, 0},
		{fmt.Sprintf("/slos/%d/budgets", owned.slo), ciKey, 1, 0},
		{"/slo-alerts", ciKey, 1, owned.alert}, {"/slo-alerts", prodKey, 1, other.alert}, {"/slo-alerts", adminKey, 2, 0},
		{fmt.Sprintf("/slo-alerts?slo_id=%d&status=active", other.slo), ciKey, 0, 0},
	} {
		t.Run(tc.path+tc.key[:8], func(t *testing.T) {
			response := get(t, server.Client(), server.URL+"/api/v1"+tc.path, tc.key)
			requireStatus(t, response, http.StatusOK)
			var out struct {
				Items []json.RawMessage `json:"items"`
			}
			decodeJSON(t, response, &out)
			if len(out.Items) != tc.total {
				t.Fatalf("filtered rows=%d want=%d", len(out.Items), tc.total)
			}
			if tc.id != 0 {
				var item struct {
					ID int64 `json:"id"`
				}
				if err := json.Unmarshal(out.Items[0], &item); err != nil || item.ID != tc.id {
					t.Fatalf("wrong visible identity=%d err=%v want=%d", item.ID, err, tc.id)
				}
			}
		})
	}
	// HTTP list envelopes expose items only. Verify the repository counts as
	// well, so a future pagination response cannot disclose other-group totals.
	for _, scope := range []struct {
		group string
		total int
	}{{ci, 1}, {prod, 1}, {"", 2}} {
		rows, total, err := adminpostgres.NewCheckRunRepository(db).List(t.Context(), checkrunquery.ListCheckRunsInput{TenantID: tenantID, ResourceGroupID: scope.group})
		if err != nil || total != scope.total || len(rows) != scope.total {
			t.Fatalf("check run count=%d rows=%d err=%v", total, len(rows), err)
		}
		alerts, alertTotal, err := adminpostgres.NewBudgetAlertReadRepository(db).List(t.Context(), tenantID, scope.group, nil, nil, 20, 0)
		if err != nil || alertTotal != int64(scope.total) || len(alerts) != scope.total {
			t.Fatalf("alert count=%d rows=%d err=%v", alertTotal, len(alerts), err)
		}
	}
	for _, scope := range []struct {
		group string
		total int
	}{{ci, 1}, {prod, 0}, {"", 1}} {
		rows, total, err := adminpostgres.NewBudgetReadRepository(db).ListHistory(t.Context(), tenantID, scope.group, owned.slo, 20, 0)
		if err != nil || total != int64(scope.total) || len(rows) != scope.total {
			t.Fatalf("budget count=%d rows=%d err=%v", total, len(rows), err)
		}
	}
}
