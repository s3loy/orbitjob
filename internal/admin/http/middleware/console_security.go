package middleware

import (
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const SessionCookieName = "__Host-orbitjob_session"
const LocalSessionCookieName = "orbitjob_session_local"

// ConsoleSecurity binds cookie authentication to the public browser origin.
// Forwarded headers never select either the origin or cookie security mode.
type ConsoleSecurity struct {
	PublicOrigin           string
	AllowInsecureLocalhost bool
}

func (s ConsoleSecurity) Validate() error {
	if s.PublicOrigin == "" {
		return nil
	}
	u, err := url.Parse(s.PublicOrigin)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.Opaque != "" || u.String() != s.PublicOrigin || u.Host != strings.ToLower(u.Host) || (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		return fmt.Errorf("CONSOLE_PUBLIC_ORIGIN must be an exact origin without path, credentials, query or fragment")
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && s.AllowInsecureLocalhost && loopbackHost(u.Host) {
		return nil
	}
	return fmt.Errorf("CONSOLE_PUBLIC_ORIGIN requires HTTPS; HTTP requires explicit insecure loopback mode")
}
func loopbackHost(host string) bool {
	if strings.ContainsAny(host, "/\\@ \t\r\n") {
		return false
	}
	h := host
	if x, _, err := net.SplitHostPort(host); err == nil {
		h = x
	}
	h = strings.Trim(h, "[]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
func (s ConsoleSecurity) Insecure(r *http.Request) bool {
	if !s.AllowInsecureLocalhost || !loopbackHost(r.Host) {
		return false
	}
	if s.PublicOrigin != "" {
		return strings.HasPrefix(s.PublicOrigin, "http://")
	}
	return true
}
func (s ConsoleSecurity) OriginAllowed(r *http.Request) bool {
	if len(r.Header.Values("Origin")) != 1 {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" || origin == "null" {
		return false
	}
	expected := s.PublicOrigin
	if expected != "" {
		u, err := url.Parse(expected)
		if err != nil || r.Host != u.Host {
			return false
		}
	}
	if expected == "" {
		scheme := "https"
		if s.Insecure(r) {
			scheme = "http"
		}
		expected = scheme + "://" + r.Host
	}
	if origin != expected {
		return false
	}
	if strings.HasPrefix(origin, "http://") && (!s.Insecure(r) || !loopbackHost(r.Host)) {
		return false
	}
	return true
}
func SafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}
func (s ConsoleSecurity) CheckCSRF(r *http.Request, token string) bool {
	supplied := r.Header.Get("X-CSRF-Token")
	return s.OriginAllowed(r) && token != "" && len(supplied) == len(token) && subtle.ConstantTimeCompare([]byte(supplied), []byte(token)) == 1
}

// SessionSecret rejects duplicate cookies rather than choosing an ambiguous path.
func (s ConsoleSecurity) CookieName(r *http.Request) string {
	if s.Insecure(r) {
		return LocalSessionCookieName
	}
	return SessionCookieName
}
func (s ConsoleSecurity) SessionSecret(r *http.Request) (string, bool) {
	var value string
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == s.CookieName(r) {
			count++
			value = cookie.Value
		}
	}
	return value, count == 1 && len(value) == 43
}
