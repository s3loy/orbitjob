package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"orbitjob/internal/admin/app/account"
	"orbitjob/internal/core/domain/audit"
	"orbitjob/internal/core/domain/policy"
)

type humanAuthStub struct {
	session   *account.Session
	err       error
	memberErr error
	docs      []policy.Document
	calls     int
}

func (s *humanAuthStub) Authenticate(context.Context, string) (*account.Session, error) {
	s.calls++
	return s.session, s.err
}
func (s *humanAuthStub) Membership(_ context.Context, user, tenant string) (account.Membership, error) {
	if user != "human-1" {
		return account.Membership{}, account.ErrForbidden
	}
	return account.Membership{TenantID: tenant, Role: "viewer"}, s.memberErr
}
func (s *humanAuthStub) EffectiveDocuments(context.Context, string, string) ([]policy.Document, error) {
	return s.docs, nil
}
func humanFixture() *humanAuthStub {
	return &humanAuthStub{session: &account.Session{ID: "session-1", User: account.User{ID: "human-1", Username: "alice", InstallationAdmin: true}, CSRFToken: "csrf-secret", RecentAuthAt: time.Now()}}
}
func TestHumanAuthenticationNeverInventsAKeyOrPlatformGrant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := humanFixture()
	a := NewAuth(nil)
	a.Accounts = store
	r := gin.New()
	r.Use(a.Middleware())
	r.GET("/api/v1/identity", func(c *gin.Context) {
		p, ok := PrincipalFrom(c)
		if !ok || !p.IsHuman() || p.KeyID != "" || p.IsPlatform() || p.ActorID() != "human-1" {
			t.Fatalf("unexpected principal %+v", p)
		}
		if audit.ActorType(c.Request.Context(), p.ActorID()) != "user" {
			t.Fatal("user audit metadata missing")
		}
		docs, ok := Documents(c)
		if !ok || len(docs) != 0 {
			t.Fatal("no workspace must grant nothing")
		}
		c.Status(200)
	})
	req := httptest.NewRequest("GET", "https://console.example/api/v1/identity", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: strings.Repeat("a", 43)})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d %s", w.Code, w.Body)
	}
}
func TestHumanBusinessWritesRequireCSRFAndCurrentMembership(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, origin, csrf string
		memberErr          error
		want               int
	}{
		{"valid", "https://console.example", "csrf-secret", nil, 204},
		{"missing CSRF", "https://console.example", "", nil, 403},
		{"wrong CSRF", "https://console.example", "other", nil, 403},
		{"cross origin", "https://other.example", "csrf-secret", nil, 403},
		{"null origin", "null", "csrf-secret", nil, 403},
		{"removed membership", "https://console.example", "csrf-secret", account.ErrForbidden, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := humanFixture()
			store.memberErr = tc.memberErr
			a := NewAuth(nil)
			a.Accounts = store
			r := gin.New()
			r.Use(a.Middleware())
			r.POST("/api/v1/jobs/1/trigger", func(c *gin.Context) {
				p, _ := PrincipalFrom(c)
				if p.TenantID != "workspace" || p.MembershipRole != "viewer" {
					t.Fatalf("wrong workspace %+v", p)
				}
				c.Status(204)
			})
			req := httptest.NewRequest("POST", "https://console.example/api/v1/jobs/1/trigger?tenant_id=workspace", nil)
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("X-CSRF-Token", tc.csrf)
			req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: strings.Repeat("a", 43)})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status %d want %d: %s", w.Code, tc.want, w.Body)
			}
		})
	}
}
func TestExplicitAuthorizationNeverFallsBackToCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, header := range []string{"", "Bearer invalid", "Basic invalid", "bearer invalid", "Bearer "} {
		t.Run(header, func(t *testing.T) {
			store := humanFixture()
			a := NewAuth(nil)
			a.Accounts = store
			r := gin.New()
			r.Use(a.Middleware())
			r.GET("/api/v1/identity", func(c *gin.Context) { t.Fatal("invalid bearer reached handler") })
			req := httptest.NewRequest("GET", "https://console.example/api/v1/identity", nil)
			req.Header.Set("Authorization", header)
			req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: strings.Repeat("a", 43)})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != 401 || store.calls != 0 {
				t.Fatalf("status=%d cookie calls=%d", w.Code, store.calls)
			}
		})
	}
}
func TestHumanRevocationAndStoreOutageRemainDistinct(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		err  error
		want int
	}{{account.ErrUnauthenticated, 401}, {errors.New("database unavailable"), 503}} {
		store := humanFixture()
		store.err = tc.err
		a := NewAuth(nil)
		a.Accounts = store
		r := gin.New()
		r.Use(a.Middleware())
		r.GET("/api/v1/identity", func(c *gin.Context) { t.Fatal("failed auth reached handler") })
		req := httptest.NewRequest("GET", "https://console.example/api/v1/identity", nil)
		req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: strings.Repeat("a", 43)})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("status %d want %d", w.Code, tc.want)
		}
		if len(w.Result().Cookies()) != 0 {
			t.Fatal("failed auth changed existing cookie")
		}
	}
}

func TestHumanMembershipDocumentsRetainExplicitDeny(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := humanFixture()
	store.docs = []policy.Document{{Statement: []policy.Statement{{Effect: policy.EffectAllow, Action: []string{"job:List"}, Resource: []string{"orbitjob:workspace:*:job/*"}}}}, {Statement: []policy.Statement{{Effect: policy.EffectDeny, Action: []string{"job:List"}, Resource: []string{"orbitjob:workspace:*:job/*"}}}}}
	a := NewAuth(nil)
	a.Accounts = store
	r := gin.New()
	r.Use(a.Middleware())
	r.GET("/api/v1/jobs", Require("job:List", func(*gin.Context) (policy.ARN, error) {
		return policy.ARN{Tenant: "workspace", Group: "*", Type: "job", ID: "*"}, nil
	}), func(c *gin.Context) { t.Fatal("installation administrator bypassed membership Deny") })
	req := httptest.NewRequest("GET", "https://console.example/api/v1/jobs?tenant_id=workspace", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: strings.Repeat("a", 43)})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("Deny status %d want403", w.Code)
	}
}
func TestHumanConflictingWorkspaceSelectorsFailBeforeBusinessHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	a := NewAuth(nil)
	a.Accounts = humanFixture()
	r := gin.New()
	r.Use(a.Middleware())
	r.POST("/api/v1/jobs", func(c *gin.Context) { t.Fatal("conflicting tenant selectors reached handler") })
	req := httptest.NewRequest("POST", "https://console.example/api/v1/jobs?tenant_id=workspace", strings.NewReader(`{"tenant_id":"different"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://console.example")
	req.Header.Set("X-CSRF-Token", "csrf-secret")
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: strings.Repeat("a", 43)})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("status %d want403", w.Code)
	}
}
