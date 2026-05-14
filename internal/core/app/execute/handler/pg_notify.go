package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"orbitjob/internal/core/app/execute"
)

const maxNotifyPayloadBytes = 4096

// PGNotify sends events via PostgreSQL NOTIFY.
type PGNotify struct {
	db              *sql.DB
	maxPayloadBytes int
}

// NewPGNotify creates a PGNotify handler with the given database connection.
func NewPGNotify(db *sql.DB) *PGNotify {
	if db == nil {
		panic("pg_notify handler requires a non-nil *sql.DB")
	}
	return &PGNotify{
		db:              db,
		maxPayloadBytes: maxNotifyPayloadBytes,
	}
}

func (h *PGNotify) Execute(ctx context.Context, task execute.AssignedTask) execute.Result {
	channel, body, err := parsePGNotifyPayload(task.HandlerPayload)
	if err != nil {
		return execute.Result{
			Success:    false,
			ResultCode: "invalid_payload",
			ErrorMsg:   err.Error(),
		}
	}

	channel = h.resolveChannel(channel, task.TenantID, task.JobID)

	payload := notifyPayload{
		RunID:       task.RunID,
		JobID:       task.JobID,
		TraceID:     task.TraceID,
		TenantID:    task.TenantID,
		ScheduledAt: task.ScheduledAt.Format(time.RFC3339),
		Attempt:     task.Attempt,
		Body:        body,
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return execute.Result{
			Success:    false,
			ResultCode: "marshal_error",
			ErrorMsg:   fmt.Sprintf("marshal notify payload: %v", err),
		}
	}

	if len(payloadJSON) > h.maxPayloadBytes {
		return execute.Result{
			Success:    false,
			ResultCode: "payload_too_large",
			ErrorMsg:   fmt.Sprintf("notify payload %d bytes exceeds limit %d", len(payloadJSON), h.maxPayloadBytes),
		}
	}

	if _, err := h.db.ExecContext(ctx, "SELECT pg_notify($1, $2)", channel, string(payloadJSON)); err != nil {
		return execute.Result{
			Success:    false,
			ResultCode: "notify_failed",
			ErrorMsg:   fmt.Sprintf("pg_notify: %v", err),
		}
	}

	return execute.Result{
		Success:    true,
		ResultCode: "notified",
	}
}

func (h *PGNotify) resolveChannel(userChannel, tenantID string, jobID int64) string {
	if userChannel != "" {
		return sanitizeChannel("orbitjob_" + userChannel)
	}
	return fmt.Sprintf("orbitjob_%s_%d", sanitizeChannel(tenantID), jobID)
}

func sanitizeChannel(name string) string {
	var out []byte
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9'):
			out = append(out, c)
		case c == '-' || c == '_':
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "orbitjob_default"
	}
	return string(out)
}

type notifyPayload struct {
	RunID       string         `json:"run_id"`
	JobID       int64          `json:"job_id"`
	TraceID     *string        `json:"trace_id,omitempty"`
	TenantID    string         `json:"tenant_id"`
	ScheduledAt string         `json:"scheduled_at"`
	Attempt     int            `json:"attempt"`
	Body        map[string]any `json:"body,omitempty"`
}

func parsePGNotifyPayload(p map[string]any) (channel string, body map[string]any, err error) {
	if chRaw, ok := p["channel"]; ok {
		ch, ok := chRaw.(string)
		if !ok {
			return "", nil, fmt.Errorf("channel must be a string")
		}
		channel = ch
	}

	if bodyRaw, ok := p["body"]; ok {
		bodyMap, ok := bodyRaw.(map[string]any)
		if !ok {
			return "", nil, fmt.Errorf("body must be a JSON object")
		}
		body = bodyMap
	}

	return channel, body, nil
}
