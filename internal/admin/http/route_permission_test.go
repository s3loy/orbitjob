package http

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"orbitjob/internal/admin/http/middleware"
	"strings"
	"testing"
	"time"

	"orbitjob/internal/core/domain/policy"
)

// TestEveryRouteDeclaresAPermission is the guard that keeps this design from
// eroding. Adding a route without naming the action it needs fails here, rather
// than silently shipping an endpoint that the authorization middleware never
// sees because its permission is empty.
func TestEveryRouteDeclaresAPermission(t *testing.T) {
	public := map[string]bool{
		"/healthz":      true,
		"/metrics":      true,
		"/openapi.json": true,
	}
	for _, route := range adminAPIRoutes() {
		// Identity and the workspace directory are authenticated filtered
		// discovery. Own labels are implicit; the directory authorizes each
		// foreign tenant:Get before applying SQL search and pagination.
		if strings.HasPrefix(route.path, "/auth/") || route.path == "/identity" || route.path == "/workspaces" {
			continue
		}
		if public[route.path] {
			if route.permission != "" {
				t.Errorf("public route %s should carry no permission, got %q", route.path, route.permission)
			}
			continue
		}
		if route.permission == "" {
			t.Errorf("route %s %s declares no permission", route.method, route.path)
		}
		if route.resource == nil && route.resourceForHandler == nil {
			t.Errorf("route %s %s declares a permission but no resource resolver", route.method, route.path)
		}
	}
}

// TestEveryRouteUsesAKnownAction closes the loop between the two action lists.
// A route naming an action outside the registry is unreachable: no tenant may
// create a policy that grants it, so no key can ever hold it. The endpoint
// would exist, be documented, and deny everyone -- a bug that looks like a
// permissions problem and gets debugged for an afternoon.
func TestEveryRouteUsesAKnownAction(t *testing.T) {
	for _, route := range adminAPIRoutes() {
		if route.permission == "" {
			continue
		}
		if !policy.IsKnownAction(route.permission) {
			t.Errorf("route %s %s requires %q, which is not in policy.KnownActions",
				route.method, route.path, route.permission)
		}
	}
}

func TestInstallationManagementGrantsRemainInsidePolicyEvaluation(t *testing.T) {
	deny := policy.Document{Statement: []policy.Statement{{Effect: policy.EffectDeny, Action: []string{"tenant:Get"}, Resource: []string{"orbitjob:blocked:*:tenant/blocked"}}}}
	docs := installationManagementDocuments([]policy.Document{deny})
	for _, tc := range []struct {
		action, tenant, kind, id string
		want                     policy.Decision
	}{
		{"tenant:Get", "blocked", "tenant", "blocked", policy.DecisionDeny},
		{"tenant:Get", "visible", "tenant", "visible", policy.DecisionAllow},
		{"tenant:Create", "*", "tenant", "*", policy.DecisionAllow},
		{"tenant:List", "*", "tenant", "*", policy.DecisionAllow},
		{"job:List", "visible", "job", "*", policy.DecisionDeny},
		{"apikey:Create", "visible", "apikey", "*", policy.DecisionDeny},
	} {
		got := policy.Evaluate(docs, policy.Request{Action: tc.action, Resource: policy.ARN{Tenant: tc.tenant, Group: "*", Type: tc.kind, ID: tc.id}})
		if got != tc.want {
			t.Fatalf("%s %s got %s want %s", tc.action, tc.tenant, got, tc.want)
		}
	}
}

func TestInstallationManagementDenyReachesTheRouteGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		middleware.SetPrincipal(c, middleware.Principal{SubjectType: "user", UserID: "human", InstallationAdmin: true})
		middleware.SetDocuments(c, []policy.Document{{Statement: []policy.Statement{{Effect: policy.EffectDeny, Action: []string{"tenant:Get"}, Resource: []string{"orbitjob:blocked:*:tenant/blocked"}}}}})
		c.Next()
	})
	route := routeDefinition{method: http.MethodGet, permission: "tenant:Get", resource: func(*gin.Context) (policy.ARN, error) {
		return policy.ARN{Tenant: "blocked", Group: "*", Type: "tenant", ID: "blocked"}, nil
	}}
	r.GET("/blocked", guardFor(route, NewHandler(nil, nil, nil)), func(c *gin.Context) { t.Fatal("installation administrator bypassed a matching Deny") })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/blocked", nil))
	if w.Code != 403 {
		t.Fatalf("status %d want403", w.Code)
	}
}

func TestHumanCredentialManagementRequiresRecentAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, action  string
		human, recent bool
		want          int
	}{{"old human credential", "apikey:Create", true, false, 403}, {"recent human credential", "apikey:Create", true, true, 204}, {"automation compatibility", "apikey:Create", false, false, 204}, {"ordinary trigger", "job:Trigger", true, false, 204}, {"policy grant", "policy:Create", true, false, 403}, {"group grant", "group:Create", true, false, 403}} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				p := middleware.Principal{KeyID: "key", RecentAuthAt: time.Now().Add(-time.Hour)}
				if tc.human {
					p.SubjectType = "user"
					p.UserID = "human"
					p.KeyID = ""
				}
				if tc.recent {
					p.RecentAuthAt = time.Now()
				}
				middleware.SetPrincipal(c, p)
				middleware.SetDocuments(c, []policy.Document{{Statement: []policy.Statement{{Effect: policy.EffectAllow, Action: []string{tc.action}, Resource: []string{"orbitjob:*:*:apikey/*"}}}}})
				c.Next()
			})
			route := routeDefinition{method: http.MethodPost, permission: tc.action, resource: func(*gin.Context) (policy.ARN, error) {
				return policy.ARN{Tenant: "tenant", Group: "*", Type: "apikey", ID: "*"}, nil
			}}
			h := NewHandler(nil, nil, nil)
			h.accounts = NewAccountHTTP(nil, middleware.ConsoleSecurity{})
			r.POST("/write", guardFor(route, h), func(c *gin.Context) { c.Status(204) })
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("POST", "/write", nil))
			if w.Code != tc.want {
				t.Fatalf("status %d want%d: %s", w.Code, tc.want, w.Body)
			}
			if tc.want == 403 && !strings.Contains(w.Body.String(), "recent_auth_required") {
				t.Fatal("missing reauth recovery code")
			}
		})
	}
}
