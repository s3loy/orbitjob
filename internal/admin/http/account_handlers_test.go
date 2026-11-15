package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"orbitjob/internal/admin/app/account"
	"orbitjob/internal/admin/http/middleware"
)

type accountRepositoryStub struct {
	call func(context.Context, string, any, any) error
}

func (s accountRepositoryStub) Transaction(ctx context.Context, fn func(account.Repository) error) error {
	return fn(s)
}
func (s accountRepositoryStub) Call(ctx context.Context, op string, input, output any) error {
	return s.call(ctx, op, input, output)
}
func accountOutput(output, value any) error {
	b, e := json.Marshal(value)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, output)
}
func accountRouter(t *testing.T, repo account.Repository) (*gin.Engine, *AccountHTTP) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	a := NewAccountHTTP(account.NewService(repo), middleware.ConsoleSecurity{})
	h := NewHandler(nil, nil, nil)
	h.SetAccountHTTP(a)
	r := gin.New()
	h.Register(r)
	return r, a
}
func consoleRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, "https://console.example/api/v1/auth"+path, strings.NewReader(body))
	r.Header.Set("Origin", "https://console.example")
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = "127.0.0.1:54321"
	return r
}
func TestSessionSnapshotDistinguishesAnonymousRevokedAndUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name          string
		authErr       error
		cookie        bool
		want          int
		authenticated bool
	}{{"anonymous", nil, false, 200, false}, {"restored", nil, true, 200, true}, {"revoked", account.ErrUnauthenticated, true, 200, false}, {"unavailable", errors.New("offline"), true, 503, false}} {
		t.Run(tc.name, func(t *testing.T) {
			repo := accountRepositoryStub{call: func(_ context.Context, op string, _, out any) error {
				switch op {
				case "initialized":
					return accountOutput(out, map[string]bool{"initialized": true})
				case "authenticate":
					if tc.authErr != nil {
						return tc.authErr
					}
					return accountOutput(out, account.Session{ID: "s1", User: account.User{ID: "u1", Username: "alice"}, RecentAuthAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)})
				case "workspaces":
					return accountOutput(out, []account.Workspace{{ID: "w1", Role: "viewer"}})
				default:
					t.Fatalf("unexpected call %s", op)
				}
				return nil
			}}
			r, _ := accountRouter(t, repo)
			req := consoleRequest("GET", "/session", "")
			if tc.cookie {
				req.AddCookie(&http.Cookie{Name: middleware.SessionCookieName, Value: strings.Repeat("a", 43)})
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status %d: %s", w.Code, w.Body)
			}
			if len(w.Result().Cookies()) != 0 {
				t.Fatal("restoration changed browser cookie")
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("session snapshot can be cached")
			}
			if tc.want == 200 {
				var out SessionSnapshot
				if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
					t.Fatal(err)
				}
				if out.Authenticated != tc.authenticated || !out.Initialized || out.Workspaces == nil {
					t.Fatalf("snapshot %+v", out)
				}
				if tc.authenticated && (out.CSRFToken == "" || out.User.ID != "u1") {
					t.Fatal("restored session binding missing")
				}
			}
		})
	}
}
func TestCredentialEndpointsRejectCrossOriginAndBoundHashWork(t *testing.T) {
	repo := accountRepositoryStub{call: func(context.Context, string, any, any) error {
		t.Fatal("rejected request reached account store")
		return nil
	}}
	r, a := accountRouter(t, repo)
	for _, path := range []string{"/login", "/setup", "/invitations/accept", "/password/reset", "/password/change", "/reauth"} {
		for _, origin := range []string{"", "null", "https://other.example"} {
			req := consoleRequest("POST", path, `{}`)
			req.Header.Set("Origin", origin)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != 403 {
				t.Fatalf("%s origin %q status %d", path, origin, w.Code)
			}
		}
	}
	a.passwordSlots <- struct{}{}
	a.passwordSlots <- struct{}{}
	for _, path := range []string{"/login", "/setup", "/invitations/accept", "/password/reset", "/password/change", "/reauth"} {
		req := consoleRequest("POST", path, `{}`)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 429 || w.Header().Get("Retry-After") == "" {
			t.Fatalf("%s did not reject exhausted hash capacity: %d", path, w.Code)
		}
	}
}
func TestCookieLifetimeAndSecurity(t *testing.T) {
	for _, remember := range []bool{false, true} {
		a := NewAccountHTTP(nil, middleware.ConsoleSecurity{})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = consoleRequest("POST", "/login", "")
		s := account.Session{Remember: remember, ExpiresAt: time.Now().Add(7 * 24 * time.Hour)}
		a.cookie(c, s, "new-session")
		cookies := w.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatal("missing cookie")
		}
		cookie := cookies[0]
		if cookie.Name != middleware.SessionCookieName || !cookie.HttpOnly || !cookie.Secure || cookie.Path != "/" || cookie.Domain != "" || cookie.SameSite != http.SameSiteStrictMode {
			t.Fatalf("unsafe cookie %+v", cookie)
		}
		if remember && (cookie.MaxAge <= 0 || cookie.Expires.IsZero()) {
			t.Fatal("remember session lacks persistence")
		}
		if !remember && (cookie.MaxAge != 0 || !cookie.Expires.IsZero()) {
			t.Fatal("temporary session persisted")
		}
	}
}
func TestCredentialBodyAndIPBudgetAreBounded(t *testing.T) {
	repo := accountRepositoryStub{call: func(context.Context, string, any, any) error { t.Fatal("invalid input reached store"); return nil }}
	r, _ := accountRouter(t, repo)
	for i := 0; i < 21; i++ {
		body := `{"unexpected":"value"}`
		if i == 0 {
			body = `{"username":"` + strings.Repeat("a", 9000) + `"}`
		}
		req := consoleRequest("POST", "/login", body)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		want := 400
		if i == 20 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("attempt %d status %d want %d", i, w.Code, want)
		}
	}
}
func TestLogoutRequiresSessionCSRFAndKeepsCookieOnOutage(t *testing.T) {
	repo := accountRepositoryStub{call: func(_ context.Context, op string, _, out any) error {
		if op == "authenticate" {
			return accountOutput(out, account.Session{ID: "s1", User: account.User{ID: "u1"}})
		}
		if op == "logout" {
			return errors.New("offline")
		}
		t.Fatalf("unexpected call %s", op)
		return nil
	}}
	r, _ := accountRouter(t, repo)
	for _, csrf := range []string{"", "wrong"} {
		req := consoleRequest("POST", "/logout", "")
		req.AddCookie(&http.Cookie{Name: middleware.SessionCookieName, Value: strings.Repeat("a", 43)})
		req.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 403 || len(w.Result().Cookies()) != 0 {
			t.Fatalf("CSRF failure status %d cookies %v", w.Code, w.Result().Cookies())
		}
	}
	req := consoleRequest("POST", "/logout", "")
	req.AddCookie(&http.Cookie{Name: middleware.SessionCookieName, Value: strings.Repeat("a", 43)})
	session, _ := account.NewService(repo).Authenticate(context.Background(), strings.Repeat("a", 43))
	req.Header.Set("X-CSRF-Token", session.CSRFToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 503 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("outage erased cookie: %d", w.Code)
	}
}
