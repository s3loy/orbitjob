// Package bootstrap ensures a default tenant and initial API key exist for
// first-time deployment. It is intentionally small and uses raw SQL so it can
// run before any repository layer is wired.
package bootstrap

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const (
	// DefaultTenantID is the deterministic ID of the bootstrap tenant.
	DefaultTenantID = "00000000000000000000000001"
	// DefaultAPIKeyID is the deterministic ID of the bootstrap API key.
	DefaultAPIKeyID = "00000000000000000000000002"
	// DefaultAPIKey is the key used when ADMIN_BOOTSTRAP_API_KEY is not set.
	// It is only suitable for local development.
	DefaultAPIKey = "otj_devkey_2026"

	defaultTenantSlug   = "default"
	defaultTenantName   = "Default"
	defaultTenantStatus = "active"

	defaultAPIKeyPermissions = "{}"

	defaultSecretName    = "bootstrap-api-key"
	defaultSecretDataKey = "api-key"

	minAPIKeyLength = 12
)

var (
	errAPIKeyTooShort = errors.New("bootstrap api key must be at least 12 characters")
	errAPIKeyRequired = errors.New("bootstrap api key is required when default key is disallowed")
)

// Options controls bootstrap behavior.
type Options struct {
	// Skip disables bootstrap entirely.
	Skip bool
	// APIKey overrides the default development key. Must be at least 12 chars.
	APIKey string
	// Writer persists the created API key to an external secret store.
	// nil means do not write the secret externally.
	Writer SecretWriter
	// DisallowDefaultKey prevents the built-in development key from being used
	// when APIKey is empty. It should be true in production.
	DisallowDefaultKey bool
}

// Result reports what bootstrap changed.
type Result struct {
	TenantCreated bool
	KeyCreated    bool
	MaskedKey     string
}

// hashPasswordFn is swappable for tests.
var hashPasswordFn = bcrypt.GenerateFromPassword

// EnsureDefault creates the default tenant and an initial API key if they do
// not already exist. It is safe to call on every startup.
func EnsureDefault(ctx context.Context, db *sql.DB, opts Options) (Result, error) {
	if opts.Skip {
		return Result{}, nil
	}

	key := opts.APIKey
	if key == "" {
		if opts.DisallowDefaultKey {
			return Result{}, errAPIKeyRequired
		}
		key = DefaultAPIKey
	}
	if len(key) < minAPIKeyLength {
		return Result{}, errAPIKeyTooShort
	}
	prefix := key[:minAPIKeyLength]

	hash, err := hashPasswordFn([]byte(key), bcrypt.DefaultCost)
	if err != nil {
		return Result{}, fmt.Errorf("hash api key: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, fmt.Errorf("begin bootstrap tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	res := Result{MaskedKey: maskKey(key)}
	if err = tx.QueryRowContext(ctx, `
		SELECT tenant_created, key_created
		FROM orbitjob_bootstrap_default($1, $2, $3, $4, $5, $6, $7, $8::jsonb)
	`, DefaultTenantID, defaultTenantSlug, defaultTenantName, defaultTenantStatus,
		DefaultAPIKeyID, string(hash), prefix, defaultAPIKeyPermissions,
	).Scan(&res.TenantCreated, &res.KeyCreated); err != nil {
		return Result{}, fmt.Errorf("ensure bootstrap defaults: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return Result{}, fmt.Errorf("commit bootstrap tx: %w", err)
	}

	if res.KeyCreated && opts.Writer != nil {
		if err := opts.Writer.Write(ctx, defaultSecretName, map[string]string{
			defaultSecretDataKey: key,
		}); err != nil {
			return Result{}, fmt.Errorf("write bootstrap secret: %w", err)
		}
	}

	return res, nil
}

func maskKey(key string) string {
	if len(key) > minAPIKeyLength {
		return key[:minAPIKeyLength] + "..."
	}
	return "..."
}
