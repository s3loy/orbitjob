package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type APIClient struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewAPIClient(baseURL, apiKey string) *APIClient {
	return &APIClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

type TriggerResponse struct {
	RunID    string `json:"run_id"`
	JobID    int64  `json:"job_id"`
	TenantID string `json:"tenant_id"`
	Status   string `json:"status"`
	Created  bool   `json:"created"`
}

func (c *APIClient) CreateJob(ctx context.Context, tenant string, request map[string]any) (int64, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return 0, err
	}
	resp, err := c.do(ctx, http.MethodPost, "/api/v1/jobs", tenant, "", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		return 0, fmt.Errorf("create job: status %d", resp.StatusCode)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return 0, err
	}
	return created.ID, nil
}

func (c *APIClient) TriggerJob(ctx context.Context, jobID int64, tenant, idempotencyKey string) (TriggerResponse, error) {
	resp, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/jobs/%d/trigger", jobID), tenant, idempotencyKey, nil)
	if err != nil {
		return TriggerResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK:
		var out TriggerResponse
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return TriggerResponse{}, err
		}
		return out, nil
	case http.StatusConflict:
		return TriggerResponse{Created: false}, nil
	default:
		return TriggerResponse{}, fmt.Errorf("trigger job %d: status %d", jobID, resp.StatusCode)
	}
}

func (c *APIClient) ListInstances(ctx context.Context, tenant string, limit int) ([]map[string]any, error) {
	resp, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/instances?limit=%d", limit), tenant, "", nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list instances: status %d", resp.StatusCode)
	}
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.Items, nil
}

func (c *APIClient) do(ctx context.Context, method, path, tenant, idempotencyKey string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if tenant != "" {
		req.Header.Set("X-OrbitJob-Tenant", tenant)
	}
	if idempotencyKey != "" {
		req.Header.Set("X-OrbitJob-Idempotency-Key", idempotencyKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.client.Do(req)
}
