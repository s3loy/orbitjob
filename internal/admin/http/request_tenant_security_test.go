package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/platform/scan"
)

func TestCreateCheckAuthorizesThePersistedTenantForEveryContentType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	home, foreign, actor, group := scan.GenerateID(), scan.GenerateID(), scan.GenerateID(), scan.GenerateID()
	for _, mediaType := range []string{"application/json", "application/json; charset=utf-8", "text/plain", ""} {
		for _, tc := range []struct {
			name, subject, query, body string
			boundary, deny             bool
			status                     int
		}{
			{name: "platform body only", subject: "platform", body: home, status: http.StatusCreated},
			{name: "platform query only", subject: "platform", query: home, status: http.StatusCreated},
			{name: "platform matching selectors", subject: "platform", query: home, body: home, status: http.StatusCreated},
			{name: "platform conflicting selectors", subject: "platform", query: home, body: foreign, status: http.StatusForbidden},
			{name: "platform boundary conflicting selectors", subject: "platform", query: home, body: foreign, boundary: true, status: http.StatusForbidden},
			{name: "platform boundary foreign body", subject: "platform", body: foreign, boundary: true, status: http.StatusForbidden},
			{name: "platform denied foreign body", subject: "platform", body: foreign, deny: true, status: http.StatusForbidden},
			{name: "platform missing target", subject: "platform", status: http.StatusForbidden},
			{name: "tenant implicit", subject: "tenant", status: http.StatusCreated},
			{name: "tenant matching selectors", subject: "tenant", query: home, body: home, status: http.StatusCreated},
			{name: "tenant foreign body", subject: "tenant", body: foreign, status: http.StatusForbidden},
			{name: "human matching membership", subject: "human", body: home, status: http.StatusCreated},
			{name: "human foreign body", subject: "human", query: home, body: foreign, status: http.StatusForbidden},
		} {
			t.Run(mediaType+"/"+tc.name, func(t *testing.T) {
				principal := middleware.Principal{KeyID: actor, Kind: middleware.KindPlatform, ResourceGroupID: group}
				if tc.subject != "platform" {
					principal.Kind, principal.TenantID = middleware.KindTenant, home
				}
				if tc.subject == "human" {
					principal.SubjectType, principal.UserID, principal.KeyID = "user", actor, ""
				}
				documents := []policy.Document{{Statement: []policy.Statement{{Effect: policy.EffectAllow, Action: []string{"check:Create"}, Resource: []string{"orbitjob:*:*:check/*"}}}}}
				if tc.boundary {
					documents = policy.Intersect(documents, policy.Document{Statement: []policy.Statement{{Effect: policy.EffectAllow, Action: []string{"check:Create"}, Resource: []string{fmt.Sprintf("orbitjob:%s:*:check/*", home)}}}})
				}
				if tc.deny {
					documents[0].Statement = append(documents[0].Statement, policy.Statement{Effect: policy.EffectDeny, Action: []string{"check:Create"}, Resource: []string{fmt.Sprintf("orbitjob:%s:*:check/*", foreign)}})
				}
				uc := &stubCreateCheckUseCase{}
				h := NewHandler(nil, nil, nil)
				h.SetCreateCheckUseCase(uc)
				router := gin.New()
				router.Use(func(c *gin.Context) {
					middleware.SetPrincipal(c, principal)
					middleware.SetDocuments(c, documents)
					c.Next()
				})
				h.Register(router)
				body := fmt.Sprintf(`{"name":"health","check_type":"http_health","check_config":{"url":"https://example.com/health"},"tenant_id":%q}`, tc.body)
				request := httptest.NewRequest(http.MethodPost, "/api/v1/checks?tenant_id="+tc.query, strings.NewReader(body))
				if mediaType != "" {
					request.Header.Set("Content-Type", mediaType)
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				if response.Code != tc.status {
					t.Fatalf("status=%d want=%d body=%s", response.Code, tc.status, response.Body)
				}
				if tc.status == http.StatusCreated {
					if !uc.called || uc.in.TenantID != home || uc.in.ResourceGroupID != group {
						t.Fatalf("write lost authorized scope: called=%t input=%+v", uc.called, uc.in)
					}
				} else if uc.called {
					t.Fatalf("forbidden request reached write: %+v", uc.in)
				}
			})
		}
	}
}

func TestHandlerTenantSelectorsCannotReplaceTheAuthorizedTarget(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(fmt.Sprintf("cached=%t", cached), func(t *testing.T) {
			home, foreign := scan.GenerateID(), scan.GenerateID()
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodPost, "/?tenant_id="+home, strings.NewReader(`{"tenant_id":"`+home+`"}`))
			middleware.SetPrincipal(c, middleware.Principal{Kind: middleware.KindPlatform})
			if cached {
				if _, err := tenantScoped("check")(c); err != nil {
					t.Fatal(err)
				}
			}
			var bound map[string]string
			if err := json.NewDecoder(c.Request.Body).Decode(&bound); err != nil {
				t.Fatal(err)
			}
			if target, ok := requireTenantID(c, foreign); ok || target != "" || response.Code != http.StatusForbidden {
				t.Fatalf("handler replaced target: target=%q ok=%t status=%d", target, ok, response.Code)
			}
		})
	}
}
