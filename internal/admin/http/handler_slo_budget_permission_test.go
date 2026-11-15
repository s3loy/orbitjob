package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	sloquery "orbitjob/internal/admin/app/slo/query"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/domain/policy"
)

type stubBudgetSLOGetter struct {
	calls int
	group string
}

func (s *stubBudgetSLOGetter) Get(_ context.Context, _ string, group string, _ int64) (sloquery.GetItem, error) {
	s.calls++
	s.group = group
	return sloquery.GetItem{
		ID: 7, Name: "permitted-objective", SLIID: 2, Version: 3,
		CurrentBudget: &sloquery.BudgetItem{BudgetTotal: 100, BudgetConsumed: 10, BudgetRemaining: 90},
	}, nil
}

func TestGetSLO_OptionalBudgetRequiresEvidenceGrant(t *testing.T) {
	allowGet := policy.Document{Statement: []policy.Statement{{
		Effect: policy.EffectAllow, Action: []string{"slo:Get"}, Resource: []string{"orbitjob:tenant-a:*:slo/*"},
	}}}
	allowBudget := policy.Document{Statement: []policy.Statement{{
		Effect: policy.EffectAllow, Action: []string{"slo:GetBudget"}, Resource: []string{"orbitjob:tenant-a:*:slo/7"},
	}}}
	denyBudget := policy.Document{Statement: []policy.Statement{{
		Effect: policy.EffectDeny, Action: []string{"slo:GetBudget"}, Resource: []string{"orbitjob:tenant-a:*:slo/7"},
	}}}
	otherBudget := policy.Document{Statement: []policy.Statement{{
		Effect: policy.EffectAllow, Action: []string{"slo:GetBudget"}, Resource: []string{"orbitjob:tenant-a:*:slo/8"},
	}}}
	foreignBudget := policy.Document{Statement: []policy.Statement{{
		Effect: policy.EffectAllow, Action: []string{"slo:GetBudget"}, Resource: []string{"orbitjob:tenant-b:*:slo/7"},
	}}}
	for _, tc := range []struct {
		name          string
		group         string
		docs          []policy.Document
		noDocuments   bool
		noPrincipal   bool
		budgetAllowed bool
	}{
		{name: "definition only", docs: []policy.Document{allowGet}},
		{name: "explicit deny overrides allow", docs: []policy.Document{allowGet, allowBudget, denyBudget}},
		{name: "evidence allowed", docs: []policy.Document{allowGet, allowBudget}, budgetAllowed: true},
		{name: "group scope refuses evidence", group: "group-a", docs: []policy.Document{allowGet, allowBudget}},
		{name: "another objective is not a grant", docs: []policy.Document{allowGet, otherBudget}},
		{name: "another tenant is not a grant", docs: []policy.Document{allowGet, foreignBudget}},
		{name: "missing documents omit evidence", noDocuments: true},
		{name: "missing principal omits evidence", docs: []policy.Document{allowGet, allowBudget}, noPrincipal: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uc := &stubBudgetSLOGetter{}
			h := NewHandler(nil, nil, nil)
			h.SetEvidenceScopeReader(stubEvidenceScopes{})
			h.SetGetSLOUseCase(uc)
			router := gin.New()
			router.Use(func(c *gin.Context) {
				ctx := middleware.WithTenantID(c.Request.Context(), "tenant-a", middleware.TenantSourceAPIKey)
				c.Request = c.Request.WithContext(ctx)
				if !tc.noPrincipal {
					middleware.SetPrincipal(c, middleware.Principal{
						TenantID: "tenant-a", Kind: middleware.KindTenant, ResourceGroupID: tc.group,
					})
				}
				if !tc.noDocuments {
					middleware.SetDocuments(c, tc.docs)
				}
				c.Next()
			})
			// Route authorization has its own tests. Mount the handler directly to
			// also prove its optional field fails closed without auth context.
			router.GET("/api/v1/slos/:id", h.GetSLO)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/slos/7", nil))
			if tc.noPrincipal {
				if response.Code != http.StatusForbidden {
					t.Fatalf("missing principal should deny: %d", response.Code)
				}
				return
			}
			if response.Code != http.StatusOK {
				t.Fatalf("definition failed: %d %s", response.Code, response.Body.String())
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if string(body["name"]) != `"permitted-objective"` || string(body["version"]) != "3" {
				t.Fatalf("permitted definition was lost: %s", response.Body.String())
			}
			_, hasBudget := body["current_budget"]
			if hasBudget != tc.budgetAllowed {
				t.Fatalf("current_budget present=%t, want %t: %s", hasBudget, tc.budgetAllowed, response.Body.String())
			}
			if uc.calls != 1 || uc.group != tc.group {
				t.Fatalf("definition query calls=%d group=%q", uc.calls, uc.group)
			}
		})
	}
}
