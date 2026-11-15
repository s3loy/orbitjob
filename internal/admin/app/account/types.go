package account

import (
	"context"
	"errors"
	"time"

	"orbitjob/internal/core/domain/policy"
)

var (
	ErrUnauthenticated = errors.New("authentication required")
	ErrForbidden       = errors.New("account operation forbidden")
	ErrInvalid         = errors.New("invalid account input or token")
	ErrConflict        = errors.New("account already exists or state conflicts")
	ErrLastAdmin       = errors.New("cannot remove the last active installation administrator")
	ErrRecentAuth      = errors.New("recent password authentication required")
)

const (
	RoleAdmin          = "admin"
	RoleOperator       = "operator"
	RoleViewer         = "viewer"
	RecentAuthLifetime = 15 * time.Minute
)

type User struct {
	ID                string    `json:"id"`
	Username          string    `json:"username"`
	DisplayName       string    `json:"display_name"`
	Status            string    `json:"status"`
	InstallationAdmin bool      `json:"installation_admin"`
	CreatedAt         time.Time `json:"created_at"`
}

type Session struct {
	ID           string    `json:"id"`
	User         User      `json:"user"`
	ExpiresAt    time.Time `json:"expires_at"`
	RecentAuthAt time.Time `json:"recent_auth_at"`
	Remember     bool      `json:"remember"`
	CreatedAt    time.Time `json:"created_at"`
	CSRFToken    string    `json:"csrf_token,omitempty"`
}

type Workspace struct {
	Status string `json:"status,omitempty"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Role   string `json:"role"`
}

type Membership struct {
	User             User     `json:"user"`
	TenantID         string   `json:"tenant_id"`
	Role             string   `json:"role"`
	Status           string   `json:"status"`
	PolicyIDs        []string `json:"policy_ids"`
	BoundaryPolicyID string   `json:"boundary_policy_id,omitempty"`
}

type Grant struct {
	TenantID         string   `json:"tenant_id"`
	Role             string   `json:"role"`
	Username         string   `json:"username,omitempty"`
	PolicyIDs        []string `json:"policy_ids,omitempty"`
	BoundaryPolicyID string   `json:"boundary_policy_id,omitempty"`
}

type Capability struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	ID        string    `json:"id"`
}

type UserChange struct {
	Status            *string `json:"status,omitempty"`
	InstallationAdmin *bool   `json:"installation_admin,omitempty"`
	DisplayName       *string `json:"display_name,omitempty"`
}

type MemberChange struct {
	Status           *string  `json:"status,omitempty"`
	Role             *string  `json:"role,omitempty"`
	PolicyIDs        []string `json:"policy_ids,omitempty"`
	BoundaryPolicyID *string  `json:"boundary_policy_id,omitempty"`
}

// Repository executes the fixed identity operations implemented by the database.
// It never returns password hashes or bearer secrets to transport DTOs.
type Repository interface {
	Call(ctx context.Context, operation string, input any, output any) error
	Transaction(ctx context.Context, fn func(Repository) error) error
}

type Documents struct {
	Grants   []policy.Document
	Boundary *policy.Document
}
