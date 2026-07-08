package handler

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"orbitjob/internal/core/app/execute"
)

const maxResponseBodyBytes = 4096

const allowLoopbackEnv = "ORBITJOB_HTTP_HANDLER_ALLOW_LOOPBACK"

// loopbackNetworks defines IP ranges that are blocked by default but may be
// allowed in test environments via ORBITJOB_HTTP_HANDLER_ALLOW_LOOPBACK=true.
var loopbackNetworks = []*net.IPNet{
	{IP: net.IPv4(127, 0, 0, 0), Mask: net.CIDRMask(8, 32)},
	{IP: net.IP{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}, Mask: net.CIDRMask(128, 128)}, // ::1/128 IPv6 loopback
}

// privateNetworks defines IP ranges blocked for SSRF protection.
var privateNetworks = []*net.IPNet{
	{IP: net.IPv4(10, 0, 0, 0), Mask: net.CIDRMask(8, 32)},
	{IP: net.IPv4(172, 16, 0, 0), Mask: net.CIDRMask(12, 32)},
	{IP: net.IPv4(192, 168, 0, 0), Mask: net.CIDRMask(16, 32)},
	{IP: net.IPv4(169, 254, 0, 0), Mask: net.CIDRMask(16, 32)}, // link-local
	{IP: net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, Mask: net.CIDRMask(10, 128)}, // fe80::/10 IPv6 link-local
	{IP: net.IP{0xfd, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, Mask: net.CIDRMask(8, 128)}, // fd00::/8 IPv6 ULA
}

var metadataIP = net.IPv4(169, 254, 169, 254)

// allowLoopback returns true when the test-only environment variable
// ORBITJOB_HTTP_HANDLER_ALLOW_LOOPBACK is set to "true". Production code
// must never set this variable; it exists only to let integration tests
// exercise the real HTTP handler against httptest.NewServer endpoints.
func allowLoopback() bool {
	return os.Getenv(allowLoopbackEnv) == "true"
}

// validateCallbackURL is overridable for tests.
var validateCallbackURL = validateURLImpl

// lookupIP is overridable for tests.
var lookupIP = net.LookupIP

// isBlockedIP is overridable for tests. The transport-level check uses this
// to enforce DNS rebinding protection at connection time.
var isBlockedIP = func(ip net.IP) bool {
	if ip.Equal(metadataIP) {
		return true
	}
	for _, network := range privateNetworks {
		if network.Contains(ip) {
			return true
		}
	}
	if !allowLoopback() {
		for _, network := range loopbackNetworks {
			if network.Contains(ip) {
				return true
			}
		}
	}
	return false
}

func validateURLImpl(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("only http/https schemes allowed, got %q", u.Scheme)
	}

	host := u.Hostname()
	ips, err := lookupIP(host)
	if err != nil {
		return fmt.Errorf("DNS resolution failed for %q: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("no IP addresses resolved for %q", host)
	}

	for _, ip := range ips {
		if isBlockedIP(ip) {
			return fmt.Errorf("URL resolves to blocked IP %s", ip)
		}
	}

	return nil
}

// newSecureTransport returns an http.RoundTripper that validates the resolved
// IP at connection time (DNS rebinding protection).
func newSecureTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	if t, ok := base.(*http.Transport); ok {
		t2 := t.Clone()
		baseDial := t2.DialContext
		t2.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ip := net.ParseIP(host)
			if ip != nil && isBlockedIP(ip) {
				return nil, fmt.Errorf("connection to blocked IP %s refused", ip)
			}
			if baseDial != nil {
				return baseDial(ctx, network, addr)
			}
			d := &net.Dialer{}
			return d.DialContext(ctx, network, addr)
		}
		return t2
	}
	return base
}

type HTTP struct {
	client  *http.Client
	secure  http.RoundTripper
	timeout time.Duration
}

func NewHTTP(client *http.Client) *HTTP {
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTP{
		client:  client,
		secure:  newSecureTransport(client.Transport),
		timeout: client.Timeout,
	}
}

func (h *HTTP) Execute(ctx context.Context, task execute.AssignedTask) execute.Result {
	rawURL, method, headers, body, err := parseHTTPPayload(task.HandlerPayload)
	if err != nil {
		return execute.Result{
			Success:    false,
			ResultCode: "invalid_payload",
			ErrorMsg:   err.Error(),
		}
	}

	if err := validateCallbackURL(rawURL); err != nil {
		return execute.Result{
			Success:    false,
			ResultCode: "ssrf_blocked",
			ErrorMsg:   err.Error(),
		}
	}

	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
	if err != nil {
		return execute.Result{
			Success:    false,
			ResultCode: "error",
			ErrorMsg:   fmt.Sprintf("build request: %v", err),
		}
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{
		Transport:     h.secure,
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       h.timeout,
	}

	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return execute.Result{
				Success:    false,
				ResultCode: "timeout",
				ErrorMsg:   fmt.Sprintf("request timed out: %v", ctx.Err()),
			}
		}
		return execute.Result{
			Success:    false,
			ResultCode: "error",
			ErrorMsg:   err.Error(),
		}
	}
	defer func() { _ = resp.Body.Close() }()

	statusCode := fmt.Sprintf("%d", resp.StatusCode)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return execute.Result{
			Success:    true,
			ResultCode: statusCode,
		}
	}

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes))
	return execute.Result{
		Success:    false,
		ResultCode: statusCode,
		ErrorMsg:   truncate(string(respBody), maxResponseBodyBytes),
	}
}

func parseHTTPPayload(p map[string]any) (rawURL, method string, headers map[string]string, body string, err error) {
	urlRaw, ok := p["url"]
	if !ok {
		return "", "", nil, "", fmt.Errorf("missing required field: url")
	}
	rawURL, ok = urlRaw.(string)
	if !ok || rawURL == "" {
		return "", "", nil, "", fmt.Errorf("url must be a non-empty string")
	}

	method = "POST"
	if methodRaw, ok := p["method"]; ok {
		method, ok = methodRaw.(string)
		if !ok || method == "" {
			return "", "", nil, "", fmt.Errorf("method must be a non-empty string")
		}
	}

	if headersRaw, ok := p["headers"]; ok {
		headersMap, ok := headersRaw.(map[string]any)
		if !ok {
			return "", "", nil, "", fmt.Errorf("headers must be a map of string to string")
		}
		headers = make(map[string]string, len(headersMap))
		for k, v := range headersMap {
			s, ok := v.(string)
			if !ok {
				return "", "", nil, "", fmt.Errorf("headers[%q] must be a string", k)
			}
			headers[k] = s
		}
	}

	if bodyRaw, ok := p["body"]; ok {
		body, ok = bodyRaw.(string)
		if !ok {
			return "", "", nil, "", fmt.Errorf("body must be a string")
		}
	}

	return rawURL, method, headers, body, nil
}
