package middleware

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"errors"
	"orbitjob/internal/admin/app/account"
	"orbitjob/internal/admin/http/apperror"
	"orbitjob/internal/core/domain/audit"
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
	DB              *sql.DB
	Accounts        HumanAuthenticator
	ConsoleSecurity ConsoleSecurity
	// Documents resolves a key's effective policies. Wiring it is optional but
	// a deployment without it grants nothing, since Require evaluates whatever
	// this produces.
	Documents DocumentLoader

	// MaxConcurrent bounds authentication SQL and bcrypt work before tenant limits.
	MaxConcurrent int
	once          sync.Once
	slots         chan struct{}
}

// DocumentLoader resolves the effective policy documents for a key. The store
// layer implements it; it is injected rather than imported so this package
// keeps its position above the store in the dependency order.
type HumanAuthenticator interface {
	Authenticate(context.Context, string) (*account.Session, error)
	Membership(context.Context, string, string) (account.Membership, error)
	EffectiveDocuments(context.Context, string, string) ([]policy.Document, error)
}

type DocumentLoader interface {
	EffectiveDocuments(ctx context.Context, tenantID, keyID string) ([]policy.Document, error)
}

// PlatformDocumentLoader is used only after authentication selected a
// tenant-NULL platform principal. It never receives a request target tenant.
type PlatformDocumentLoader interface {
	EffectivePlatformDocuments(ctx context.Context, keyID string) ([]policy.Document, error)
}

func NewAuth(db *sql.DB) *Auth {
	return &Auth{DB: db, MaxConcurrent: 32}
}

func (a *Auth) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Public endpoints bypass auth entirely.
		if isPublicPath(c.Request.URL.Path) {
			c.Next()
			return
		}

		a.once.Do(func() {
			capacity := a.MaxConcurrent
			if capacity < 1 || capacity > 256 {
				capacity = 32
			}
			a.slots = make(chan struct{}, capacity)
		})
		select {
		case a.slots <- struct{}{}:
		default:
			apperror.Write(c, http.StatusTooManyRequests, apperror.APIError{Code: apperror.CodeRateLimited, Message: "authentication capacity is busy; retry later"})
			return
		}
		authCtx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		principal, src, ok, authErr := a.resolveTenant(authCtx, c)
		if authErr != nil {
			cancel()
			<-a.slots
			c.Header("Retry-After", "1")
			apperror.Write(c, http.StatusServiceUnavailable, apperror.APIError{Code: apperror.CodeServiceUnavailable, Message: "authentication is temporarily unavailable; keep your current credential"})
			return
		}
		if ok {
			ctx := WithTenantID(c.Request.Context(), principal.TenantID, src)
			ctx = audit.WithActor(ctx, principal.ActorID(), principal.ActorType())
			c.Request = c.Request.WithContext(ctx)
			SetPrincipal(c, principal)
			if err := a.loadDocuments(authCtx, c, principal); err != nil {
				cancel()
				<-a.slots
				apperror.Write(c, http.StatusServiceUnavailable, apperror.APIError{
					Code:    apperror.CodeServiceUnavailable,
					Message: "permissions are temporarily unavailable",
				})
				c.Abort()
				return
			}
			cancel()
			<-a.slots
			c.Next()
			return
		}
		cancel()
		<-a.slots
		if c.IsAborted() {
			return
		}
		// Auth required but failed → abort.
		apperror.Write(c, http.StatusUnauthorized, apperror.APIError{
			Code:    apperror.CodeUnauthorized,
			Message: "valid authentication required",
		})
	}
}

func isPublicPath(path string) bool {
	if strings.HasPrefix(path, "/api/v1/auth/") {
		return true
	}
	switch path {
	case "/healthz", "/openapi.json", "/metrics":
		return true
	}
	return false
}

// resolveTenant distinguishes rejected credentials from an unavailable identity store.
func (a *Auth) resolveTenant(ctx context.Context, c *gin.Context) (Principal, string, bool, error) {
	authValues := c.Request.Header.Values("Authorization")
	authHeader := c.GetHeader("Authorization")
	if len(authValues) > 0 {
		if len(authValues) != 1 || authHeader == "" {
			return Principal{}, "", false, nil
		}
		if !strings.HasPrefix(authHeader, "Bearer ") {
			return Principal{}, "", false, nil
		}
		key := strings.TrimPrefix(authHeader, "Bearer ")
		row, ok, err := a.lookupAPIKey(ctx, key)
		if err != nil {
			return Principal{}, "", false, err
		}
		if ok {
			return principalFromRow(row), TenantSourceAPIKey, true, nil
		}
	}
	if len(authValues) > 0 || a.Accounts == nil {
		return Principal{}, "", false, nil
	}
	secret, ok := a.ConsoleSecurity.SessionSecret(c.Request)
	if !ok {
		return Principal{}, "", false, nil
	}
	session, err := a.Accounts.Authenticate(ctx, secret)
	if errors.Is(err, account.ErrUnauthenticated) {
		return Principal{}, "", false, nil
	}
	if err != nil {
		return Principal{}, "", false, err
	}
	if !SafeMethod(c.Request.Method) && !a.ConsoleSecurity.CheckCSRF(c.Request, session.CSRFToken) {
		apperror.Write(c, http.StatusForbidden, apperror.APIError{Code: apperror.CodeForbidden, Message: "same-origin session and CSRF token required"})
		return Principal{}, "", false, nil
	}
	p := Principal{SubjectType: "user", UserID: session.User.ID, Username: session.User.Username, DisplayName: session.User.DisplayName, InstallationAdmin: session.User.InstallationAdmin, SessionID: session.ID, RecentAuthAt: session.RecentAuthAt, Kind: KindTenant}
	target := strings.TrimSpace(c.Query("tenant_id"))
	if strings.HasPrefix(c.GetHeader("Content-Type"), "application/json") && c.Request.Body != nil {
		body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20))
		if err != nil {
			apperror.Write(c, http.StatusRequestEntityTooLarge, apperror.APIError{Code: apperror.CodeValidation, Message: "request body too large"})
			return Principal{}, "", false, nil
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		var selector struct {
			TenantID string `json:"tenant_id"`
		}
		if json.Unmarshal(body, &selector) == nil && selector.TenantID != "" {
			if target != "" && target != selector.TenantID {
				apperror.Write(c, http.StatusForbidden, apperror.APIError{Code: apperror.CodeForbidden, Message: "conflicting workspace selectors"})
				return Principal{}, "", false, nil
			}
			target = selector.TenantID
		}
	}
	// Explicit management routes name their target before their resource guard runs.
	path := strings.TrimPrefix(c.Request.URL.Path, "/api/v1/tenants/")
	if path != c.Request.URL.Path {
		if id, _, _ := strings.Cut(path, "/"); id != "" {
			if target != "" && target != id {
				apperror.Write(c, http.StatusForbidden, apperror.APIError{Code: apperror.CodeForbidden, Message: "conflicting workspace selectors"})
				return Principal{}, "", false, nil
			}
			target = id
		}
	}
	metadataOnly := p.InstallationAdmin && c.Request.Method == http.MethodGet && strings.HasPrefix(c.Request.URL.Path, "/api/v1/tenants/") && !strings.Contains(strings.TrimPrefix(c.Request.URL.Path, "/api/v1/tenants/"), "/")
	if target != "" {
		member, err := a.Accounts.Membership(ctx, p.UserID, target)
		if metadataOnly && errors.Is(err, account.ErrForbidden) {
			return p, TenantSourceSession, true, nil
		}
		if errors.Is(err, account.ErrForbidden) || errors.Is(err, account.ErrUnauthenticated) {
			apperror.Write(c, http.StatusForbidden, apperror.APIError{Code: apperror.CodeForbidden, Message: "workspace membership required"})
			return Principal{}, "", false, nil
		}
		if err != nil {
			return Principal{}, "", false, err
		}
		p.TenantID = target
		p.MembershipRole = member.Role
		p.BoundaryPolicyID = member.BoundaryPolicyID
	}
	return p, TenantSourceSession, true, nil
}

// loadDocuments resolves the credential's grants once, so every authorization
// check on this request sees the same set.
//
// A deployment that wires no loader authenticates requests but grants nothing,
// which fails closed: handlers still deny rather than fall open.
func (a *Auth) loadDocuments(ctx context.Context, c *gin.Context, p Principal) error {
	if p.IsHuman() {
		if p.TenantID == "" {
			SetDocuments(c, nil)
			return nil
		}
		docs, err := a.Accounts.EffectiveDocuments(ctx, p.UserID, p.TenantID)
		if err != nil {
			return err
		}
		SetDocuments(c, docs)
		return nil
	}
	if a.Documents == nil {
		SetDocuments(c, nil)
		return nil
	}
	var docs []policy.Document
	var err error
	if p.Kind == KindPlatform && p.TenantID == "" {
		loader, ok := a.Documents.(PlatformDocumentLoader)
		if !ok {
			SetDocuments(c, nil)
			return nil
		}
		docs, err = loader.EffectivePlatformDocuments(ctx, p.KeyID)
	} else {
		docs, err = a.Documents.EffectiveDocuments(ctx, p.TenantID, p.KeyID)
	}
	if err != nil {
		return fmt.Errorf("load effective documents for key %s: %w", p.KeyID, err)
	}
	SetDocuments(c, docs)
	return nil
}

// validateAPIKey is retained for callers that only need a credential match.
func (a *Auth) validateAPIKey(ctx context.Context, key string) (apiKeyRow, bool) {
	row, ok, _ := a.lookupAPIKey(ctx, key)
	return row, ok
}

func (a *Auth) lookupAPIKey(ctx context.Context, key string) (apiKeyRow, bool, error) {
	if !strings.HasPrefix(key, "otj_") || len(key) < 12 || len(key) > 256 {
		return apiKeyRow{}, false, nil
	}
	if a.DB == nil {
		return apiKeyRow{}, false, fmt.Errorf("authentication database is not configured")
	}
	prefix := key[:12]
	rows, err := a.DB.QueryContext(ctx, `
  SELECT id, tenant_id, kind, resource_group_id, boundary_policy_id, key_hash, revoked, expired
  FROM orbitjob_auth_api_key($1)
 `, prefix)
	if err != nil {
		return apiKeyRow{}, false, fmt.Errorf("query authentication store: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r apiKeyRow
		if err := rows.Scan(&r.ID, &r.TenantID, &r.Kind, &r.ResourceGroupID, &r.BoundaryPolicyID, &r.KeyHash, &r.RevokedAt, &r.ExpiresAt); err != nil {
			return apiKeyRow{}, false, fmt.Errorf("read authentication record: %w", err)
		}
		if r.RevokedAt != nil || r.ExpiresAt != nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return apiKeyRow{}, false, err
		}
		if bcrypt.CompareHashAndPassword([]byte(r.KeyHash), []byte(key)) == nil {
			return r, true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return apiKeyRow{}, false, fmt.Errorf("read authentication store: %w", err)
	}
	return apiKeyRow{}, false, nil
}

// principalFromRow turns a validated key row into the request principal.
func principalFromRow(r apiKeyRow) Principal {
	p := Principal{SubjectType: "api_key", KeyID: r.ID, Kind: r.Kind}
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
