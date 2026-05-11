// healthcheck is a minimal HTTP health check binary for scratch Docker images.
// Usage: healthcheck <url>
// Exits 0 on 2xx response, 1 otherwise.
package main

import (
	"net/http"
	"os"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		os.Exit(1)
	}

	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(os.Args[1])
	if err != nil {
		os.Exit(1)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		os.Exit(0)
	}
	os.Exit(1)
}
