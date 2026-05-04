package tenant

import (
	"strings"
	"time"

	"orbitjob/internal/domain/validation"
)

type Tenant struct {
	ID        string
	Slug      string
	Name      string
	Status    string
	Quotas    map[string]any
	Metadata  map[string]any
	CreatedAt time.Time
	UpdatedAt time.Time
}

const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
)

type CreateTenantInput struct {
	Slug string
	Name string
}

func NormalizeCreateTenant(in CreateTenantInput) (CreateTenantInput, error) {
	slug := strings.TrimSpace(in.Slug)
	if slug == "" {
		return CreateTenantInput{}, validation.New("slug", "is required")
	}
	if len(slug) > 64 {
		return CreateTenantInput{}, validation.New("slug", "must be <= 64 characters")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return CreateTenantInput{}, validation.New("name", "is required")
	}
	if len(name) > 128 {
		return CreateTenantInput{}, validation.New("name", "must be <= 128 characters")
	}
	return CreateTenantInput{Slug: slug, Name: name}, nil
}

type ApiKey struct {
	ID          string
	TenantID    string
	KeyHash     string
	KeyPrefix   string
	Permissions map[string]any
	ExpiresAt   *time.Time
	RevokedAt   *time.Time
	CreatedAt   time.Time
	CreatedBy   string
}

type AuditEvent struct {
	ID             int64
	TenantID       string
	ActorType      string
	ActorID        string
	EventType      string
	ResourceType   string
	ResourceID     string
	Diff           map[string]any
	TraceID        string
	IdempotencyKey string
	CreatedAt      time.Time
}

const (
	ActorTypeSystem  = "system"
	ActorTypeAPIKey  = "api_key"
	ActorTypeUser    = "user"

	EventTypeJobCreated        = "job.created"
	EventTypeJobUpdated        = "job.updated"
	EventTypeJobStatusChanged  = "job.status_changed"
	EventTypeInstanceCreated   = "instance.created"
	EventTypeInstanceCompleted = "instance.completed"
	EventTypeInstanceStatusChanged = "instance.status_changed"
	EventTypeOrphanRecovered   = "instance.orphan_recovered"

	ResourceTypeJob      = "job"
	ResourceTypeInstance = "instance"
	ResourceTypeAudit    = "audit"
)
