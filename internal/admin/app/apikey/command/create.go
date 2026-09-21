package command

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"orbitjob/internal/core/domain/apikey"
	"orbitjob/internal/core/domain/audit"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/domain/validation"
	"orbitjob/internal/platform/scan"
)

const (
	apiKeyPrefix = "otj_"
	apiKeyMinLen = 12
)

// Caller describes who is creating a key and what they already hold. The
// guards need both: what may be granted depends on what the granter has.
type Caller struct {
	TenantID string
	// KeyID identifies the caller in the audit trail.
	KeyID string
	// ResourceGroupID is the scope the caller's own key is limited to, empty
	// when it is not scoped to a group.
	ResourceGroupID string
	Documents       []policy.Document
	// IsPlatform reports whether the caller acts outside any tenant.
	IsPlatform bool
	// BoundaryPolicyID names the boundary the caller is itself subject to. The
	// creator loads it and forces created keys to carry at least as much.
	BoundaryPolicyID string
}

// CreateInput is the control-plane command model for creating an API key.
type CreateInput struct {
	TenantID         string
	ResourceGroupID  string
	PolicyIDs        []string
	BoundaryPolicyID string
	Caller           Caller
}

// APIKeyCreateResult is the control-plane response model for a created API key.
// The full key is returned only once.
type APIKeyCreateResult struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	KeyPrefix string `json:"key_prefix"`
	CreatedAt string `json:"created_at"`
}

// apiKeyCreator persists the key, its grants and the record of the grant in one
// transaction. The event travels with the write rather than being recorded by a
// separate call, because a grant whose record can fail independently is a grant
// nobody can account for.
type apiKeyCreator interface {
	Create(ctx context.Context, in apikey.PersistInput, ev audit.Event) error
}

// policyReader loads a policy so its document can be checked against the
// caller's own grants before it is handed to anyone else.
type policyReader interface {
	Get(ctx context.Context, tenantID, id string) (policy.Record, error)
}

// groupReader reports whether a resource group belongs to a tenant. Scoping a
// key to a group that is not the tenant's own has to be refused: the foreign id
// would be stored unchecked and read back as a scope the tenant never had.
type groupReader interface {
	Exists(ctx context.Context, tenantID, groupID string) (bool, error)
}

// Creator creates API keys.
type Creator struct {
	repo        apiKeyCreator
	policies    policyReader
	groups      groupReader
	generateKey func() (string, error)
	hashKey     func(key []byte) ([]byte, error)
}

// NewCreator builds a Creator backed by repo.
func NewCreator(repo apiKeyCreator) *Creator {
	return &Creator{
		repo:        repo,
		generateKey: func() (string, error) { return generateAPIKey(rand.Reader) },
		hashKey:     func(key []byte) ([]byte, error) { return bcrypt.GenerateFromPassword(key, bcrypt.DefaultCost) },
	}
}

// WithPolicies supplies the reader the escalation guards need. Without it a
// caller may only create keys that carry no policies.
func (c *Creator) WithPolicies(reader policyReader) *Creator {
	c.policies = reader
	return c
}

// WithGroups supplies the resource group lookup. Without it a key may not name
// a group at all, which is the fail-closed direction: an unchecked group id
// would be stored and then used to filter every query the key makes.
func (c *Creator) WithGroups(reader groupReader) *Creator {
	c.groups = reader
	return c
}

// Create generates a new API key, hashes it, and persists it along with its
// grants.
//
// Both escalation guards run before anything is written:
//
//   - every policy being attached must already be held by the caller, so a key
//     cannot mint a more powerful key than itself;
//   - the new key's boundary must not exceed the caller's own, so a bounded
//     caller cannot escape its ceiling by creating a key that lacks one.
//
// Omitting either turns self-service into a privilege-escalation path.
func (c *Creator) Create(ctx context.Context, in CreateInput) (APIKeyCreateResult, error) {
	if err := c.checkGroup(ctx, in); err != nil {
		return APIKeyCreateResult{}, err
	}

	granted, err := c.resolvePolicies(ctx, in)
	if err != nil {
		return APIKeyCreateResult{}, err
	}
	if err := policy.CheckGrantable(in.Caller.Documents, granted); err != nil {
		return APIKeyCreateResult{}, err
	}

	callerBoundary, err := c.callerBoundary(ctx, in.Caller)
	if err != nil {
		return APIKeyCreateResult{}, err
	}
	requestedBoundary, err := c.resolveBoundary(ctx, in)
	if err != nil {
		return APIKeyCreateResult{}, err
	}
	boundary, err := policy.PropagateBoundary(callerBoundary, requestedBoundary)
	if err != nil {
		return APIKeyCreateResult{}, err
	}

	// Which boundary policy the new key should point at. When the caller named
	// none but is itself bounded, the key inherits the caller's boundary --
	// otherwise it would escape the caller's ceiling.
	boundaryID := in.BoundaryPolicyID
	if requestedBoundary == nil && boundary != nil {
		boundaryID = in.Caller.BoundaryPolicyID
	}

	key, err := c.generateKey()
	if err != nil {
		return APIKeyCreateResult{}, fmt.Errorf("generate api key: %w", err)
	}

	hash, err := c.hashKey([]byte(key))
	if err != nil {
		return APIKeyCreateResult{}, fmt.Errorf("hash api key: %w", err)
	}

	id := scan.GenerateID()
	prefix := keyPrefix(key)
	createdAt := time.Now().UTC()

	// The diff records the grants as granted, not as requested: a bare request
	// that inherited the caller's boundary must read back as bounded, or the
	// trail would suggest the new key was unconstrained.
	event := audit.Event{
		TenantID:     in.TenantID,
		ActorID:      in.Caller.KeyID,
		EventType:    audit.EventCreate,
		ResourceType: audit.ResourceAPIKey,
		ResourceID:   id,
		Diff: map[string]any{
			"policies":           policyIDsOrEmpty(in.PolicyIDs),
			"boundary_policy_id": boundaryID,
			"resource_group_id":  in.ResourceGroupID,
			"bound_by":           in.Caller.TenantID,
		},
	}
	if err := c.repo.Create(ctx, apikey.PersistInput{
		TenantID:         in.TenantID,
		ResourceGroupID:  in.ResourceGroupID,
		ID:               id,
		KeyHash:          string(hash),
		KeyPrefix:        prefix,
		PolicyIDs:        in.PolicyIDs,
		BoundaryPolicyID: boundaryID,
		BoundBy:          in.Caller.TenantID,
	}, event); err != nil {
		return APIKeyCreateResult{}, fmt.Errorf("create api key: %w", err)
	}

	return APIKeyCreateResult{
		ID:        id,
		Key:       key,
		KeyPrefix: prefix,
		CreatedAt: createdAt.Format(time.RFC3339),
	}, nil
}

// policyIDsOrEmpty keeps the audit diff readable: an absent grant list is
// recorded as an empty list rather than null, so a reviewer reading the trail
// sees "no policies" instead of having to decide what null meant.
func policyIDsOrEmpty(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}

func generateAPIKey(r io.Reader) (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return apiKeyPrefix + base64.URLEncoding.EncodeToString(b), nil
}

func keyPrefix(key string) string {
	if len(key) < apiKeyMinLen {
		return key
	}
	return key[:apiKeyMinLen]
}

// checkGroup decides which resource group a new key may be scoped to.
//
// Two rules, and both are needed:
//
//   - The new key stays inside the caller's own scope. A key granted
//     apikey:Create only over group "ci" could otherwise mint a key for group
//     "prod", or for no group at all -- which sees the whole tenant -- and act
//     through it. The route guard would have allowed the call, because the
//     caller does hold apikey:Create where its policy says it does; what it
//     must not hold is the ability to leave its scope. This is the subset rule
//     applied to the group dimension rather than the action dimension.
//   - The named group belongs to the tenant. The schema's foreign key only
//     proves the group exists somewhere, so without this a tenant could scope a
//     key to another tenant's group and then see nothing, because its own rows
//     never carry that group.
func (c *Creator) checkGroup(ctx context.Context, in CreateInput) error {
	callerGroup := strings.TrimSpace(in.Caller.ResourceGroupID)
	requested := strings.TrimSpace(in.ResourceGroupID)

	if callerGroup != "" && requested != callerGroup {
		return fmt.Errorf(
			"%w: caller is scoped to resource group %q and cannot grant %q",
			policy.ErrPrivilegeEscalation, callerGroup, requested)
	}
	if requested == "" {
		return nil
	}
	if c.groups == nil {
		return fmt.Errorf("resource group reader is not configured")
	}
	ok, err := c.groups.Exists(ctx, in.TenantID, requested)
	if err != nil {
		return fmt.Errorf("check resource group %s: %w", requested, err)
	}
	if !ok {
		return &validation.Error{
			Field:   "resource_group_id",
			Message: "does not belong to this tenant",
		}
	}
	return nil
}

// resolvePolicies loads the documents for the requested policy ids.
func (c *Creator) resolvePolicies(ctx context.Context, in CreateInput) ([]policy.Document, error) {
	if len(in.PolicyIDs) == 0 {
		return nil, nil
	}
	if c.policies == nil {
		return nil, fmt.Errorf("policy reader is not configured")
	}
	docs := make([]policy.Document, 0, len(in.PolicyIDs))
	for _, id := range in.PolicyIDs {
		rec, err := c.policies.Get(ctx, in.TenantID, id)
		if err != nil {
			return nil, fmt.Errorf("load policy %s: %w", id, err)
		}
		docs = append(docs, rec.Document)
	}
	return docs, nil
}

// callerBoundary loads the boundary the caller is subject to, so propagation can
// compare against it. A caller with no boundary id is unrestricted within its
// own grants.
func (c *Creator) callerBoundary(ctx context.Context, caller Caller) (*policy.Document, error) {
	if caller.BoundaryPolicyID == "" {
		return nil, nil
	}
	if c.policies == nil {
		return nil, fmt.Errorf("policy reader is not configured")
	}
	rec, err := c.policies.Get(ctx, caller.TenantID, caller.BoundaryPolicyID)
	if err != nil {
		return nil, fmt.Errorf("load caller boundary %s: %w", caller.BoundaryPolicyID, err)
	}
	return &rec.Document, nil
}

// resolveBoundary loads the boundary the caller asked for, if any.
func (c *Creator) resolveBoundary(ctx context.Context, in CreateInput) (*policy.Document, error) {
	if in.BoundaryPolicyID == "" {
		return nil, nil
	}
	if c.policies == nil {
		return nil, fmt.Errorf("policy reader is not configured")
	}
	rec, err := c.policies.Get(ctx, in.TenantID, in.BoundaryPolicyID)
	if err != nil {
		return nil, fmt.Errorf("load boundary policy %s: %w", in.BoundaryPolicyID, err)
	}
	return &rec.Document, nil
}
