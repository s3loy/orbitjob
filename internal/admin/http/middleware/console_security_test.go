package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConsoleOriginAndCookieModes(t *testing.T) {
	cases := []struct {
		name, public, host, origin string
		local, want                bool
	}{
		{"https default", "", "console.example", "https://console.example", false, true},
		{"http default refused", "", "localhost:8080", "http://localhost:8080", false, false},
		{"local explicit", "", "localhost:8080", "http://localhost:8080", true, true},
		{"local IP", "", "127.0.0.1:8080", "http://127.0.0.1:8080", true, true},
		{"local IPv6", "", "[::1]:8080", "http://[::1]:8080", true, true},
		{"nonloopback optin", "", "console.example", "http://console.example", true, false},
		{"null", "", "console.example", "null", false, false},
		{"missing", "", "console.example", "", false, false},
		{"cross origin", "", "console.example", "https://other.example", false, false},
		{"configured", "https://console.example", "console.example", "https://console.example", false, true},
		{"configured host mismatch", "https://console.example", "other.example", "https://console.example", false, false},
		{"configured ignores forwarded", "https://console.example", "console.example", "https://attacker.example", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := ConsoleSecurity{PublicOrigin: tc.public, AllowInsecureLocalhost: tc.local}
			r := httptest.NewRequest(http.MethodPost, "https://"+tc.host+"/", nil)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("X-Forwarded-Host", "attacker.example")
			r.Header.Set("X-Forwarded-Proto", "http")
			if got := s.OriginAllowed(r); got != tc.want {
				t.Fatalf("origin allowed=%v want %v", got, tc.want)
			}
			if !tc.local && s.Insecure(r) {
				t.Fatal("proxy headers disabled Secure")
			}
		})
	}
	for _, origin := range []string{"https://console.example:443", "https://CONSOLE.example", "https://console.example/", "http://console.example", "https://u:p@console.example", "https://console.example?q=x", "https://console.example#x", "null"} {
		if (ConsoleSecurity{PublicOrigin: origin}).Validate() == nil {
			t.Errorf("accepted invalid origin %q", origin)
		}
	}
	if err := (ConsoleSecurity{PublicOrigin: "http://localhost:8080", AllowInsecureLocalhost: true}).Validate(); err != nil {
		t.Fatal(err)
	}
}
func TestCSRFRequiresBothOriginAndSessionBinding(t *testing.T) {
	s := ConsoleSecurity{}
	r := httptest.NewRequest(http.MethodPost, "https://console.example/api/v1/jobs/1/trigger", nil)
	r.Header.Set("Origin", "https://console.example")
	r.Header.Set("X-CSRF-Token", "secret")
	if !s.CheckCSRF(r, "secret") {
		t.Fatal("valid binding denied")
	}
	for _, origin := range []string{"", "null", "https://other.example"} {
		r.Header.Set("Origin", origin)
		if s.CheckCSRF(r, "secret") {
			t.Fatal("invalid origin accepted")
		}
	}
	r.Header.Set("Origin", "https://console.example")
	if s.CheckCSRF(r, "other") {
		t.Fatal("wrong CSRF accepted")
	}
}
func TestSessionCookieRejectsAmbiguousOrWrongModeCookies(t *testing.T) {
	s := ConsoleSecurity{}
	r := httptest.NewRequest(http.MethodGet, "https://console.example/", nil)
	token := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
	if value, ok := s.SessionSecret(r); !ok || value != token {
		t.Fatal("valid cookie rejected")
	}
	r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
	if _, ok := s.SessionSecret(r); ok {
		t.Fatal("duplicate cookie accepted")
	}
	r = httptest.NewRequest(http.MethodGet, "https://console.example/", nil)
	r.AddCookie(&http.Cookie{Name: LocalSessionCookieName, Value: token})
	if _, ok := s.SessionSecret(r); ok {
		t.Fatal("local cookie accepted on production origin")
	}
}
