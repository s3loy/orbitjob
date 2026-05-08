package middleware

import "context"

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

func WithTenantID(ctx context.Context, tenantID, source string) context.Context {
	ctx = context.WithValue(ctx, tenantIDKey, tenantID)
	return context.WithValue(ctx, tenantSourceKey, source)
}

func TenantID(ctx context.Context) (string, string) {
	tid, _ := ctx.Value(tenantIDKey).(string)
	src, _ := ctx.Value(tenantSourceKey).(string)
	return tid, src
}

func WithIdempotencyKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, idempotencyKeyKey, key)
}

func IdempotencyKey(ctx context.Context) string {
	key, _ := ctx.Value(idempotencyKeyKey).(string)
	return key
}
