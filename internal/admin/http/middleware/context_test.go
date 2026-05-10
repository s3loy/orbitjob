package middleware

import (
	"context"
	"testing"
)

func TestIdempotencyKey_RoundTrip(t *testing.T) {
	ctx := context.Background()

	// Initially empty.
	if key := IdempotencyKey(ctx); key != "" {
		t.Errorf("expected empty key, got %q", key)
	}

	// Set key.
	ctx = WithIdempotencyKey(ctx, "idem-abc-123")

	// Retrieve key.
	if key := IdempotencyKey(ctx); key != "idem-abc-123" {
		t.Errorf("expected idem-abc-123, got %q", key)
	}
}

func TestIdempotencyKey_EmptyValue(t *testing.T) {
	ctx := WithIdempotencyKey(context.Background(), "")
	if key := IdempotencyKey(ctx); key != "" {
		t.Errorf("expected empty key, got %q", key)
	}
}

func TestIdempotencyKey_Overwrite(t *testing.T) {
	ctx := context.Background()
	ctx = WithIdempotencyKey(ctx, "first")
	ctx = WithIdempotencyKey(ctx, "second")
	if key := IdempotencyKey(ctx); key != "second" {
		t.Errorf("expected second, got %q", key)
	}
}

func TestIdempotencyKey_NotOverlappingTenant(t *testing.T) {
	ctx := context.Background()
	ctx = WithIdempotencyKey(ctx, "idem-key")
	ctx = WithTenantID(ctx, "tenant-1", TenantSourceHeader)

	// Tenant ID should not affect IdempotencyKey.
	if key := IdempotencyKey(ctx); key != "idem-key" {
		t.Errorf("expected idem-key, got %q", key)
	}
	tid, src := TenantID(ctx)
	if tid != "tenant-1" {
		t.Errorf("expected tenant-1, got %q", tid)
	}
	if src != TenantSourceHeader {
		t.Errorf("expected x-tenant-id, got %q", src)
	}
}

func TestIdempotencyKey_NotSet(t *testing.T) {
	ctx := context.Background()
	ctx = WithTenantID(ctx, "tenant-x", TenantSourceAPIKey)

	// IdempotencyKey should be empty when not set.
	if key := IdempotencyKey(ctx); key != "" {
		t.Errorf("expected empty key, got %q", key)
	}
}
