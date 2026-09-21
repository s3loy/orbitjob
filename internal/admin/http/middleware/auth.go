package middleware

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"orbitjob/internal/admin/http/apperror"
	"orbitjob/internal/core/domain/policy"
)

type apiKeyRow struct {
	ID               string
	TenantID         *string
	Kind             string
	ResourceGroupID  *string
	BoundaryPolicyID *string
	KeyHash          string
	RevokedAt        *string
	ExpiresAt        *string
}

// Auth extracts tenant_id from a Bearer token validated against api_keys.
type Auth struct {
	DB *sql.DB
	// Documents resolves a key's effective policies. Wiring it is optional but
	// a deployment without it grants nothing, since Require evaluates whatever
	// this produces.
	Documents DocumentLoader
}

// DocumentLoader resolves the effective policy documents for a key. The store
// layer implements it; it is injected rather than imported so this package
// keeps its position above the store in the dependency order.
type DocumentLoader interface {
	EffectiveDocuments(ctx context.Context, tenantID, keyID string) ([]policy.Document, error)
}

func NewAuth(db *sql.DB) *Auth {
	return &Auth{DB: db}
}

func (a *Auth) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Public endpoints bypass auth entirely.
		if isPublicPath(c.Request.URL.Path) {
			c.Next()
			return
		}

		principal, src, ok := a.resolveTenant(c)
		if ok {
			ctx := WithTenantID(c.Request.Context(), principal.TenantID, src)
			c.Request = c.Request.WithContext(ctx)
			SetPrincipal(c, principal)
			if err := a.loadDocuments(c, principal); err != nil {
				apperror.Write(c, http.StatusInternalServerError, apperror.APIError{
					Code:    apperror.CodeInternal,
					Message: "load permissions",
				})
				c.Abort()
				return
			}
			c.Next()
			return
		}
		// Auth required but failed → abort.
		apperror.Write(c, http.StatusUnauthorized, apperror.APIError{
			Code:    apperror.CodeUnauthorized,
			Message: "valid Bearer token required",
		})
	}
}

func isPublicPath(path string) bool {
	switch path {
	case "/healthz", "/openapi.json", "/metrics":
		return true
	}
	return false
}

// resolveTenant validates credentials. Returns (principal, source, ok).
// ok=true  → caller should set the principal in context and continue.
// ok=false → caller should abort with 401.
func (a *Auth) resolveTenant(c *gin.Context) (Principal, string, bool) {
	authHeader := c.GetHeader("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		key := strings.TrimPrefix(authHeader, "Bearer ")
		if row, ok := a.validateAPIKey(c.Request.Context(), key); ok {
			return principalFromRow(row), TenantSourceAPIKey, true
		}
		return Principal{}, "", false
	}

	// No Bearer token → reject.
	return Principal{}, "", false
}

// loadDocuments resolves the credential's grants once, so every authorization
// check on this request sees the same set.
//
// A deployment that wires no loader authenticates requests but grants nothing,
// which fails closed: handlers still deny rather than fall open.
func (a *Auth) loadDocuments(c *gin.Context, p Principal) error {
	if a.Documents == nil {
		SetDocuments(c, nil)
		return nil
	}
	docs, err := a.Documents.EffectiveDocuments(c.Request.Context(), p.TenantID, p.KeyID)
	if err != nil {
		return fmt.Errorf("load effective documents for key %s: %w", p.KeyID, err)
	}
	SetDocuments(c, docs)
	return nil
}

func (a *Auth) validateAPIKey(ctx context.Context, key string) (apiKeyRow, bool) {
	if a.DB == nil {
		return apiKeyRow{}, false
	}
	if !strings.HasPrefix(key, "otj_") || len(key) < 12 {
		return apiKeyRow{}, false
	}
	prefix := key[:12] // "otj_" + first 8 chars

	rows, err := a.DB.QueryContext(ctx, `
		SELECT id, tenant_id, kind, resource_group_id, boundary_policy_id, key_hash, revoked, expired
		FROM orbitjob_auth_api_key($1)
	`, prefix)
	if err != nil {
		return apiKeyRow{}, false
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var r apiKeyRow
		if err := rows.Scan(&r.ID, &r.TenantID, &r.Kind, &r.ResourceGroupID,
			&r.BoundaryPolicyID, &r.KeyHash, &r.RevokedAt, &r.ExpiresAt); err != nil {
			return apiKeyRow{}, false
		}
		if r.RevokedAt != nil || r.ExpiresAt != nil {
			continue
		}
		if bcrypt.CompareHashAndPassword([]byte(r.KeyHash), []byte(key)) == nil {
			return r, true
		}
	}
	return apiKeyRow{}, false
}

// principalFromRow turns a validated key row into the request principal.
func principalFromRow(r apiKeyRow) Principal {
	p := Principal{KeyID: r.ID, Kind: r.Kind}
	if r.TenantID != nil {
		p.TenantID = *r.TenantID
	}
	if r.ResourceGroupID != nil {
		p.ResourceGroupID = *r.ResourceGroupID
	}
	if r.BoundaryPolicyID != nil {
		p.BoundaryPolicyID = *r.BoundaryPolicyID
	}
	return p
}

// GetTenantID returns the tenant_id from the gin context. If no tenant has been
// set (e.g. the request is unauthenticated), it returns an empty string. Callers
// that require a tenant must handle the empty case explicitly.
func GetTenantID(c *gin.Context) string {
	tid, _ := TenantID(c.Request.Context())
	return tid
}

// GetTenantSource returns how the tenant was identified.
func GetTenantSource(c *gin.Context) string {
	_, src := TenantID(c.Request.Context())
	return src
}
