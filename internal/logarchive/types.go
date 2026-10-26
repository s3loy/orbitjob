// Package logarchive owns bounded execution evidence, independently of Pods.
package logarchive

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

const (
	MiB            = int64(1024 * 1024)
	GiB            = 1024 * MiB
	Retention      = 15 * 24 * time.Hour
	MaxRecordBytes = 128 * 1024
)

var (
	ErrCapacity      = errors.New("execution log capacity exhausted")
	ErrMissing       = errors.New("execution archive not registered")
	ErrSealed        = errors.New("execution archive is sealed")
	ErrInvalidSource = errors.New("invalid execution log source")
)

// Identity is resolved by the server, never from collector-supplied labels.
type Identity struct {
	TenantID      string    `json:"tenant_id"`
	SourceUID     string    `json:"source_uid"`
	OccurrenceKey string    `json:"occurrence_key"`
	Namespace     string    `json:"namespace"`
	SourceName    string    `json:"source_name"`
	RevisionID    int64     `json:"revision_id"`
	RunID         int64     `json:"run_id"`
	RunName       string    `json:"run_name"`
	RunUID        string    `json:"run_uid"`
	GroupID       string    `json:"group_id"`
	FunctionID    int64     `json:"function_id"`
	FunctionRunID string    `json:"function_run_id"`
	WorkflowID    int64     `json:"workflow_run_id"`
	CreatedAt     time.Time `json:"created_at"`
}

func (i Identity) Key() string {
	h := sha256.Sum256([]byte(i.TenantID + "\x00" + i.SourceUID + "\x00" + i.OccurrenceKey))
	return hex.EncodeToString(h[:])
}

func (i Identity) Validate() error {
	if i.TenantID == "" || i.SourceUID == "" || i.OccurrenceKey == "" || i.RunUID == "" || i.Namespace == "" || i.RunName == "" || i.RevisionID < 1 {
		return fmt.Errorf("incomplete immutable archive identity")
	}
	return nil
}

type Record struct {
	ID         string    `json:"id"`
	Sequence   int64     `json:"sequence"`
	Time       time.Time `json:"time"`
	ObservedAt time.Time `json:"observed_at"`
	TimeSource string    `json:"time_source"`
	Kind       string    `json:"kind"`
	Stage      string    `json:"stage"`
	Attempt    int       `json:"attempt"`
	PodUID     string    `json:"pod_uid"`
	Container  string    `json:"container"`
	Restart    int32     `json:"restart"`
	Text       string    `json:"text"`
	ExitCode   *int32    `json:"exit_code,omitempty"`
	Signal     *int32    `json:"signal,omitempty"`
}

type Gap struct {
	From    int64  `json:"from"`
	Through int64  `json:"through"`
	Reason  string `json:"reason"`
}

type Segment struct {
	Name      string `json:"name"`
	First     int64  `json:"first"`
	Last      int64  `json:"last"`
	Bytes     int64  `json:"bytes"`
	DiskBytes int64  `json:"disk_bytes"`
	Records   int    `json:"records"`
}

type Source struct {
	Namespace string `json:"namespace"`
	PodName   string `json:"pod_name"`
	PodUID    string `json:"pod_uid"`
	JobUID    string `json:"job_uid"`
	Attempt   int    `json:"attempt"`
	Container string `json:"container"`
	Restart   int32  `json:"restart"`
}

type Manifest struct {
	Identity       Identity   `json:"identity"`
	State          string     `json:"state"`
	Reason         string     `json:"reason"`
	Outcome        string     `json:"outcome"`
	Next           int64      `json:"next_sequence"`
	OutputBytes    int64      `json:"output_bytes"`
	RetainedBytes  int64      `json:"retained_bytes"`
	Segments       []Segment  `json:"segments"`
	Events         []Record   `json:"events"`
	Gaps           []Gap      `json:"gaps"`
	Seen           []string   `json:"seen,omitempty"`
	Batches        []string   `json:"batches,omitempty"`
	Sources        []Source   `json:"sources,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	SealedAt       *time.Time `json:"sealed_at,omitempty"`
	ProtectedUntil *time.Time `json:"protected_until,omitempty"`
}

type Query struct {
	After   int64  `form:"after" binding:"min=0"`
	Limit   int    `form:"limit" binding:"omitempty,min=1,max=1000"`
	Attempt int    `form:"attempt" binding:"omitempty,min=1"`
	Search  string `form:"search" binding:"max=256"`
}

type Page struct {
	State          string     `json:"state"`
	Reason         string     `json:"reason"`
	Outcome        string     `json:"outcome"`
	Records        []Record   `json:"records"`
	Gaps           []Gap      `json:"gaps"`
	Next           int64      `json:"next"`
	HasMore        bool       `json:"has_more"`
	OutputBytes    int64      `json:"output_bytes"`
	RetainedBytes  int64      `json:"retained_bytes"`
	LimitBytes     int64      `json:"limit_bytes"`
	ProtectedUntil *time.Time `json:"protected_until,omitempty"`
}

type Limits struct {
	TotalBytes    int64
	RunBytes      int64
	HeadBytes     int64
	WorkflowBytes int64
	MetadataBytes int64
	ManifestBytes int64
	ReserveBytes  int64
	DeclaredBytes int64
	TenantBytes   int64
	Now           func() time.Time
}

func DefaultLimits() Limits {
	return Limits{TotalBytes: 7 * GiB, RunBytes: 32 * MiB, HeadBytes: 8 * MiB,
		WorkflowBytes: 128 * MiB, MetadataBytes: 128 * MiB, ManifestBytes: 256 * 1024,
		ReserveBytes: GiB, DeclaredBytes: 10 * GiB, TenantBytes: 2 * GiB, Now: time.Now}
}

func (l Limits) Validate() error {
	if l.Now == nil || l.RunBytes < 256*1024 || l.HeadBytes < 0 || l.HeadBytes >= l.RunBytes || l.TotalBytes < l.RunBytes || l.ManifestBytes < 4096 || l.MetadataBytes < l.ManifestBytes || l.TenantBytes < l.RunBytes || l.WorkflowBytes < l.RunBytes || l.DeclaredBytes < l.TotalBytes+l.ReserveBytes {
		return fmt.Errorf("invalid execution archive budgets")
	}
	return nil
}
