package middleware

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type apiKeyRow struct {
	ID        string
	TenantID  string
	KeyHash   string
	RevokedAt *string
	ExpiresAt *string
}

// Auth extracts tenant_id from a Bearer token validated against api_keys.
// Falls back to X-OrbitJob-Tenant-Id header if no Bearer token is present.
type Auth struct {
	DB *sql.DB
}

func NewAuth(db *sql.DB) *Auth {
	return &Auth{DB: db}
}

func (a *Auth) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tid, src := a.resolveTenant(c)
		if tid != "" {
			ctx := WithTenantID(c.Request.Context(), tid, src)
			c.Request = c.Request.WithContext(ctx)
		}
		c.Next()
	}
}

func (a *Auth) resolveTenant(c *gin.Context) (string, string) {
	authHeader := c.GetHeader("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		key := strings.TrimPrefix(authHeader, "Bearer ")
		if tid, ok := a.validateAPIKey(c.Request.Context(), key); ok {
			return tid, TenantSourceAPIKey
		}
		_ = c.AbortWithError(http.StatusUnauthorized, fmt.Errorf("invalid api key"))
		return "", ""
	}

	// Fallback: X-OrbitJob-Tenant-Id header
	if tid := strings.TrimSpace(c.GetHeader("X-OrbitJob-Tenant-Id")); tid != "" {
		return tid, TenantSourceHeader
	}

	return "", TenantSourceDefault
}

func (a *Auth) validateAPIKey(ctx context.Context, key string) (string, bool) {
	if a.DB == nil {
		return "", false
	}
	if !strings.HasPrefix(key, "otj_") || len(key) < 12 {
		return "", false
	}
	prefix := key[:12] // "otj_" + first 8 chars

	row := a.DB.QueryRowContext(ctx, `
		SELECT ak.id, ak.tenant_id, ak.key_hash,
		       CASE WHEN ak.revoked_at IS NOT NULL THEN 'revoked' ELSE NULL END,
		       CASE WHEN ak.expires_at IS NOT NULL AND ak.expires_at < now() THEN 'expired' ELSE NULL END
		FROM api_keys ak
		JOIN tenants t ON t.id = ak.tenant_id
		WHERE ak.key_prefix = $1
		  AND ak.revoked_at IS NULL
		  AND t.status = 'active'
		ORDER BY ak.created_at DESC
		LIMIT 1
	`, prefix)

	var r apiKeyRow
	err := row.Scan(&r.ID, &r.TenantID, &r.KeyHash, &r.RevokedAt, &r.ExpiresAt)
	if err != nil {
		return "", false
	}
	if r.RevokedAt != nil || r.ExpiresAt != nil {
		return "", false
	}

	if err := bcrypt.CompareHashAndPassword([]byte(r.KeyHash), []byte(key)); err != nil {
		return "", false
	}

	return r.TenantID, true
}

// GetTenantID returns the tenant_id from the gin context, defaulting to "default".
func GetTenantID(c *gin.Context) string {
	tid, _ := TenantID(c.Request.Context())
	if tid == "" {
		return "default"
	}
	return tid
}

// GetTenantSource returns how the tenant was identified.
func GetTenantSource(c *gin.Context) string {
	_, src := TenantID(c.Request.Context())
	return src
}
