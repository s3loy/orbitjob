package handler

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"orbitjob/internal/core/app/execute"
)

func makeHTTPTask(payload map[string]any) execute.AssignedTask {
	return execute.AssignedTask{
		InstanceID:     1,
		TenantID:       "default",
		HandlerType:    "http",
		HandlerPayload: payload,
		TimeoutSec:     10,
	}
}

// disableSSRF is a helper that disables URL and transport-level IP blocking for
// tests that use httptest.NewServer (which binds to 127.0.0.1).
func disableSSRF() func() {
	saveValidate := validateCallbackURL
	saveBlocked := isBlockedIP
	validateCallbackURL = func(_ string) error { return nil }
	isBlockedIP = func(_ net.IP) bool { return false }
	return func() {
		validateCallbackURL = saveValidate
		isBlockedIP = saveBlocked
	}
}

func TestHTTP_Success2xx(t *testing.T) {
	defer disableSSRF()()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	h := NewHTTP(srv.Client())
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url":    srv.URL,
		"method": "GET",
	}))
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.ErrorMsg)
	}
	if result.ResultCode != "200" {
		t.Fatalf("expected result_code=200, got %q", result.ResultCode)
	}
}

func TestHTTP_Non2xx(t *testing.T) {
	defer disableSSRF()()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer srv.Close()

	h := NewHTTP(srv.Client())
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url": srv.URL,
	}))
	if result.Success {
		t.Fatal("expected failure for 500")
	}
	if result.ResultCode != "500" {
		t.Fatalf("expected result_code=500, got %q", result.ResultCode)
	}
	if result.ErrorMsg != "server error" {
		t.Fatalf("expected error_msg=%q, got %q", "server error", result.ErrorMsg)
	}
}

func TestHTTP_DefaultMethodIsPOST(t *testing.T) {
	defer disableSSRF()()

	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	h := NewHTTP(srv.Client())
	h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url": srv.URL,
	}))
	if gotMethod != "POST" {
		t.Fatalf("expected default method=POST, got %q", gotMethod)
	}
}

func TestHTTP_HeadersForwarded(t *testing.T) {
	defer disableSSRF()()

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	h := NewHTTP(srv.Client())
	h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url":     srv.URL,
		"headers": map[string]any{"Authorization": "Bearer token-123"},
	}))
	if gotAuth != "Bearer token-123" {
		t.Fatalf("expected Authorization header, got %q", gotAuth)
	}
}

func TestHTTP_Timeout(t *testing.T) {
	defer disableSSRF()()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	h := NewHTTP(srv.Client())
	result := h.Execute(ctx, makeHTTPTask(map[string]any{
		"url": srv.URL,
	}))
	if result.Success {
		t.Fatal("expected failure on timeout")
	}
	if result.ResultCode != "timeout" {
		t.Fatalf("expected result_code=timeout, got %q", result.ResultCode)
	}
}

func TestHTTP_InvalidPayload_MissingURL(t *testing.T) {
	h := NewHTTP(nil)
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{}))
	if result.Success {
		t.Fatal("expected failure")
	}
	if result.ResultCode != "invalid_payload" {
		t.Fatalf("expected result_code=invalid_payload, got %q", result.ResultCode)
	}
}

func TestHTTP_InvalidPayload_BadHeaders(t *testing.T) {
	h := NewHTTP(nil)
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url":     "http://localhost",
		"headers": "not-a-map",
	}))
	if result.Success {
		t.Fatal("expected failure")
	}
	if result.ResultCode != "invalid_payload" {
		t.Fatalf("expected result_code=invalid_payload, got %q", result.ResultCode)
	}
}

func TestHTTP_ConnectionRefused(t *testing.T) {
	defer disableSSRF()()

	h := NewHTTP(&http.Client{})
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url": "http://127.0.0.1:1",
	}))
	if result.Success {
		t.Fatal("expected failure")
	}
	if result.ResultCode != "error" {
		t.Fatalf("expected result_code=error, got %q", result.ResultCode)
	}
}

// ---------------------------------------------------------------------------
// SSRF protection tests
// ---------------------------------------------------------------------------

func TestHTTP_SSRF_Private10(t *testing.T) {
	h := NewHTTP(nil)
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url": "http://10.0.0.1/",
	}))
	if result.Success {
		t.Fatal("expected ssrf_blocked for 10.0.0.0/8")
	}
	if result.ResultCode != "ssrf_blocked" {
		t.Fatalf("expected result_code=ssrf_blocked, got %q", result.ResultCode)
	}
}

func TestHTTP_SSRF_Private172(t *testing.T) {
	h := NewHTTP(nil)
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url": "http://172.16.0.1/",
	}))
	if result.Success {
		t.Fatal("expected ssrf_blocked for 172.16.0.0/12")
	}
	if result.ResultCode != "ssrf_blocked" {
		t.Fatalf("expected result_code=ssrf_blocked, got %q", result.ResultCode)
	}
}

func TestHTTP_SSRF_Private192(t *testing.T) {
	h := NewHTTP(nil)
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url": "http://192.168.1.1/",
	}))
	if result.Success {
		t.Fatal("expected ssrf_blocked for 192.168.0.0/16")
	}
	if result.ResultCode != "ssrf_blocked" {
		t.Fatalf("expected result_code=ssrf_blocked, got %q", result.ResultCode)
	}
}

func TestHTTP_SSRF_Loopback(t *testing.T) {
	h := NewHTTP(nil)
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url": "http://127.0.0.1/",
	}))
	if result.Success {
		t.Fatal("expected ssrf_blocked for 127.0.0.0/8")
	}
	if result.ResultCode != "ssrf_blocked" {
		t.Fatalf("expected result_code=ssrf_blocked, got %q", result.ResultCode)
	}
}

func TestHTTP_SSRF_Metadata(t *testing.T) {
	h := NewHTTP(nil)
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url": "http://169.254.169.254/",
	}))
	if result.Success {
		t.Fatal("expected ssrf_blocked for metadata IP")
	}
	if result.ResultCode != "ssrf_blocked" {
		t.Fatalf("expected result_code=ssrf_blocked, got %q", result.ResultCode)
	}
}

func TestHTTP_SSRF_BadScheme(t *testing.T) {
	h := NewHTTP(nil)
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url": "ftp://example.com/",
	}))
	if result.Success {
		t.Fatal("expected ssrf_blocked for non-http scheme")
	}
	if result.ResultCode != "ssrf_blocked" {
		t.Fatalf("expected result_code=ssrf_blocked, got %q", result.ResultCode)
	}
}

func TestHTTP_AllowLoopbackForTest(t *testing.T) {
	save := allowLoopback
	allowLoopback = true
	defer func() { allowLoopback = save }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	h := NewHTTP(srv.Client())
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url":    srv.URL,
		"method": "GET",
	}))
	if !result.Success {
		t.Fatalf("expected success with allowLoopback=true, got error: %s", result.ErrorMsg)
	}
	if result.ResultCode != "200" {
		t.Fatalf("expected result_code=200, got %q", result.ResultCode)
	}
}

func TestHTTP_AllowLoopbackForTest_PrivateStillBlocked(t *testing.T) {
	save := allowLoopback
	allowLoopback = true
	defer func() { allowLoopback = save }()

	h := NewHTTP(nil)
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url": "http://10.0.0.1/",
	}))
	if result.Success {
		t.Fatal("expected private IP to remain blocked when loopback override is enabled")
	}
	if result.ResultCode != "ssrf_blocked" {
		t.Fatalf("expected result_code=ssrf_blocked, got %q", result.ResultCode)
	}
}

func TestHTTP_SSRF_RedirectDisabled(t *testing.T) {
	defer disableSSRF()()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/target", http.StatusMovedPermanently)
	}))
	defer srv.Close()

	h := NewHTTP(srv.Client())
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url": srv.URL,
	}))
	if result.Success {
		t.Fatal("expected non-2xx (301) since redirects are disabled")
	}
	if result.ResultCode != "301" {
		t.Fatalf("expected result_code=301 (redirect not followed), got %q", result.ResultCode)
	}
}

// ---------------------------------------------------------------------------
// validateURLImpl edge case tests
// ---------------------------------------------------------------------------

func TestValidateURLImpl_BlockedIPs(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		wantErr bool
	}{
		{"10.0.0.0/8 lower", "http://10.0.0.1/", true},
		{"10.0.0.0/8 upper", "http://10.255.255.255/", true},
		{"172.16.0.0/12 lower", "http://172.16.0.1/", true},
		{"172.16.0.0/12 upper", "http://172.31.255.255/", true},
		{"192.168.0.0/16", "http://192.168.1.1/", true},
		{"127.0.0.0/8 loopback", "http://127.0.0.1/", true},
		{"127.0.0.0/8 other", "http://127.99.88.77/", true},
		{"metadata IP", "http://169.254.169.254/", true},
		{"IPv6 loopback ::1", "http://[::1]/", true},
		{"IPv6 link-local fe80::1", "http://[fe80::1]/", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateURLImpl(tt.rawURL)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for %q", tt.rawURL)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for %q: %v", tt.rawURL, err)
			}
		})
	}
}

func TestValidateURLImpl_InvalidURL(t *testing.T) {
	// A URL with an invalid control character that url.Parse rejects.
	err := validateURLImpl("http://example.com/\x00")
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
	if !strings.Contains(err.Error(), "invalid URL") {
		t.Errorf("expected 'invalid URL' in error, got: %v", err)
	}
}

func TestValidateURLImpl_BadScheme(t *testing.T) {
	err := validateURLImpl("ftp://example.com/")
	if err == nil {
		t.Fatal("expected error for non-http scheme")
	}
	if !strings.Contains(err.Error(), "only http/https schemes allowed") {
		t.Errorf("expected scheme error, got: %v", err)
	}
}

func TestValidateURLImpl_NoHost(t *testing.T) {
	err := validateURLImpl("http:///path")
	if err == nil {
		t.Fatal("expected error for URL without host")
	}
}

func TestValidateURLImpl_MixedDNSAnswers(t *testing.T) {
	save := lookupIP
	defer func() { lookupIP = save }()

	lookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8"), net.ParseIP("10.0.0.1")}, nil
	}

	err := validateURLImpl("http://example.com/")
	if err == nil {
		t.Fatal("expected error when any resolved IP is blocked")
	}
	if !strings.Contains(err.Error(), "blocked IP") {
		t.Fatalf("expected blocked IP error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// newSecureTransport tests
// ---------------------------------------------------------------------------

type mockRoundTripper struct{}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("mock")
}

func TestNewSecureTransport_NilBase(t *testing.T) {
	rt := newSecureTransport(nil)
	if rt == nil {
		t.Fatal("expected non-nil transport")
	}
	tr, ok := rt.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", rt)
	}
	if tr.DialContext == nil {
		t.Fatal("expected DialContext to be set")
	}
}

func TestNewSecureTransport_NonTransport(t *testing.T) {
	dummy := &mockRoundTripper{}
	rt := newSecureTransport(dummy)
	if rt != dummy {
		t.Fatal("expected same RoundTripper returned for non-Transport")
	}
}

func TestNewSecureTransport_DialContext_BlocksPrivateIP(t *testing.T) {
	rt := newSecureTransport(nil)
	tr := rt.(*http.Transport)

	_, err := tr.DialContext(context.Background(), "tcp", "10.0.0.1:80")
	if err == nil {
		t.Fatal("expected error for blocked IP")
	}
	if !strings.Contains(err.Error(), "blocked IP") {
		t.Errorf("expected 'blocked IP' error, got: %v", err)
	}
}

func TestNewSecureTransport_DialContext_BlocksLoopback(t *testing.T) {
	rt := newSecureTransport(nil)
	tr := rt.(*http.Transport)

	_, err := tr.DialContext(context.Background(), "tcp", "127.0.0.1:80")
	if err == nil {
		t.Fatal("expected error for loopback")
	}
	if !strings.Contains(err.Error(), "blocked IP") {
		t.Errorf("expected 'blocked IP' error, got: %v", err)
	}
}

func TestNewSecureTransport_DialContext_BlocksMetadata(t *testing.T) {
	rt := newSecureTransport(nil)
	tr := rt.(*http.Transport)

	_, err := tr.DialContext(context.Background(), "tcp", "169.254.169.254:80")
	if err == nil {
		t.Fatal("expected error for metadata IP")
	}
	if !strings.Contains(err.Error(), "blocked IP") {
		t.Errorf("expected 'blocked IP' error, got: %v", err)
	}
}

func TestNewSecureTransport_DialContext_AllowsPublicIP(t *testing.T) {
	save := isBlockedIP
	isBlockedIP = func(ip net.IP) bool { return false }
	defer func() { isBlockedIP = save }()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()

	rt := newSecureTransport(nil)
	tr := rt.(*http.Transport)

	conn, err := tr.DialContext(context.Background(), "tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("expected successful dial, got: %v", err)
	}
	_ = conn.Close()
}

func TestNewSecureTransport_PreservesTLSConfig(t *testing.T) {
	base := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	rt := newSecureTransport(base)
	tr := rt.(*http.Transport)

	if tr.TLSClientConfig == nil {
		t.Fatal("expected TLSClientConfig to be preserved")
	}
	if !tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify to be true")
	}
}

// ---------------------------------------------------------------------------
// parseHTTPPayload edge case tests
// ---------------------------------------------------------------------------

func TestParseHTTPPayload_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]any
		wantErr string
	}{
		{
			name:    "url is not a string (int)",
			payload: map[string]any{"url": 42},
			wantErr: "url must be a non-empty string",
		},
		{
			name:    "url is empty string",
			payload: map[string]any{"url": ""},
			wantErr: "url must be a non-empty string",
		},
		{
			name:    "method is not a string",
			payload: map[string]any{"url": "http://example.com", "method": 123},
			wantErr: "method must be a non-empty string",
		},
		{
			name:    "method is empty string",
			payload: map[string]any{"url": "http://example.com", "method": ""},
			wantErr: "method must be a non-empty string",
		},
		{
			name:    "headers value is not a string",
			payload: map[string]any{"url": "http://example.com", "headers": map[string]any{"X-Key": 42}},
			wantErr: `headers["X-Key"] must be a string`,
		},
		{
			name:    "body is not a string",
			payload: map[string]any{"url": "http://example.com", "body": []int{1, 2, 3}},
			wantErr: "body must be a string",
		},
		{
			name: "valid payload with all fields",
			payload: map[string]any{
				"url":     "http://example.com",
				"method":  "PUT",
				"headers": map[string]any{"Content-Type": "application/json"},
				"body":    `{"key": "value"}`,
			},
			wantErr: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawURL, method, headers, body, err := parseHTTPPayload(tt.payload)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if rawURL != "http://example.com" {
				t.Errorf("expected url=http://example.com, got %q", rawURL)
			}
			if method != "PUT" {
				t.Errorf("expected method=PUT, got %q", method)
			}
			if headers["Content-Type"] != "application/json" {
				t.Errorf("expected Content-Type header, got %v", headers)
			}
			if body != `{"key": "value"}` {
				t.Errorf("expected body, got %q", body)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// validateURLImpl success and edge case tests
// ---------------------------------------------------------------------------

func TestValidateURLImpl_Success(t *testing.T) {
	// 8.8.8.8 is a public IP not in any blocked range.
	err := validateURLImpl("http://8.8.8.8/")
	if err != nil {
		t.Fatalf("expected success for public IP, got: %v", err)
	}
}

func TestValidateURLImpl_IPv6Loopback(t *testing.T) {
	// IPv6 loopback ::1 should be blocked now that IPv6 SSRF protection is in place.
	err := validateURLImpl("http://[::1]/")
	if err == nil {
		t.Fatal("expected ssrf_blocked for IPv6 loopback ::1")
	}
	if !strings.Contains(err.Error(), "blocked IP") {
		t.Errorf("expected 'blocked IP' error, got: %v", err)
	}
}

func TestValidateURLImpl_IPv6LinkLocal(t *testing.T) {
	// fe80::/10 link-local addresses should be blocked now that IPv6 SSRF protection is in place.
	err := validateURLImpl("http://[fe80::1]/")
	if err == nil {
		t.Fatal("expected ssrf_blocked for IPv6 link-local fe80::1")
	}
	if !strings.Contains(err.Error(), "blocked IP") {
		t.Errorf("expected 'blocked IP' error, got: %v", err)
	}
}

func TestValidateURLImpl_URLCredentials(t *testing.T) {
	// URL with userinfo — url.Parse handles credentials,
	// Hostname() strips them. Should resolve same as without.
	err := validateURLImpl("http://user:pass@8.8.8.8/")
	if err != nil {
		t.Fatalf("expected success for URL with credentials, got: %v", err)
	}
}

func TestValidateURLImpl_HTTPS(t *testing.T) {
	// Verify the https scheme branch of the compound condition.
	err := validateURLImpl("https://8.8.8.8/")
	if err != nil {
		t.Fatalf("expected success for https URL, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// newSecureTransport additional coverage tests
// ---------------------------------------------------------------------------

func TestNewSecureTransport_DialContext_BadAddress(t *testing.T) {
	rt := newSecureTransport(nil)
	tr := rt.(*http.Transport)

	_, err := tr.DialContext(context.Background(), "tcp", "bad-address-no-colon")
	if err == nil {
		t.Fatal("expected error for address without port separator")
	}
}

func TestNewSecureTransport_DialContext_UsesBareDialer(t *testing.T) {
	// When the base transport has nil DialContext, the wrapper
	// must fall back to a bare net.Dialer{} for actual dialing.
	save := isBlockedIP
	isBlockedIP = func(ip net.IP) bool { return false }
	defer func() { isBlockedIP = save }()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()

	base := &http.Transport{DialContext: nil}
	rt := newSecureTransport(base)
	tr := rt.(*http.Transport)

	conn, err := tr.DialContext(context.Background(), "tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("expected successful dial via bare Dialer, got: %v", err)
	}
	_ = conn.Close()
}

// ---------------------------------------------------------------------------
// Execute additional coverage tests
// ---------------------------------------------------------------------------

func TestHTTP_WithBody(t *testing.T) {
	defer disableSSRF()()

	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf [1024]byte
		n, _ := r.Body.Read(buf[:])
		gotBody = string(buf[:n])
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	h := NewHTTP(srv.Client())
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url":  srv.URL,
		"body": "hello-world-payload",
	}))
	if !result.Success {
		t.Fatalf("expected success, got: %s", result.ErrorMsg)
	}
	if gotBody != "hello-world-payload" {
		t.Fatalf("expected body=%q, got %q", "hello-world-payload", gotBody)
	}
}

func TestHTTP_InvalidMethod(t *testing.T) {
	// A method with a space is rejected by http.NewRequestWithContext
	// but passes parseHTTPPayload (which only checks non-empty string).
	defer disableSSRF()()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	h := NewHTTP(srv.Client())
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url":    srv.URL,
		"method": "BAD METHOD",
	}))
	if result.Success {
		t.Fatal("expected failure for invalid method")
	}
	if result.ResultCode != "error" {
		t.Fatalf("expected result_code=error, got %q", result.ResultCode)
	}
	if !strings.Contains(result.ErrorMsg, "build request") {
		t.Errorf("expected 'build request' in error, got: %s", result.ErrorMsg)
	}
}

func TestHTTP_ResponseBodyTruncation(t *testing.T) {
	// Verify that large non-2xx response bodies are truncated at maxResponseBodyBytes.
	defer disableSSRF()()

	bigBody := strings.Repeat("x", maxResponseBodyBytes+100)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(bigBody))
	}))
	defer srv.Close()

	h := NewHTTP(srv.Client())
	result := h.Execute(context.Background(), makeHTTPTask(map[string]any{
		"url": srv.URL,
	}))
	if result.Success {
		t.Fatal("expected failure for 400")
	}
	if result.ResultCode != "400" {
		t.Fatalf("expected result_code=400, got %q", result.ResultCode)
	}
	if len(result.ErrorMsg) != maxResponseBodyBytes {
		t.Errorf("expected error_msg truncated to %d bytes, got %d", maxResponseBodyBytes, len(result.ErrorMsg))
	}
}

// ---------------------------------------------------------------------------
// validateURLImpl defensive branch: empty IP list
// ---------------------------------------------------------------------------

func TestValidateURLImpl_NoIPsResolved(t *testing.T) {
	// net.LookupIP never returns empty slice without error, but validateURLImpl
	// has a defensive check for this case. Mock lookupIP to hit it.
	save := lookupIP
	lookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{}, nil
	}
	defer func() { lookupIP = save }()

	err := validateURLImpl("http://example.com/")
	if err == nil {
		t.Fatal("expected error for empty IP list")
	}
	if !strings.Contains(err.Error(), "no IP addresses resolved") {
		t.Errorf("expected 'no IP addresses resolved' error, got: %v", err)
	}
}
