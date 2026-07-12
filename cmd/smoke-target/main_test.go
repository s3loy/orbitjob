package main

import (
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebhookHandler(t *testing.T) {
	const secret = "test-secret"
	body := `{"event":"job.completed"}`

	tests := []struct {
		name       string
		signature  string
		header     string
		wantStatus int
	}{
		{name: "valid", signature: "sha256=" + encode(sign(secret, body)), header: "orbitjob", wantStatus: http.StatusNoContent},
		{name: "bad signature", signature: "sha256=00", header: "orbitjob", wantStatus: http.StatusUnauthorized},
		{name: "missing header", signature: "sha256=" + encode(sign(secret, body)), wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
			request.Header.Set(signatureHeader, tt.signature)
			request.Header.Set("X-Smoke-Test", tt.header)
			rec := httptest.NewRecorder()
			webhookHandler(secret).ServeHTTP(rec, request)
			if rec.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, rec.Code)
			}
		})
	}
}

func encode(value []byte) string {
	return hex.EncodeToString(value)
}
