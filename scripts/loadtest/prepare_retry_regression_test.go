package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPrepareOperationsRetryTransientHTTPFailures(t *testing.T) {
	for _, operation := range []string{"policy", "key"} {
		for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				if calls == 1 {
					w.WriteHeader(status)
					return
				}
				if operation == "policy" {
					_, _ = w.Write([]byte(`{"items":[{"id":"policy","name":"TenantAdminAccess","platform":true}]}`))
				} else {
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte(`{"key":"fixture-key"}`))
				}
			}))
			client := NewAPIClient(server.URL, "fixture")
			err := withBackoff(context.Background(), func() error {
				if operation == "policy" {
					_, err := client.PolicyIDByName(context.Background(), TenantAdminPolicyName)
					return err
				}
				_, err := client.CreateAPIKey(context.Background(), "tenant", []string{"policy"})
				return err
			})
			server.Close()
			if err != nil || calls != 2 {
				t.Errorf("%s after %d: calls=%d error=%v", operation, status, calls, err)
			}
		}
	}
}
