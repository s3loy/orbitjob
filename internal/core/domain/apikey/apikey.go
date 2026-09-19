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

// PersistInput is everything needed to write one key together with its grants.
//
// The two travel as one value because they must be written in one transaction:
// a key stored without its policies can do nothing, and a policy bound to a key
// that was never written is a dangling grant.
type PersistInput struct {
	ID               string
	TenantID         string
	ResourceGroupID  string
	KeyHash          string
	KeyPrefix        string
	PolicyIDs        []string
	BoundaryPolicyID string
	// BoundBy records which principal attached the policies. "Who can create
	// bindings" is itself a privilege-escalation vector, so the answer has to
	// survive in the data rather than only in a log line.
	BoundBy string
}
