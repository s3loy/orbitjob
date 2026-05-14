package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"orbitjob/internal/core/app/execute"
)

const signatureHeader = "X-OrbitJob-Signature"

// Webhook sends HTTP callbacks with optional HMAC-SHA256 signature.
type Webhook struct {
	client  *http.Client
	secure  http.RoundTripper
	timeout time.Duration
}

// NewWebhook creates a webhook handler that reuses the given HTTP client
// for connection pooling, with SSRF protection and DNS rebinding checks.
func NewWebhook(client *http.Client) *Webhook {
	if client == nil {
		client = http.DefaultClient
	}
	return &Webhook{
		client:  client,
		secure:  newSecureTransport(client.Transport),
		timeout: client.Timeout,
	}
}

func (h *Webhook) Execute(ctx context.Context, task execute.AssignedTask) execute.Result {
	rawURL, method, headers, body, err := parseHTTPPayload(task.HandlerPayload)
	if err != nil {
		return execute.Result{
			Success:    false,
			ResultCode: "invalid_payload",
			ErrorMsg:   err.Error(),
		}
	}

	secret, err := parseWebhookSecret(task.HandlerPayload)
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

	if secret != "" {
		sig := hmacSha256(secret, body)
		req.Header.Set(signatureHeader, "sha256="+hex.EncodeToString(sig))
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

func parseWebhookSecret(p map[string]any) (string, error) {
	secretRaw, ok := p["secret"]
	if !ok {
		return "", nil
	}
	secret, ok := secretRaw.(string)
	if !ok {
		return "", fmt.Errorf("secret must be a string")
	}
	return secret, nil
}

func hmacSha256(key, message string) []byte {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(message))
	return mac.Sum(nil)
}
