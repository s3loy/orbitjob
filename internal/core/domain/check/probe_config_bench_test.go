package check

import "testing"

// benchProbe keeps benchmark results alive so the compiler cannot drop the calls.
var benchProbe ProbeConfig

// NormalizeProbeConfig runs on two hot paths of the check plane: every admin
// create of an http_health check re-validates the config here, and every
// revision synthesis re-validates what is already stored. It is not an
// expensive function — a URL parse, a couple of string operations — but it is
// on the boundary of every check write, so the benchmark pins both the cost of
// the accepting path and the cost of refusing a bad one. Invalid inputs are a
// steady-state load, not an edge case: the boundary exists because clients
// send configs that fail.
func BenchmarkNormalizeProbeConfig(b *testing.B) {
	b.Run("valid/full-config", func(b *testing.B) {
		cfg := map[string]any{
			"url":             "https://api.example.com/v1/health?probe=orbitjob&details=1",
			"method":          "get",
			"expected_status": 200,
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			benchProbe, _ = NormalizeProbeConfig(cfg)
		}
	})

	b.Run("valid/url-only-defaults", func(b *testing.B) {
		cfg := map[string]any{
			"url": "https://api.example.com/v1/health",
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			benchProbe, _ = NormalizeProbeConfig(cfg)
		}
	})

	b.Run("invalid/bad-scheme", func(b *testing.B) {
		cfg := map[string]any{
			"url":    "ftp://api.example.com/v1/health",
			"method": "GET",
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			benchProbe, _ = NormalizeProbeConfig(cfg)
		}
	})

	b.Run("invalid/relative-url", func(b *testing.B) {
		cfg := map[string]any{
			"url":    "/health",
			"method": "GET",
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			benchProbe, _ = NormalizeProbeConfig(cfg)
		}
	})
}
