//go:build integration

package http

import (
	"net/http"
	"net/http/httptest"
	"orbitjob/internal/core/domain/check"
	"testing"
)

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
