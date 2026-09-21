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

// TriggerResponse is what a manual trigger returns: a reference to the JobRun
// custom resource, not a ledger row. The Admin API does not write the row --
// the operator does, asynchronously, after the CR is reconciled -- so there is
// no run id in this response. A caller that needs the ledger id (cancel does)
// resolves it from the occurrence key against the run list.
type TriggerResponse struct {
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
	OccurrenceKey string `json:"occurrence_key"`
	Trigger       string `json:"trigger"`
	Phase         string `json:"phase"`
	Created       bool   `json:"created"`
}

// TriggerError carries the HTTP status of a failed trigger so the run engine
// can classify rejections. StatusCode 0 means the request never got a response
// (dial failure, timeout, connection reset).
type TriggerError struct {
	RevisionID int64
	StatusCode int
	Message    string
}

func (e *TriggerError) Error() string {
	if e.StatusCode == 0 {
		return fmt.Sprintf("trigger revision %d: transport error: %s", e.RevisionID, e.Message)
	}
	return fmt.Sprintf("trigger revision %d: status %d", e.RevisionID, e.StatusCode)
}

// CancelError carries the HTTP status of a failed cancel request. 409 means the
// run's ledger row is open but its JobRun custom resource is missing; the API
// reports that as Conflict, not NotFound.
type CancelError struct {
	RunID      string
	StatusCode int
	Message    string
}

func (e *CancelError) Error() string {
	if e.StatusCode == 0 {
		return fmt.Sprintf("cancel run %s: transport error: %s", e.RunID, e.Message)
	}
	return fmt.Sprintf("cancel run %s: status %d", e.RunID, e.StatusCode)
}

// TriggerJob publishes a manual JobRun for the definition revision. The API
// key determines the tenant; there is no tenant header to set. The
// idempotency key makes a repeated trigger resolve to the same run instead of
// creating another.
func (c *APIClient) TriggerJob(ctx context.Context, revisionID int64, idempotencyKey string) (TriggerResponse, error) {
	resp, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/jobs/%d/trigger", revisionID), idempotencyKey, nil)
	if err != nil {
		return TriggerResponse{}, &TriggerError{RevisionID: revisionID, StatusCode: 0, Message: err.Error()}
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
		return TriggerResponse{}, &TriggerError{RevisionID: revisionID, StatusCode: resp.StatusCode}
	}
}

// ListInstances reads the newest runs from the ledger, newest first. It is the
// only way for a trigger caller to learn the ledger id of a run it just
// created: the trigger response names the JobRun object, and the row appears
// once the operator has reconciled it.
func (c *APIClient) ListInstances(ctx context.Context, limit int) ([]map[string]any, error) {
	resp, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/instances?limit=%d", limit), "", nil)
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

// RunIDByOccurrenceKey resolves a just-triggered run's ledger id from its
// occurrence key. The operator writes the row asynchronously, so the first
// read can legitimately miss; poll until it appears or the deadline passes.
// A key that belongs to another tenant can never be returned: the API scopes
// the list to the key's own tenant.
func (c *APIClient) RunIDByOccurrenceKey(ctx context.Context, occurrenceKey string, deadline time.Time) (string, error) {
	for {
		items, err := c.ListInstances(ctx, 50)
		if err != nil {
			return "", err
		}
		for _, item := range items {
			if key, _ := item["occurrence_key"].(string); key == occurrenceKey {
				switch id := item["id"].(type) {
				case float64:
					return fmt.Sprintf("%d", int64(id)), nil
				case string:
					return id, nil
				}
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("run with occurrence key %s did not appear in the ledger", occurrenceKey)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// CancelInstance requests a stop for one run. The request carries no body: the
// whole effect is a patch of spec.cancelRequested on the run's JobRun custom
// resource, and the operator does the rest. A 409 response means the ledger
// row exists but its CR is gone.
func (c *APIClient) CancelInstance(ctx context.Context, runID string) error {
	path := fmt.Sprintf("/api/v1/instances/%s/cancel", runID)
	resp, err := c.do(ctx, http.MethodPost, path, "", nil)
	if err != nil {
		return &CancelError{RunID: runID, StatusCode: 0, Message: err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return &CancelError{RunID: runID, StatusCode: resp.StatusCode, Message: fmt.Sprintf("cancel instance %s: status %d", runID, resp.StatusCode)}
	}
	return nil
}

func (c *APIClient) CreateTenant(ctx context.Context, slug, name string) (string, error) {
	body, err := json.Marshal(map[string]any{"slug": slug, "name": name, "status": "active"})
	if err != nil {
		return "", err
	}
	resp, err := c.do(ctx, http.MethodPost, "/api/v1/tenants", "", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("create tenant %s: status %d", slug, resp.StatusCode)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return "", err
	}
	return created.ID, nil
}

// TenantAdminPolicyName is the platform preset the fixtures bind to their
// tenant keys.
//
// A key with no policies authenticates and then reaches nothing, which is the
// intended default rather than a gap: authorization is a list of grants, not a
// property of being logged in. The fixtures create, trigger and read jobs, so
// they ask for the tenant-scoped administrator preset.
const TenantAdminPolicyName = "TenantAdminAccess"

// PolicyIDByName resolves a platform preset by name. The fixture keys bind by
// name rather than by hardcoded id so the binding survives a reseed.
func (c *APIClient) PolicyIDByName(ctx context.Context, name string) (string, error) {
	resp, err := c.do(ctx, http.MethodGet, "/api/v1/policies", "", nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("list policies: status %d", resp.StatusCode)
	}
	var body struct {
		Items []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Platform bool   `json:"platform"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	for _, item := range body.Items {
		if item.Name == name && item.Platform {
			return item.ID, nil
		}
	}
	return "", fmt.Errorf("platform policy %q not found", name)
}

func (c *APIClient) CreateAPIKey(ctx context.Context, tenantID string, policyIDs []string) (string, error) {
	body, err := json.Marshal(map[string]any{"policies": policyIDs})
	if err != nil {
		return "", err
	}
	resp, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/tenants/%s/api_keys", tenantID), "", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("create api key for %s: status %d", tenantID, resp.StatusCode)
	}
	var created struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return "", err
	}
	return created.Key, nil
}

// GetJob reads one definition revision by id. It is what the cross-tenant
// probes use: a key from another tenant must be refused, not served.
func (c *APIClient) GetJob(ctx context.Context, revisionID int64) (map[string]any, error) {
	resp, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/jobs/%d", revisionID), "", nil)
	if err != nil {
		return nil, &APIError{StatusCode: 0, Message: err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("get job %d: status %d", revisionID, resp.StatusCode)}
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// APIError carries the HTTP status of a failed API call so callers can tell
// authorization denials (403) apart from server faults and transport errors.
// StatusCode 0 means no response was received.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e.StatusCode == 0 {
		return "transport error: " + e.Message
	}
	return e.Message
}

func (c *APIClient) do(ctx context.Context, method, path, idempotencyKey string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if idempotencyKey != "" {
		req.Header.Set("X-OrbitJob-Idempotency-Key", idempotencyKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.client.Do(req)
}
