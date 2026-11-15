//go:build integration

package http

import (
	"fmt"
	"net/http"
	"orbitjob/internal/core/domain/policy"
	"testing"
)

func TestEvidenceGroupDenyCannotEscapeThroughUnscopedKey(t *testing.T) {
	server, db, bootstrapKey := newIntegrationServer(t)
	defer server.Close()
	tenant := createTenant(t, server, bootstrapKey, "evidence-deny")
	_, adminKey := createKey(t, server, bootstrapKey, tenant, []string{tenantAdminPolicyID}, "")
	prod := createGroup(t, server, adminKey, "prod")
	_, prodKey := createKeyScoped(t, server, adminKey, tenant, prod)
	check, _, slo := createScopedAvailabilitySLO(t, server, prodKey)
	var run, budget, alert int64
	if err := db.QueryRowContext(t.Context(), `INSERT INTO check_runs(tenant_id,check_id,scheduled_at)VALUES($1,$2,now())RETURNING id`, tenant, check).Scan(&run); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), `INSERT INTO budgets(tenant_id,slo_id,window_start,window_end,budget_total,budget_remaining,burn_rate)VALUES($1,$2,now(),now()+interval '24h',1,1,0)RETURNING id`, tenant, slo).Scan(&budget); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), `INSERT INTO budget_alerts(tenant_id,slo_id,budget_id,alert_type,burn_rate)VALUES($1,$2,$3,'fast_burn',14.4)RETURNING id`, tenant, slo, budget).Scan(&alert); err != nil {
		t.Fatal(err)
	}
	doc := policy.Document{Version: "1"}
	for _, target := range []struct {
		kind    string
		id      int64
		actions []string
	}{
		{"checkrun", run, []string{"checkrun:Get", "checkrun:List"}},
		{"slo", slo, []string{"slo:GetBudget", "slo:ListBudgets"}},
		{"sloalert", alert, []string{"sloalert:Get", "sloalert:List"}},
	} {
		doc.Statement = append(doc.Statement, policy.Statement{Effect: policy.EffectDeny, Action: target.actions, Resource: []string{(policy.ARN{Tenant: policy.SelfToken, Group: prod, Type: target.kind, ID: fmt.Sprint(target.id)}).String()}})
	}
	response := postJSON(t, server.Client(), server.URL+"/api/v1/policies", adminKey, map[string]any{"name": "Deny exact prod evidence", "document": doc})
	requireStatus(t, response, http.StatusCreated)
	var created struct {
		ID string `json:"id"`
	}
	decodeJSON(t, response, &created)
	_, restricted := createKey(t, server, adminKey, tenant, []string{tenantAdminPolicyID, created.ID}, "")
	for _, path := range []string{
		fmt.Sprintf("/check-runs/%d", run), "/check-runs", fmt.Sprintf("/check-runs?check_id=%d", check),
		fmt.Sprintf("/slos/%d/budget", slo), fmt.Sprintf("/slos/%d/budgets", slo),
		fmt.Sprintf("/slo-alerts/%d", alert), "/slo-alerts", fmt.Sprintf("/slo-alerts?slo_id=%d", slo),
	} {
		for _, caller := range []struct {
			key    string
			status int
		}{{adminKey, 200}, {restricted, 403}} {
			response := get(t, server.Client(), server.URL+"/api/v1"+path, caller.key)
			requireStatus(t, response, caller.status)
			response.Body.Close()
		}
	}
	response = get(t, server.Client(), server.URL+fmt.Sprintf("/api/v1/slos/%d", slo), restricted)
	requireStatus(t, response, http.StatusOK)
	var result map[string]any
	decodeJSON(t, response, &result)
	if _, leaked := result["current_budget"]; leaked {
		t.Fatal("definition detail leaked a denied budget")
	}
}
