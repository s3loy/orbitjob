package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"orbitjob/internal/core/app/execute"
)

const httpHealthMaxBodyBytes = 4096

// HTTPHealth performs HTTP health checks with response time measurement.
type HTTPHealth struct {
	client *http.Client
	secure http.RoundTripper
}

// NewHTTPHealth creates a new HTTP health check handler.
func NewHTTPHealth(client *http.Client) *HTTPHealth {
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPHealth{
		client: client,
		secure: newSecureTransport(client.Transport),
	}
}

func (h *HTTPHealth) Execute(ctx context.Context, task execute.AssignedTask) execute.Result {
	config, err := parseHTTPHealthConfig(task.HandlerPayload)
	if err != nil {
		return execute.Result{
			Success:    false,
			ResultCode: "invalid_payload",
			ErrorMsg:   err.Error(),
		}
	}

	if err := validateCallbackURL(config.URL); err != nil {
		return execute.Result{
			Success:    false,
			ResultCode: "ssrf_blocked",
			ErrorMsg:   err.Error(),
		}
	}

	var bodyReader io.Reader
	if config.Body != "" {
		bodyReader = strings.NewReader(config.Body)
	}

	req, err := http.NewRequestWithContext(ctx, config.Method, config.URL, bodyReader)
	if err != nil {
		return execute.Result{
			Success:    false,
			ResultCode: "error",
			ErrorMsg:   fmt.Sprintf("build request: %v", err),
		}
	}

	for k, v := range config.Headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{
		Transport:     h.secure,
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       h.client.Timeout,
	}

	start := time.Now()
	resp, err := client.Do(req)
	durationMs := time.Since(start).Milliseconds()

	if err != nil {
		output := map[string]any{
			"response_time_ms": durationMs,
			"error":            err.Error(),
		}
		if ctx.Err() != nil {
			return execute.Result{
				Success:    false,
				ResultCode: "timeout",
				ErrorMsg:   fmt.Sprintf("request timed out: %v", ctx.Err()),
				Output:     output,
			}
		}
		return execute.Result{
			Success:    false,
			ResultCode: "error",
			ErrorMsg:   err.Error(),
			Output:     output,
		}
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, httpHealthMaxBodyBytes))
	bodyStr := truncate(string(respBody), httpHealthMaxBodyBytes)

	output := map[string]any{
		"status_code":      resp.StatusCode,
		"response_time_ms": durationMs,
		"response_body":    bodyStr,
	}

	// If expected_status is configured, check against it.
	if config.ExpectedStatus > 0 && resp.StatusCode != config.ExpectedStatus {
		return execute.Result{
			Success:    false,
			ResultCode: fmt.Sprintf("%d", resp.StatusCode),
			ErrorMsg:   fmt.Sprintf("expected status %d, got %d", config.ExpectedStatus, resp.StatusCode),
			Output:     output,
		}
	}

	// Default: 2xx is success.
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return execute.Result{
			Success:    true,
			ResultCode: fmt.Sprintf("%d", resp.StatusCode),
			Output:     output,
		}
	}

	return execute.Result{
		Success:    false,
		ResultCode: fmt.Sprintf("%d", resp.StatusCode),
		ErrorMsg:   bodyStr,
		Output:     output,
	}
}

type httpHealthConfig struct {
	URL            string
	Method         string
	Headers        map[string]string
	Body           string
	ExpectedStatus int
}

func parseHTTPHealthConfig(p map[string]any) (httpHealthConfig, error) {
	var cfg httpHealthConfig

	urlRaw, ok := p["url"]
	if !ok {
		return cfg, fmt.Errorf("missing required field: url")
	}
	cfg.URL, ok = urlRaw.(string)
	if !ok || cfg.URL == "" {
		return cfg, fmt.Errorf("url must be a non-empty string")
	}

	cfg.Method = "GET"
	if methodRaw, ok := p["method"]; ok {
		method, ok := methodRaw.(string)
		if !ok || method == "" {
			return cfg, fmt.Errorf("method must be a non-empty string")
		}
		cfg.Method = method
	}

	if headersRaw, ok := p["headers"]; ok {
		headersMap, ok := headersRaw.(map[string]any)
		if !ok {
			return cfg, fmt.Errorf("headers must be a map of string to string")
		}
		cfg.Headers = make(map[string]string, len(headersMap))
		for k, v := range headersMap {
			s, ok := v.(string)
			if !ok {
				return cfg, fmt.Errorf("headers[%q] must be a string", k)
			}
			cfg.Headers[k] = s
		}
	}

	if bodyRaw, ok := p["body"]; ok {
		body, ok := bodyRaw.(string)
		if !ok {
			return cfg, fmt.Errorf("body must be a string")
		}
		cfg.Body = body
	}

	if expectedRaw, ok := p["expected_status"]; ok {
		switch v := expectedRaw.(type) {
		case float64:
			cfg.ExpectedStatus = int(v)
		case int:
			cfg.ExpectedStatus = v
		case int64:
			cfg.ExpectedStatus = int(v)
		default:
			return cfg, fmt.Errorf("expected_status must be an integer")
		}
	}

	return cfg, nil
}
