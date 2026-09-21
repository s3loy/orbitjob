package check

import (
	"strings"
	"testing"
)

// TestNormalizeProbeConfigDefaults locks the defaults a minimal config gets:
// GET and 200 are the probe semantics the rendering bakes into the Job args,
// so a config the admin API accepted must produce exactly these values.
func TestNormalizeProbeConfigDefaults(t *testing.T) {
	probe, err := NormalizeProbeConfig(map[string]any{"url": "http://api.local/health"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if probe.URL != "http://api.local/health" {
		t.Fatalf("url = %q, want the stored url", probe.URL)
	}
	if probe.Method != DefaultProbeMethod {
		t.Fatalf("method = %q, want default %q", probe.Method, DefaultProbeMethod)
	}
	if probe.ExpectedStatus != DefaultExpectedStatus {
		t.Fatalf("expected_status = %d, want default %d", probe.ExpectedStatus, DefaultExpectedStatus)
	}
}

// TestNormalizeProbeConfigAccepts covers the shapes the boundary must accept:
// every admitted method, both schemes, and the numeric encodings JSON and Go
// callers produce.
func TestNormalizeProbeConfigAccepts(t *testing.T) {
	for _, method := range ProbeMethods {
		probe, err := NormalizeProbeConfig(map[string]any{
			"url":             "https://api.local/health",
			"method":          method,
			"expected_status": 404,
		})
		if err != nil {
			t.Fatalf("method %s rejected: %v", method, err)
		}
		if probe.Method != method {
			t.Fatalf("method = %q, want %q", probe.Method, method)
		}
		if probe.ExpectedStatus != 404 {
			t.Fatalf("expected_status = %d, want 404", probe.ExpectedStatus)
		}
	}

	for name, cfg := range map[string]map[string]any{
		"integer status":    {"url": "http://a.local/", "expected_status": 204},
		"float status":      {"url": "http://a.local/", "expected_status": float64(302)},
		"lowercase method":  {"url": "http://a.local/", "method": "get"},
		"explicit nulls":    {"url": "http://a.local/", "method": nil, "expected_status": nil},
		"url with userinfo": {"url": "https://user:pass@a.local/health?probe=1"},
	} {
		if _, err := NormalizeProbeConfig(cfg); err != nil {
			t.Fatalf("%s rejected: %v", name, err)
		}
	}
}

// TestNormalizeProbeConfigRejects covers every way a config can be malformed.
// Each rejection names the exact field so the API error body can attribute it.
func TestNormalizeProbeConfigRejects(t *testing.T) {
	longURL := "http://a.local/" + strings.Repeat("p", 2048)
	tests := []struct {
		name      string
		cfg       map[string]any
		wantField string
	}{
		{"nil config", nil, "check_config.url"},
		{"empty config", map[string]any{}, "check_config.url"},
		{"missing url", map[string]any{"method": "GET"}, "check_config.url"},
		{"empty url", map[string]any{"url": ""}, "check_config.url"},
		{"blank url", map[string]any{"url": "   "}, "check_config.url"},
		{"non-string url", map[string]any{"url": 8080}, "check_config.url"},
		{"relative url", map[string]any{"url": "api.local/health"}, "check_config.url"},
		{"scheme only", map[string]any{"url": "http://"}, "check_config.url"},
		{"ftp scheme", map[string]any{"url": "ftp://a.local/"}, "check_config.url"},
		{"javascript scheme", map[string]any{"url": "javascript:alert(1)"}, "check_config.url"},
		{"non-string method", map[string]any{"url": "http://a.local/", "method": 42}, "check_config.method"},
		{"post method", map[string]any{"url": "http://a.local/", "method": "POST"}, "check_config.method"},
		{"delete method", map[string]any{"url": "http://a.local/", "method": "DELETE"}, "check_config.method"},
		{"bogus method", map[string]any{"url": "http://a.local/", "method": "TRACE"}, "check_config.method"},
		{"string status", map[string]any{"url": "http://a.local/", "expected_status": "200 OK"}, "check_config.expected_status"},
		{"status below range", map[string]any{"url": "http://a.local/", "expected_status": 99}, "check_config.expected_status"},
		{"status above range", map[string]any{"url": "http://a.local/", "expected_status": 600}, "check_config.expected_status"},
		{"fractional status", map[string]any{"url": "http://a.local/", "expected_status": 200.5}, "check_config.expected_status"},
		{"bool status", map[string]any{"url": "http://a.local/", "expected_status": true}, "check_config.expected_status"},
		{"absurdly long url", map[string]any{"url": longURL}, "check_config.url"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeProbeConfig(tt.cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			assertValidationField(t, err, tt.wantField)
		})
	}
}

// TestCheckSourceUIDRoundTrip pins the revision/ledger/SLI identity derivation:
// one direction builds it from the row id, the other parses it back, and
// anything else reports false so callers can route on the result.
func TestCheckSourceUIDRoundTrip(t *testing.T) {
	if got, want := CheckSourceUID(42), "check-42"; got != want {
		t.Fatalf("CheckSourceUID(42) = %q, want %q", got, want)
	}
	id, ok := CheckIDFromSourceUID("check-42")
	if !ok || id != 42 {
		t.Fatalf("CheckIDFromSourceUID(check-42) = %d, %v; want 42, true", id, ok)
	}
	for _, bogus := range []string{"", "job-42", "check", "check-", "check-0", "check-1e3", "check-42x", "Check-42"} {
		if _, ok := CheckIDFromSourceUID(bogus); ok {
			t.Fatalf("CheckIDFromSourceUID(%q) accepted a non-check uid", bogus)
		}
	}
}
