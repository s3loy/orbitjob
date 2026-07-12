package middleware

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"orbitjob/internal/admin/http/apperror"
)

type apiKeyRow struct {
	ID        string
	TenantID  string
	KeyHash   string
	RevokedAt *string
	ExpiresAt *string
}

// Auth extracts tenant_id from a Bearer token validated against api_keys.
type Auth struct {
	DB *sql.DB
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

		tid, src, ok := a.resolveTenant(c)
		if ok {
			ctx := WithTenantID(c.Request.Context(), tid, src)
			c.Request = c.Request.WithContext(ctx)
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

// resolveTenant validates credentials. Returns (tenantID, source, ok).
// ok=true  → caller should set tenant in context and continue.
// ok=false → caller should abort with 401.
func (a *Auth) resolveTenant(c *gin.Context) (string, string, bool) {
	authHeader := c.GetHeader("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		key := strings.TrimPrefix(authHeader, "Bearer ")
		if tid, ok := a.validateAPIKey(c.Request.Context(), key); ok {
			return tid, TenantSourceAPIKey, true
		}
		return "", "", false
	}

	// No Bearer token → reject.
	return "", "", false
}

func (a *Auth) validateAPIKey(ctx context.Context, key string) (string, bool) {
	if a.DB == nil {
		return "", false
	}
	if !strings.HasPrefix(key, "otj_") || len(key) < 12 {
		return "", false
	}
	prefix := key[:12] // "otj_" + first 8 chars

	rows, err := a.DB.QueryContext(ctx, `
		SELECT id, tenant_id, key_hash, revoked, expired
		FROM orbitjob_auth_api_key($1)
	`, prefix)
	if err != nil {
		return "", false
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var r apiKeyRow
		if err := rows.Scan(&r.ID, &r.TenantID, &r.KeyHash, &r.RevokedAt, &r.ExpiresAt); err != nil {
			return "", false
		}
		if r.RevokedAt != nil || r.ExpiresAt != nil {
			continue
		}
		if bcrypt.CompareHashAndPassword([]byte(r.KeyHash), []byte(key)) == nil {
			return r.TenantID, true
		}
	}
	return "", false
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
