package middleware

import (
	"context"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"

	"orbitjob/internal/core/domain/policy"
)

type contextKey int

const (
	tenantIDKey contextKey = iota
	tenantSourceKey
	principalKey
	documentsKey
)

const (
	TenantSourceAPIKey  = "api_key"
	TenantSourceHeader  = "x-tenant-id"
	TenantSourceDefault = "default"
)

// ErrTenantRequired is returned by ResolveTenantID when no tenant can be determined.
var ErrTenantRequired = errors.New("tenant required")

// Principal kinds, mirroring the api_keys.kind check constraint.
const (
	KindTenant   = "tenant"
	KindPlatform = "platform"
)

// Principal is the authenticated identity behind a request. It carries the
// credential's own scope so handlers never have to re-read the key row.
type Principal struct {
	KeyID            string
	TenantID         string // empty for a platform principal
	Kind             string
	ResourceGroupID  string
	BoundaryPolicyID string
}

func (p Principal) ActorID() string   { return p.KeyID }
func (p Principal) ActorType() string { return "api_key" }

// IsPlatform reports whether this identity acts outside any tenant.
func (p Principal) IsPlatform() bool { return p.Kind == KindPlatform }

// SetPrincipal records the authenticated identity on the request. The auth
// middleware is the only caller; everything else reads it via PrincipalFrom.
func SetPrincipal(c *gin.Context, p Principal) {
	ctx := context.WithValue(c.Request.Context(), principalKey, p)
	c.Request = c.Request.WithContext(ctx)
}

// PrincipalFrom returns the authenticated identity, reporting false when the
// request never passed through auth.
func PrincipalFrom(c *gin.Context) (Principal, bool) {
	p, ok := c.Request.Context().Value(principalKey).(Principal)
	return p, ok
}

// SetDocuments records the effective policy documents for the request. They are
// resolved once per request so every authorization check sees the same grants.
func SetDocuments(c *gin.Context, docs []policy.Document) {
	ctx := context.WithValue(c.Request.Context(), documentsKey, docs)
	c.Request = c.Request.WithContext(ctx)
}

// Documents returns the request's effective policy documents, reporting false
// when they were never set -- a wiring defect the caller must not treat as
// "no permissions" or as "all permissions".
func Documents(c *gin.Context) ([]policy.Document, bool) {
	docs, ok := c.Request.Context().Value(documentsKey).([]policy.Document)
	return docs, ok
}

func WithTenantID(ctx context.Context, tenantID, source string) context.Context {
	ctx = context.WithValue(ctx, tenantIDKey, tenantID)
	return context.WithValue(ctx, tenantSourceKey, source)
}

// TenantID returns the tenant identifier and its source from the context.
func TenantID(ctx context.Context) (string, string) {
	tid, _ := ctx.Value(tenantIDKey).(string)
	src, _ := ctx.Value(tenantSourceKey).(string)
	return tid, src
}

// ResolveTenantID returns an explicit tenant_id from the request if provided,
// otherwise falls back to the tenant established by authentication. If neither
// is present it returns ErrTenantRequired.
func ResolveTenantID(c *gin.Context, fromRequest string) (string, error) {
	if fromRequest != "" {
		return fromRequest, nil
	}
	tid, _ := TenantID(c.Request.Context())
	if tid == "" {
		return "", ErrTenantRequired
	}
	return tid, nil
}

// ErrTenantMismatch denies a credential attempting to switch tenant scope.
var ErrTenantMismatch = errors.New("tenant mismatch")

// ResolveEffectiveTenant binds request targets to the authenticated credential.
// Platform credentials have no default tenant and must select one explicitly.
func ResolveEffectiveTenant(c *gin.Context, explicit string) (string, error) {
	explicit = strings.TrimSpace(explicit)
	p, ok := PrincipalFrom(c)
	if !ok {
		return "", ErrTenantRequired
	}
	if p.IsPlatform() {
		if explicit == "" {
			return "", ErrTenantRequired
		}
		return explicit, nil
	}
	if explicit != "" && explicit != p.TenantID {
		return "", ErrTenantMismatch
	}
	if p.TenantID == "" {
		return "", ErrTenantRequired
	}
	return p.TenantID, nil
}

// Resource lookup errors are classified without disclosing row existence.
var ErrResourceUnavailable = errors.New("resource unavailable")
var ErrAuthorizationLookup = errors.New("authorization lookup failed")
