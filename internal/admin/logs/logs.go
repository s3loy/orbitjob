// Package logs describes the immutable ledger locator, never a client Pod locator.
package logs

import "context"

type Locator struct {
	TenantID      string
	Namespace     string
	SourceUID     string
	SourceName    string
	RevisionID    int64
	OccurrenceKey string
	JobName       string
	JobUID        string
	GroupID       string
	Attempt       int
}
type Reader interface {
	Resolve(context.Context, string, int64, int) (Locator, error)
	ResolveFunction(context.Context, string, int64, string, int) (Locator, error)
}
type Request struct {
	TailLines  int64 `form:"tail_lines" binding:"omitempty,min=1,max=500"`
	LimitBytes int64 `form:"limit_bytes" binding:"omitempty,min=1,max=65536"`
}
type Result struct {
	State      string `json:"state"`
	Text       string `json:"text"`
	Truncated  bool   `json:"truncated"`
	TailLines  int64  `json:"tail_lines"`
	LimitBytes int64  `json:"limit_bytes"`
	Attempt    int    `json:"attempt"`
	Container  string `json:"container"`
	Reason     string `json:"reason"`
}
