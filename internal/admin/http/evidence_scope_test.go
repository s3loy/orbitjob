package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	checkrunquery "orbitjob/internal/admin/app/checkrun/query"
	alertquery "orbitjob/internal/admin/app/sloalert/query"
	budgetquery "orbitjob/internal/admin/app/slobudget/query"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/platform/scan"
)

func TestSLODefinitionDoesNotEmbedBudgetDeniedOnItsActualGroup(t *testing.T) {
	tenant, prod := scan.GenerateID(), scan.GenerateID()
	h := NewHandler(nil, nil, nil)
	h.SetGetSLOUseCase(&stubBudgetSLOGetter{})
	h.SetEvidenceScopeReader(stubEvidenceScopes{group: prod})
	docs := []policy.Document{
		evidenceGrant(tenant, "*", "slo", "7", "slo:Get", policy.EffectAllow),
		evidenceGrant(tenant, "*", "slo", "7", "slo:GetBudget", policy.EffectAllow),
		evidenceGrant(tenant, prod, "slo", "7", "slo:GetBudget", policy.EffectDeny),
	}
	response := httptest.NewRecorder()
	evidenceTestRouter(h, tenant, "", docs).ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/slos/7", nil))
	var out map[string]any
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &out) != nil {
		t.Fatalf("definition read: %d %s", response.Code, response.Body.String())
	}
	if _, leaked := out["current_budget"]; leaked {
		t.Fatal("denied budget escaped through the SLO definition")
	}
}

type stubEvidenceScopes struct {
	group string
	err   error
}

func (s stubEvidenceScopes) CheckRunGroup(context.Context, string, int64) (string, error) {
	return s.group, s.err
}
func (s stubEvidenceScopes) SLOGroup(context.Context, string, int64) (string, error) {
	return s.group, s.err
}
func (s stubEvidenceScopes) AlertGroup(context.Context, string, int64) (string, error) {
	return s.group, s.err
}

func evidenceTestRouter(h *Handler, tenant, group string, docs []policy.Document) *gin.Engine {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(middleware.WithTenantID(c.Request.Context(), tenant, middleware.TenantSourceAPIKey))
		middleware.SetPrincipal(c, middleware.Principal{Kind: middleware.KindTenant, TenantID: tenant, ResourceGroupID: group})
		middleware.SetDocuments(c, docs)
		c.Next()
	})
	h.Register(r)
	return r
}

func evidenceGrant(tenant, group, kind, id, action string, effect policy.Effect) policy.Document {
	return policy.Document{Statement: []policy.Statement{{Effect: effect, Action: []string{action}, Resource: []string{(policy.ARN{Tenant: tenant, Group: group, Type: kind, ID: id}).String()}}}}
}

func TestEvidenceDetailsAuthorizeStoredParentGroup(t *testing.T) {
	for _, kind := range []string{"checkrun", "budget", "history", "alert"} {
		for _, scenario := range []string{"allow", "deny_actual_group", "other_group", "missing_reader", "lookup_error"} {
			t.Run(kind+"/"+scenario, func(t *testing.T) {
				tenant, prod := scan.GenerateID(), scan.GenerateID()
				calls := 0
				queryGroup := ""
				h := NewHandler(nil, nil, nil)
				h.SetEvidenceScopeReader(stubEvidenceScopes{group: prod})
				path, resourceType, action := "/check-runs/7", "checkrun", "checkrun:Get"
				switch kind {
				case "checkrun":
					h.SetGetCheckRunUseCase(scopedMonitoringGet[checkrunquery.GetResult](func(_ context.Context, _ string, group string, _ int64) (checkrunquery.GetResult, error) {
						calls++
						queryGroup = group
						return checkrunquery.GetResult{}, nil
					}))
				case "budget":
					path, resourceType, action = "/slos/7/budget", "slo", "slo:GetBudget"
					h.SetGetBudgetUseCase(scopedMonitoringGet[budgetquery.GetItem](func(_ context.Context, _ string, group string, _ int64) (budgetquery.GetItem, error) {
						calls++
						queryGroup = group
						return budgetquery.GetItem{}, nil
					}))
				case "history":
					path, resourceType, action = "/slos/7/budgets", "slo", "slo:ListBudgets"
					h.SetListBudgetHistoryUseCase(scopedMonitoringList[budgetquery.ListInput, budgetquery.ListResult](func(_ context.Context, in budgetquery.ListInput) (budgetquery.ListResult, error) {
						calls++
						queryGroup = in.ResourceGroupID
						return budgetquery.ListResult{}, nil
					}))
				case "alert":
					path, resourceType, action = "/slo-alerts/7", "sloalert", "sloalert:Get"
					h.SetGetAlertUseCase(scopedMonitoringGet[alertquery.GetItem](func(_ context.Context, _ string, group string, _ int64) (alertquery.GetItem, error) {
						calls++
						queryGroup = group
						return alertquery.GetItem{}, nil
					}))
				}
				docs := []policy.Document{evidenceGrant(tenant, "*", resourceType, "*", action, policy.EffectAllow)}
				group := ""
				want := 200
				switch scenario {
				case "deny_actual_group":
					docs = append(docs, evidenceGrant(tenant, prod, resourceType, "7", action, policy.EffectDeny))
					want = 403
				case "other_group":
					group = scan.GenerateID()
					want = 404
				case "missing_reader":
					h.SetEvidenceScopeReader(nil)
					want = 500
				case "lookup_error":
					h.SetEvidenceScopeReader(stubEvidenceScopes{err: errors.New("scope lookup failed")})
					want = 500
				}
				response := httptest.NewRecorder()
				evidenceTestRouter(h, tenant, group, docs).ServeHTTP(response, httptest.NewRequest("GET", "/api/v1"+path, nil))
				if response.Code != want || calls != map[bool]int{true: 1, false: 0}[want == 200] {
					t.Fatalf("status=%d calls=%d want=%d body=%s", response.Code, calls, want, response.Body.String())
				}
				if calls > 0 && queryGroup != prod {
					t.Fatalf("read did not retain authorized group: %q", queryGroup)
				}
			})
		}
	}
}

func TestEvidenceCollectionRequiresWholeQueryPermission(t *testing.T) {
	for _, kind := range []string{"checkrun", "sloalert"} {
		for _, scenario := range []string{"allow", "group_deny", "id_deny", "deny_outside_credential_group", "boundary_deny", "proof_budget"} {
			t.Run(kind+"/"+scenario, func(t *testing.T) {
				tenant, prod, ci := scan.GenerateID(), scan.GenerateID(), scan.GenerateID()
				calls := 0
				h := NewHandler(nil, nil, nil)
				path, action := "/check-runs", "checkrun:List"
				if kind == "checkrun" {
					h.SetListCheckRunsUseCase(scopedMonitoringList[checkrunquery.ListCheckRunsInput, checkrunquery.ListCheckRunsResult](func(_ context.Context, _ checkrunquery.ListCheckRunsInput) (checkrunquery.ListCheckRunsResult, error) {
						calls++
						return checkrunquery.ListCheckRunsResult{}, nil
					}))
				} else {
					path, action = "/slo-alerts", "sloalert:List"
					h.SetListAlertsUseCase(scopedMonitoringList[alertquery.ListInput, alertquery.ListResult](func(_ context.Context, _ alertquery.ListInput) (alertquery.ListResult, error) {
						calls++
						return alertquery.ListResult{}, nil
					}))
				}
				allow := evidenceGrant(tenant, "*", kind, "*", action, policy.EffectAllow)
				docs := []policy.Document{allow}
				group := ""
				want := 200
				switch scenario {
				case "group_deny":
					docs = append(docs, evidenceGrant(tenant, prod, kind, "*", action, policy.EffectDeny))
					want = 403
				case "id_deny":
					docs = append(docs, evidenceGrant(tenant, "*", kind, "7", action, policy.EffectDeny))
					want = 403
				case "deny_outside_credential_group":
					group = ci
					docs = append(docs, evidenceGrant(tenant, prod, kind, "7", action, policy.EffectDeny))
				case "boundary_deny":
					boundary := allow
					boundary.Statement = append(append([]policy.Statement{}, allow.Statement...), evidenceGrant(tenant, prod, kind, "7", action, policy.EffectDeny).Statement...)
					docs = policy.Intersect(docs, boundary)
					want = 403
				case "proof_budget":
					docs = []policy.Document{{Statement: make([]policy.Statement, 4097)}}
					for i := range docs[0].Statement {
						docs[0].Statement[i] = allow.Statement[0]
					}
					want = 403
				}
				response := httptest.NewRecorder()
				evidenceTestRouter(h, tenant, group, docs).ServeHTTP(response, httptest.NewRequest("GET", "/api/v1"+path, nil))
				if response.Code != want || calls != map[bool]int{true: 1, false: 0}[want == 200] {
					t.Fatalf("status=%d calls=%d want=%d body=%s", response.Code, calls, want, response.Body.String())
				}
			})
		}
	}
}
