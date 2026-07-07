package middleware

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"
)

type contextKey int

const (
	tenantIDKey contextKey = iota
	tenantSourceKey
	idempotencyKeyKey
)

const (
	TenantSourceAPIKey  = "api_key"
	TenantSourceHeader  = "x-tenant-id"
	TenantSourceDefault = "default"
)

// ErrTenantRequired is returned by ResolveTenantID when no tenant can be determined.
var ErrTenantRequired = errors.New("tenant required")

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

func WithIdempotencyKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, idempotencyKeyKey, key)
}

func IdempotencyKey(ctx context.Context) string {
	key, _ := ctx.Value(idempotencyKeyKey).(string)
	return key
}
