package http

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	apikeycommand "orbitjob/internal/admin/app/apikey/command"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/domain/apikey"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/domain/resource"
)

type stubKeyMetadata struct {
	called     bool
	tenant, id string
	out        apikey.Metadata
	err        error
}

func (s *stubKeyMetadata) Metadata(_ context.Context, tenantID, id string) (apikey.Metadata, error) {
	s.called, s.tenant, s.id = true, tenantID, id
	return s.out, s.err
}

type stubRenameAPIKey struct {
	called bool
	in     apikeycommand.RenameInput
}

func (s *stubRenameAPIKey) Rename(_ context.Context, in apikeycommand.RenameInput) (apikeycommand.APIKeyRenameResult, error) {
	s.called, s.in = true, in
	return apikeycommand.APIKeyRenameResult{ID: in.ID, Name: in.Name}, nil
}

func TestRenameRouteAuthorizesExactOwnerGroupAndAction(t *testing.T) {
	for _, tc := range []struct {
		name, kind, owner, callerGroup, keyGroup, grant, action string
		missing                                                 bool
		status                                                  int
	}{
		{name: "own exact", kind: middleware.KindTenant, owner: "home", grant: "orbitjob:home:*:apikey/key", action: "apikey:Update", status: 200},
		{name: "revoke does not rename", kind: middleware.KindTenant, owner: "home", grant: "orbitjob:home:*:apikey/key", action: "apikey:Revoke", status: 403},
		{name: "foreign tenant", kind: middleware.KindTenant, owner: "foreign", grant: "orbitjob:*:*:apikey/*", action: "apikey:Update", status: 403},
		{name: "missing", kind: middleware.KindTenant, missing: true, grant: "orbitjob:*:*:apikey/*", action: "apikey:Update", status: 403},
		{name: "wrong key", kind: middleware.KindTenant, owner: "home", grant: "orbitjob:home:*:apikey/other", action: "apikey:Update", status: 403},
		{name: "own group", kind: middleware.KindTenant, owner: "home", callerGroup: "ci", keyGroup: "ci", grant: "orbitjob:home:ci:apikey/key", action: "apikey:Update", status: 200},
		{name: "foreign group", kind: middleware.KindTenant, owner: "home", callerGroup: "ci", keyGroup: "prod", grant: "orbitjob:home:*:apikey/*", action: "apikey:Update", status: 403},
		{name: "group cannot rename unscoped", kind: middleware.KindTenant, owner: "home", callerGroup: "ci", grant: "orbitjob:home:*:apikey/*", action: "apikey:Update", status: 403},
		{name: "platform exact target", kind: middleware.KindPlatform, owner: "foreign", grant: "orbitjob:foreign:*:apikey/key", action: "apikey:Update", status: 200},
		{name: "platform wrong owner", kind: middleware.KindPlatform, owner: "foreign", grant: "orbitjob:home:*:apikey/key", action: "apikey:Update", status: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHandler(nil, nil, nil)
			uc := &stubRenameAPIKey{}
			h.SetRenameAPIKeyUseCase(uc)
			owner := stubKeyOwner{tenant: tc.owner}
			if tc.missing {
				owner.err = &resource.NotFoundError{Resource: "api_key", ID: "key"}
			}
			h.SetAPIKeyOwnerReader(owner)
			h.SetAPIKeyMetadataReader(&stubKeyMetadata{out: apikey.Metadata{ResourceGroupID: tc.keyGroup}})
			r := gin.New()
			r.Use(func(c *gin.Context) {
				middleware.SetPrincipal(c, middleware.Principal{Kind: tc.kind, TenantID: "home", KeyID: "actor", ResourceGroupID: tc.callerGroup})
				middleware.SetDocuments(c, []policy.Document{{Statement: []policy.Statement{{Effect: policy.EffectAllow, Action: []string{tc.action}, Resource: []string{tc.grant}}}}})
				c.Next()
			})
			h.Register(r)
			w := httptest.NewRecorder()
			req := httptest.NewRequest("PATCH", "/api/v1/api_keys/key", strings.NewReader(`{"name":"Daily operator"}`))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != tc.status || uc.called != (tc.status == 200) {
				t.Fatalf("status=%d called=%v body=%s", w.Code, uc.called, w.Body)
			}
			if uc.called && (uc.in.TenantID != tc.owner || uc.in.ID != "key" || uc.in.ActorID != "actor" || uc.in.Name != "Daily operator") {
				t.Fatalf("wrong update target %+v", uc.in)
			}
		})
	}
}

func TestRenameRouteHonorsBoundaryAndRequiresNameField(t *testing.T) {
	for _, tc := range []struct {
		body   string
		deny   bool
		status int
	}{
		{`{}`, false, 400}, {`{"name":null}`, false, 400}, {`{"name":""}`, false, 200},
		{`{"name":"Alias"}`, true, 403}, {`{"name":"` + strings.Repeat("x", 129) + `"}`, false, 400},
	} {
		h := NewHandler(nil, nil, nil)
		uc := &stubRenameAPIKey{}
		h.SetRenameAPIKeyUseCase(uc)
		h.SetAPIKeyOwnerReader(stubKeyOwner{tenant: "home"})
		h.SetAPIKeyMetadataReader(&stubKeyMetadata{})
		r := gin.New()
		r.Use(func(c *gin.Context) {
			middleware.SetPrincipal(c, middleware.Principal{Kind: middleware.KindTenant, TenantID: "home"})
			docs := []policy.Document{{Statement: []policy.Statement{{Effect: policy.EffectAllow, Action: []string{"apikey:Update"}, Resource: []string{"orbitjob:home:*:apikey/*"}}}}}
			if tc.deny {
				docs = policy.Intersect(docs, policy.Document{Statement: []policy.Statement{{Effect: policy.EffectAllow, Action: []string{"apikey:List"}, Resource: []string{"orbitjob:home:*:apikey/*"}}}})
			}
			middleware.SetDocuments(c, docs)
			c.Next()
		})
		h.Register(r)
		w := httptest.NewRecorder()
		req := httptest.NewRequest("PATCH", "/api/v1/api_keys/key", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("status=%d body=%s", w.Code, w.Body)
		}
		if tc.status == 200 {
			var out apikeycommand.APIKeyRenameResult
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Name != "" {
				t.Fatalf("clear name failed %+v %v", out, err)
			}
		}
	}
}

func TestRenameOpenAPIHasPatchAndNonNullableName(t *testing.T) {
	doc := ServiceOpenAPIDocument()
	patch := doc.Paths["/api/v1/api_keys/{id}"].Patch
	if patch == nil || patch.OperationID != "renameAPIKey" || patch.Permission != "apikey:Update" {
		t.Fatalf("missing guarded PATCH operation %+v", patch)
	}
	name := doc.Components.Schemas["RenameAPIKeyRequest"].Properties["name"]
	if name.Type != "string" || name.Nullable || name.MaxLength == nil || *name.MaxLength != 128 {
		t.Fatalf("name contract disagrees with binding %+v", name)
	}
}
