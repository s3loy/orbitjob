package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"
)

// Exit codes the CLI contract pins. Everything that is not one of the typed
// API failures is a plain exit 1.
const (
	exitGeneral   = 1
	exitNotFound  = 3
	exitForbidden = 4
)

// apiClient is the one HTTP path every API command takes. It carries the key
// and never prints it: the key exists only in the Authorization header.
type apiClient struct {
	base    string
	key     string
	jsonOut bool
	http    *http.Client
}

// apiError is a typed response from the admin API. Status drives the exit
// code; the server's own code and message drive the text, so the CLI and the
// API never disagree about what went wrong.
type apiError struct {
	status  int
	code    string
	message string
}

func (e *apiError) Error() string {
	if e.code == "" {
		return fmt.Sprintf("admin API returned %d", e.status)
	}
	return fmt.Sprintf("admin API returned %d %s: %s", e.status, e.code, e.message)
}

// exitCode maps an API status to the CLI's contract: 404 is not found (3),
// 401 and 403 are forbidden (4), everything else is generic (1).
func (e *apiError) exitCode() int {
	switch e.status {
	case http.StatusNotFound:
		return exitNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return exitForbidden
	default:
		return exitGeneral
	}
}

// connectionHint is the one actionable thing a developer needs when the CLI
// cannot reach the API: the port-forward is down or never started.
const connectionHint = "cannot reach the admin API at %s (%v)" +
	" -- start the port-forward: kubectl -n orbitjob-system port-forward svc/orbitjob-admin-api 18080:8080" +
	" and source <(make kind-env) for the key"

// do performs one API call and returns the raw body. Non-2xx responses become
// apiError; transport failures become the connection-refused hint with exit 1.
func (c *apiClient) do(ctx context.Context, method, path string, query map[string]string, body []byte) ([]byte, error) {
	return c.doWithHeader(ctx, method, path, nil, query, body)
}

// doWithHeader is do with extra request headers (the trigger route's
// idempotency key is the one user).
func (c *apiClient) doWithHeader(ctx context.Context, method, path string, headers map[string]string, query map[string]string, body []byte) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, fatal("build request for %s: %v", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	q := req.URL.Query()
	for k, v := range query {
		if v != "" {
			q.Set(k, v)
		}
	}
	req.URL.RawQuery = q.Encode()

	client := c.http
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		if isConnectionError(err) {
			return nil, failf(exitGeneral, "[FAIL] "+connectionHint, c.base, err)
		}
		return nil, fatal("%s %s failed: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fatal("read response from %s: %v", path, err)
	}
	if resp.StatusCode >= 300 {
		apiErr := &apiError{status: resp.StatusCode}
		var envelope struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &envelope) == nil && envelope.Error.Code != "" {
			apiErr.code = envelope.Error.Code
			apiErr.message = envelope.Error.Message
		}
		return nil, apiErr
	}
	return data, nil
}

// isConnectionError recognizes the transport failures that mean "you are not
// talking to the server": refused, unreachable, reset, timed out, TLS broken.
func isConnectionError(err error) bool {
	var dnsErr *net.DNSError
	switch {
	case errors.Is(err, syscall.ECONNREFUSED),
		errors.Is(err, syscall.ECONNRESET),
		errors.Is(err, syscall.ENETUNREACH),
		errors.Is(err, syscall.EHOSTUNREACH),
		errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, net.ErrClosed):
		return true
	case errors.As(err, &dnsErr):
		return true
	}
	var urlErr *net.OpError
	return errors.As(err, &urlErr)
}

// JSON views of the API read models. Only the fields the CLI prints are
// declared; --json mode prints the raw response so nothing is lost.

type runItem struct {
	ID            int64  `json:"id"`
	OccurrenceKey string `json:"occurrence_key"`
	Trigger       string `json:"trigger"`
	Actor         string `json:"actor"`
	Phase         string `json:"phase"`
	Attempt       int    `json:"attempt"`
	MaxAttempts   int    `json:"max_attempts"`
	UpdatedAt     string `json:"updated_at"`
}

type attemptItem struct {
	ID                int64   `json:"id"`
	AttemptNumber     int     `json:"attempt_number"`
	Phase             string  `json:"phase"`
	KubernetesJobName string  `json:"kubernetes_job_name"`
	StartedAt         *string `json:"started_at"`
	CompletedAt       *string `json:"completed_at"`
}

type runDetail struct {
	ID            int64         `json:"id"`
	OccurrenceKey string        `json:"occurrence_key"`
	Trigger       string        `json:"trigger"`
	Actor         string        `json:"actor"`
	Phase         string        `json:"phase"`
	Attempt       int           `json:"attempt"`
	MaxAttempts   int           `json:"max_attempts"`
	CreatedAt     string        `json:"created_at"`
	UpdatedAt     string        `json:"updated_at"`
	Attempts      []attemptItem `json:"attempts"`
}

type runRef struct {
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
	OccurrenceKey string `json:"occurrence_key"`
	Phase         string `json:"phase"`
}

type jobItem struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	Namespace       string `json:"namespace"`
	Schedule        string `json:"schedule"`
	Suspend         bool   `json:"suspend"`
	ScheduleSummary string `json:"schedule_summary"`
	CreatedAt       string `json:"created_at"`
}

type jobDetail struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	Namespace        string `json:"namespace"`
	Schedule         string `json:"schedule"`
	Suspend          bool   `json:"suspend"`
	ScheduleSummary  string `json:"schedule_summary"`
	TimeoutSeconds   int32  `json:"timeout_seconds"`
	RetryMaxAttempts int32  `json:"retry_max_attempts"`
	Actor            string `json:"actor"`
	CreatedAt        string `json:"created_at"`
	JobTemplate      struct {
		Image        string `json:"image"`
		BackoffLimit int32  `json:"backoff_limit"`
	} `json:"job_template"`
}

type checkItem struct {
	ID           int64   `json:"id"`
	Name         string  `json:"name"`
	Status       string  `json:"status"`
	CheckType    string  `json:"check_type"`
	ScheduleType string  `json:"schedule_type"`
	NextRunAt    *string `json:"next_run_at"`
	CreatedAt    string  `json:"created_at"`
}

type checkDetail struct {
	ID           int64   `json:"id"`
	Name         string  `json:"name"`
	Status       string  `json:"status"`
	CheckType    string  `json:"check_type"`
	ScheduleType string  `json:"schedule_type"`
	CronExpr     *string `json:"cron_expr"`
	IntervalSec  *int    `json:"interval_sec"`
	NextRunAt    *string `json:"next_run_at"`
	TimeoutSec   int     `json:"timeout_sec"`
	RetryLimit   int     `json:"retry_limit"`
	Priority     int     `json:"priority"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

// decodeList unpacks an {"items": [...]} envelope.
func decodeList[T any](data []byte) ([]T, error) {
	var envelope struct {
		Items []T `json:"items"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fatal("parse API response: %v", err)
	}
	return envelope.Items, nil
}

// decodeOne unpacks a single JSON object.
func decodeOne[T any](data []byte) (T, error) {
	var item T
	if err := json.Unmarshal(data, &item); err != nil {
		return item, fatal("parse API response: %v", err)
	}
	return item, nil
}

// printJSONOr emits the raw response in --json mode, else hands the decoded
// form to render.
func printJSONOr(client *apiClient, data []byte, render func()) error {
	if client.jsonOut {
		_, _ = fmt.Fprintln(stdoutWriter, strings.TrimRight(string(data), "\n"))
		return nil
	}
	render()
	return nil
}

// apiErrorOf unwraps a typed API failure so callers can branch on it.
func apiErrorOf(err error) (*apiError, bool) {
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}
