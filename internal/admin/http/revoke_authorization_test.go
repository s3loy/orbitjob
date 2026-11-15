package http

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	apikeycommand "orbitjob/internal/admin/app/apikey/command"
	"orbitjob/internal/admin/bootstrap"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/domain/apikey"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/platform/scan"
)

type targetRevoker struct {
	target apikey.APIKey
	got    apikeycommand.RevokeInput
}

func (r *targetRevoker) Target(context.Context, string) (apikey.APIKey, error) { return r.target, nil }
func (r *targetRevoker) Revoke(_ context.Context, in apikeycommand.RevokeInput) error {
	r.got = in
	return nil
}

func TestRevokeAuthorizesActualOwnerAndRetainsActor(t *testing.T) {
	for _, cross := range []bool{false, true} {
		t.Run(map[bool]string{false: "own_tenant", true: "explicit_cross_tenant_grant"}[cross], func(t *testing.T) {
			caller, owner, key, actor := scan.GenerateID(), scan.GenerateID(), scan.GenerateID(), scan.GenerateID()
			if !cross {
				owner = caller
			}
			uc := &targetRevoker{target: apikey.APIKey{ID: key, TenantID: owner}}
			h := NewHandler(nil, nil, nil)
			h.SetRevokeAPIKeyUseCase(uc)
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Request = c.Request.WithContext(middleware.WithTenantID(c.Request.Context(), caller, middleware.TenantSourceAPIKey))
				middleware.SetPrincipal(c, middleware.Principal{TenantID: caller, Kind: middleware.KindTenant, KeyID: actor})
				middleware.SetDocuments(c, []policy.Document{{Statement: []policy.Statement{{Effect: policy.EffectAllow, Action: []string{"apikey:Revoke"}, Resource: []string{(policy.ARN{Tenant: owner, Group: "*", Type: "apikey", ID: key}).String()}}}}})
				c.Next()
			})
			h.Register(router)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("POST", "/api/v1/api_keys/"+key+"/revoke", nil))
			if response.Code != 200 {
				t.Fatalf("revoke status=%d body=%s", response.Code, response.Body.String())
			}
			if uc.got.TenantID != owner || uc.got.ActorID != actor || uc.got.ID != key {
				t.Fatalf("wrong revocation/audit target: %+v", uc.got)
			}
		})
	}
}

func TestRevokeRequiresTargetGrantAndMatchingGroup(t *testing.T) {
	for _, scenario := range []string{"bootstrap_without_target_grant", "caller_tenant_grant", "other_group", "same_group"} {
		t.Run(scenario, func(t *testing.T) {
			caller, owner, key, actor, group := scan.GenerateID(), scan.GenerateID(), scan.GenerateID(), scan.GenerateID(), scan.GenerateID()
			principal := middleware.Principal{TenantID: caller, KeyID: actor, Kind: middleware.KindTenant}
			grantTenant := owner
			switch scenario {
			case "bootstrap_without_target_grant":
				principal.TenantID = bootstrap.DefaultTenantID
				grantTenant = principal.TenantID
			case "caller_tenant_grant":
				grantTenant = caller
			case "other_group":
				principal.ResourceGroupID = scan.GenerateID()
			case "same_group":
				principal.ResourceGroupID = group
			}
			uc := &targetRevoker{target: apikey.APIKey{ID: key, TenantID: owner, ResourceGroupID: group}}
			h := NewHandler(nil, nil, nil)
			h.SetRevokeAPIKeyUseCase(uc)
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Request = c.Request.WithContext(middleware.WithTenantID(c.Request.Context(), principal.TenantID, middleware.TenantSourceAPIKey))
				middleware.SetPrincipal(c, principal)
				middleware.SetDocuments(c, []policy.Document{{Statement: []policy.Statement{{Effect: policy.EffectAllow, Action: []string{"apikey:Revoke"}, Resource: []string{(policy.ARN{Tenant: grantTenant, Group: group, Type: "apikey", ID: key}).String()}}}}})
				c.Next()
			})
			h.Register(router)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("POST", "/api/v1/api_keys/"+key+"/revoke", nil))
			want := 403
			switch scenario {
			case "other_group":
				want = 404
			case "same_group":
				want = 200
			}
			if response.Code != want || (uc.got.ID != "") != (want == 200) {
				t.Fatalf("status=%d mutation=%+v body=%s", response.Code, uc.got, response.Body.String())
			}
		})
	}
}
