// Package apikey defines the API key domain type used by admin store and app layers.
package apikey

import "time"

// APIKey is the read-only domain representation of an API key record.
type APIKey struct {
	ID        string
	TenantID  string
	KeyPrefix string
	CreatedAt time.Time
	RevokedAt *time.Time
}
