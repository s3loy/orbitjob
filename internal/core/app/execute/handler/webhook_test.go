package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"orbitjob/internal/core/app/execute"
)

func TestWebhook_HMACSignature(t *testing.T) {
	defer disableSSRF()()

	secret := "my-secret"
	body := `{"key":"value"}`

	var receivedSig string
	var receivedBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSig = r.Header.Get("X-OrbitJob-Signature")
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		receivedBody = string(buf)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	wh := NewWebhook(http.DefaultClient)
	task := execute.AssignedTask{
		HandlerPayload: map[string]any{
			"url":    ts.URL,
			"method": "POST",
			"body":   body,
			"secret": secret,
		},
		TimeoutSec: 30,
	}

	result := wh.Execute(context.Background(), task)
	if !result.Success {
		t.Fatalf("expected success, got %s: %s", result.ResultCode, result.ErrorMsg)
	}

	if receivedSig == "" {
		t.Fatal("expected signature header to be set")
	}

	expectedMAC := hmacSha256(secret, body)
	expectedSig := "sha256=" + hex.EncodeToString(expectedMAC)
	if receivedSig != expectedSig {
		t.Errorf("signature mismatch: got %q, want %q", receivedSig, expectedSig)
	}

	if receivedBody != body {
		t.Errorf("body mismatch: got %q, want %q", receivedBody, body)
	}
}

func TestWebhook_NoSecret(t *testing.T) {
	defer disableSSRF()()

	var receivedSig string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSig = r.Header.Get("X-OrbitJob-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	wh := NewWebhook(http.DefaultClient)
	task := execute.AssignedTask{
		HandlerPayload: map[string]any{
			"url":    ts.URL,
			"method": "POST",
		},
		TimeoutSec: 30,
	}

	result := wh.Execute(context.Background(), task)
	if !result.Success {
		t.Fatalf("expected success, got %s: %s", result.ResultCode, result.ErrorMsg)
	}

	if receivedSig != "" {
		t.Errorf("expected no signature when secret is empty, got %q", receivedSig)
	}
}

func TestWebhook_InvalidPayload(t *testing.T) {
	wh := NewWebhook(http.DefaultClient)

	tests := []struct {
		name    string
		payload map[string]any
		want    string
	}{
		{
			name:    "missing url",
			payload: map[string]any{"method": "POST"},
			want:    "invalid_payload",
		},
		{
			name:    "invalid secret type",
			payload: map[string]any{"url": "http://example.com", "secret": 123},
			want:    "invalid_payload",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := execute.AssignedTask{
				HandlerPayload: tt.payload,
				TimeoutSec:     30,
			}
			result := wh.Execute(context.Background(), task)
			if result.Success {
				t.Errorf("expected failure")
			}
			if result.ResultCode != tt.want {
				t.Errorf("result code = %q, want %q", result.ResultCode, tt.want)
			}
		})
	}
}

func TestWebhook_Non2xxResponse(t *testing.T) {
	defer disableSSRF()()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	}))
	defer ts.Close()

	wh := NewWebhook(http.DefaultClient)
	task := execute.AssignedTask{
		HandlerPayload: map[string]any{
			"url":    ts.URL,
			"method": "POST",
		},
		TimeoutSec: 30,
	}

	result := wh.Execute(context.Background(), task)
	if result.Success {
		t.Fatal("expected failure for non-2xx")
	}
	if result.ResultCode != "400" {
		t.Errorf("result code = %q, want 400", result.ResultCode)
	}
}

func TestWebhook_SSRFBlocked(t *testing.T) {
	wh := NewWebhook(http.DefaultClient)
	task := execute.AssignedTask{
		HandlerPayload: map[string]any{
			"url":    "http://127.0.0.1:8080/hook",
			"method": "POST",
		},
		TimeoutSec: 30,
	}

	result := wh.Execute(context.Background(), task)
	if result.Success {
		t.Fatal("expected SSRF block")
	}
	if result.ResultCode != "ssrf_blocked" {
		t.Errorf("result code = %q, want ssrf_blocked", result.ResultCode)
	}
}

func TestHMACSha256(t *testing.T) {
	key := "key"
	msg := "message"
	got := hmacSha256(key, msg)

	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(msg))
	want := mac.Sum(nil)

	if string(got) != string(want) {
		t.Errorf("hmac mismatch")
	}
}
