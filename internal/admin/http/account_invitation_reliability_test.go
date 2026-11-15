package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"orbitjob/internal/admin/app/account"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/platform/scan"
)

func invitationCSRF(token string) string {
	hash := sha256.Sum256([]byte("orbitjob-session-csrf:" + token))
	return hex.EncodeToString(hash[:])
}

func TestInvitationAcceptanceUsesAuthenticatedIdentity(t *testing.T) {
	for _, username := range []string{"", "alice", " Alice "} {
		t.Run("username="+username, func(t *testing.T) {
			token := strings.Repeat("a", 43)
			session := account.Session{ID: scan.GenerateID(), User: account.User{ID: scan.GenerateID(), Username: "alice"}, RecentAuthAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
			accepted := false
			repo := accountRepositoryStub{call: func(_ context.Context, op string, in, out any) error {
				switch op {
				case "authenticate":
					return accountOutput(out, session)
				case "session_actor":
					if in.(map[string]any)["session_id"] != session.ID {
						t.Fatal("identity lookup lost authenticated session")
					}
					return accountOutput(out, session.User)
				case "accept_invitation":
					params := in.(map[string]any)
					if params["session_id"] != session.ID || params["username"] != "alice" || params["password_hash"] != "" {
						t.Fatal("token-only invitation acceptance lost session identity")
					}
					accepted = true
					return accountOutput(out, session.User)
				case "initialized":
					return accountOutput(out, map[string]bool{"initialized": true})
				case "workspaces":
					if !accepted {
						t.Fatal("snapshot refreshed before membership change")
					}
					return accountOutput(out, []account.Workspace{{ID: scan.GenerateID(), Role: account.RoleViewer}})
				default:
					t.Fatalf("unexpected operation %s", op)
					return nil
				}
			}}
			router, _ := accountRouter(t, repo)
			body, err := json.Marshal(map[string]string{"token": strings.Repeat("b", 43), "username": username})
			if err != nil {
				t.Fatal(err)
			}
			req := consoleRequest(http.MethodPost, "/invitations/accept", string(body))
			req.AddCookie(&http.Cookie{Name: middleware.SessionCookieName, Value: token})
			req.Header.Set("X-CSRF-Token", invitationCSRF(token))
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusOK || !accepted {
				t.Fatalf("status=%d accepted=%v body=%s", w.Code, accepted, w.Body)
			}
			if len(w.Result().Cookies()) != 0 {
				t.Fatal("accepting membership rotated an existing session")
			}
			var snapshot SessionSnapshot
			if err = json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil || !snapshot.Authenticated || snapshot.User.ID != session.User.ID || len(snapshot.Workspaces) != 1 {
				t.Fatalf("snapshot=%+v err=%v", snapshot, err)
			}
		})
	}
}

func TestInvitationAcceptanceRejectsForgedIdentityAndUntrustedSessions(t *testing.T) {
	for _, tc := range []struct {
		name, cookie, csrf, extra string
		duplicate                 bool
		authErr                   error
		want                      int
	}{
		{name: "conflicting username", cookie: "valid", csrf: "valid", extra: `,"username":"mallory"`, want: http.StatusUnauthorized},
		{name: "forged session id", cookie: "valid", csrf: "valid", extra: `,"session_id":"forged"`, want: http.StatusBadRequest},
		{name: "missing csrf", cookie: "valid", want: http.StatusForbidden},
		{name: "incorrect csrf", cookie: "valid", csrf: "wrong", want: http.StatusForbidden},
		{name: "malformed cookie", cookie: "bad", csrf: "valid", want: http.StatusUnauthorized},
		{name: "unparseable cookie", cookie: "unparseable", csrf: "valid", want: http.StatusUnauthorized},
		{name: "unparseable cookie with whitespace", cookie: "unparseable whitespace", csrf: "valid", want: http.StatusUnauthorized},
		{name: "duplicate cookies", cookie: "valid", csrf: "valid", duplicate: true, want: http.StatusUnauthorized},
		{name: "unparseable duplicate cookie", cookie: "unparseable", csrf: "valid", duplicate: true, want: http.StatusUnauthorized},
		{name: "expired or forged cookie", cookie: "valid", csrf: "valid", authErr: account.ErrUnauthenticated, want: http.StatusUnauthorized},
		{name: "account store unavailable", cookie: "valid", csrf: "valid", authErr: errors.New("offline"), want: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := strings.Repeat("a", 43)
			repo := accountRepositoryStub{call: func(_ context.Context, op string, _, out any) error {
				if op != "authenticate" {
					t.Fatalf("rejected request reached %s", op)
				}
				if tc.authErr != nil {
					return tc.authErr
				}
				return accountOutput(out, account.Session{ID: scan.GenerateID(), User: account.User{ID: scan.GenerateID(), Username: "alice"}})
			}}
			router, _ := accountRouter(t, repo)
			body := `{"token":"` + strings.Repeat("b", 43) + `","password":"An invitation passphrase for OrbitJob 42"` + tc.extra + `}`
			req := consoleRequest(http.MethodPost, "/invitations/accept", body)
			cookie := token
			if tc.cookie == "bad" {
				cookie = "bad"
			}
			req.AddCookie(&http.Cookie{Name: middleware.SessionCookieName, Value: cookie})
			if tc.cookie == "unparseable" {
				req.Header.Set("Cookie", middleware.SessionCookieName+`=bad\cookie`)
			}
			if tc.cookie == "unparseable whitespace" {
				req.Header.Set("Cookie", middleware.SessionCookieName+` =bad\cookie`)
			}
			if tc.duplicate {
				req.AddCookie(&http.Cookie{Name: middleware.SessionCookieName, Value: token})
			}
			if tc.csrf == "valid" {
				req.Header.Set("X-CSRF-Token", invitationCSRF(token))
			} else if tc.csrf != "" {
				req.Header.Set("X-CSRF-Token", tc.csrf)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tc.want || len(w.Result().Cookies()) != 0 {
				t.Fatalf("status=%d want=%d cookies=%v body=%s", w.Code, tc.want, w.Result().Cookies(), w.Body)
			}
		})
	}
}

func TestInvitationAcceptanceCreatesAndSignsInAnonymousAccount(t *testing.T) {
	user := account.User{ID: scan.GenerateID(), Username: "new-member", DisplayName: "New Member"}
	password := "An invitation passphrase for OrbitJob 42"
	var passwordHash string
	accepted := false
	repo := accountRepositoryStub{call: func(_ context.Context, op string, in, out any) error {
		switch op {
		case "accept_invitation":
			params := in.(map[string]any)
			if params["session_id"] != "" || params["username"] != user.Username || params["display_name"] != user.DisplayName {
				t.Fatal("anonymous invitation lost new account details")
			}
			passwordHash = params["password_hash"].(string)
			accepted = true
			return accountOutput(out, user)
		case "credential":
			if !accepted {
				t.Fatal("login occurred before account creation")
			}
			return accountOutput(out, map[string]any{"user": user, "password_hash": passwordHash})
		case "login":
			return accountOutput(out, account.Session{ID: scan.GenerateID(), User: user, ExpiresAt: time.Now().Add(8 * time.Hour), RecentAuthAt: time.Now()})
		case "initialized":
			return accountOutput(out, map[string]bool{"initialized": true})
		case "workspaces":
			return accountOutput(out, []account.Workspace{{ID: scan.GenerateID(), Role: account.RoleViewer}})
		default:
			t.Fatalf("unexpected operation %s", op)
			return nil
		}
	}}
	router, _ := accountRouter(t, repo)
	body, err := json.Marshal(SetupAccountRequest{Token: strings.Repeat("b", 43), Username: user.Username, Password: password, DisplayName: user.DisplayName})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, consoleRequest(http.MethodPost, "/invitations/accept", string(body)))
	if w.Code != http.StatusOK || !accepted {
		t.Fatalf("status=%d accepted=%v body=%s", w.Code, accepted, w.Body)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != middleware.SessionCookieName || !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatalf("anonymous account did not receive a secure session: %+v", cookies)
	}
	var snapshot SessionSnapshot
	if err = json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil || !snapshot.Authenticated || snapshot.User.ID != user.ID || snapshot.CSRFToken == "" {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
}
