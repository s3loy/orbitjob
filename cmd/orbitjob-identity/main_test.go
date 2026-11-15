package main

import (
	"strings"
	"testing"
)

func TestConsoleOriginAllowsHTTPSAndLocalHTTP(t *testing.T) {
	for _, origin := range []string{"", "https://console.example", "https://console.example:8443", "http://localhost:18080", "http://127.0.0.1:18080", "http://[::1]:18080"} {
		got, err := consoleOrigin(origin)
		if err != nil || got != origin {
			t.Fatalf("origin=%q got=%q err=%v", origin, got, err)
		}
	}
}
func TestConsoleOriginRejectsUnsafeAndNonOriginInput(t *testing.T) {
	for _, origin := range []string{"http://console.example", "http://127.0.0.1.example", "http://localhost.example", "ftp://localhost", "javascript:alert(1)", "https://console.example/", "https://console.example/console", "https://console.example?token=hidden-secret", "https://console.example?", "https://console.example#hidden-secret", "https://console.example#", "https://user:hidden-secret@console.example", "https://console.example:0", "https://console.example:65536", "https://console.example:", "https:///console", " https://console.example"} {
		_, err := consoleOrigin(origin)
		if err == nil {
			t.Fatalf("unsafe origin accepted: %q", origin)
		}
		if strings.Contains(err.Error(), "hidden-secret") {
			t.Fatal("origin validation exposed a pasted secret")
		}
	}
}
