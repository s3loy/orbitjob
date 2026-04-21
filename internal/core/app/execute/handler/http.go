package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"orbitjob/internal/core/app/execute"
)

const maxResponseBodyBytes = 4096

type HTTPHandler struct {
	client *http.Client
}

func NewHTTPHandler(client *http.Client) *HTTPHandler {
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPHandler{client: client}
}

func (h *HTTPHandler) Execute(ctx context.Context, task execute.AssignedTask) execute.Result {
	url, method, headers, body, err := parseHTTPPayload(task.HandlerPayload)
	if err != nil {
		return execute.Result{
			Success:    false,
			ResultCode: "invalid_payload",
			ErrorMsg:   err.Error(),
		}
	}

	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
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

	resp, err := h.client.Do(req)
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
	defer resp.Body.Close()

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

func parseHTTPPayload(p map[string]any) (url, method string, headers map[string]string, body string, err error) {
	urlRaw, ok := p["url"]
	if !ok {
		return "", "", nil, "", fmt.Errorf("missing required field: url")
	}
	url, ok = urlRaw.(string)
	if !ok || url == "" {
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

	return url, method, headers, body, nil
}
