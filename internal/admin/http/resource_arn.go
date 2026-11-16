package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/gin-gonic/gin"

	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/domain/apikey"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

// errNoPrincipal is returned when a resource resolver runs before auth. It is a
// wiring defect: every non-public route must sit behind the auth middleware.
var errNoPrincipal = errors.New("no authenticated principal")

// tenantScoped resolves the exact effective request tenant and credential group.
// Query-layer row scope remains an independent restriction after this guard.
func tenantScoped(resourceType string) middleware.ResourceResolver {
	return func(c *gin.Context) (policy.ARN, error) {
		p, ok := middleware.PrincipalFrom(c)
		if !ok {
			return policy.ARN{}, errNoPrincipal
		}
		group := p.ResourceGroupID
		if group == "" {
			group = "*"
		}
		id := c.Param("id")
		if id == "" {
			id = c.Param("run_id")
		}
		if id == "" {
			id = "*"
		}
		tenantID, err := requestTenant(c)
		if err != nil {
			return policy.ARN{}, err
		}
		return policy.ARN{Tenant: tenantID, Group: group, Type: resourceType, ID: id}, nil
	}
}

type apiKeyTargetContextKey struct{}

// Resolve ownership before the guard evaluates the target ARN. The exact same
// resolved identity travels to the handler; no tenant identity grants authority.
func apiKeyRevokeResource(h *Handler) middleware.ResourceResolver {
	return func(c *gin.Context) (policy.ARN, error) {
		p, ok := middleware.PrincipalFrom(c)
		if !ok {
			return policy.ARN{}, errNoPrincipal
		}
		var path APIKeyURI
		if err := c.ShouldBindUri(&path); err != nil {
			return policy.ARN{}, validation.New("id", "must identify an API key")
		}
		target, err := h.revokeAPIKeyUC.Target(c.Request.Context(), path.ID)
		if err != nil {
			return policy.ARN{}, err
		}
		if target.ID != path.ID || target.TenantID == "" {
			return policy.ARN{}, errors.New("invalid resolved key scope")
		}
		if p.ResourceGroupID != "" && p.ResourceGroupID != target.ResourceGroupID {
			return policy.ARN{}, &resource.NotFoundError{Resource: "api_key", ID: path.ID}
		}
		group := target.ResourceGroupID
		if group == "" {
			group = "*"
		}
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), apiKeyTargetContextKey{}, target))
		return policy.ARN{Tenant: target.TenantID, Group: group, Type: "apikey", ID: path.ID}, nil
	}
}

func resolvedAPIKeyTarget(c *gin.Context) (apikey.APIKey, bool) {
	target, ok := c.Request.Context().Value(apiKeyTargetContextKey{}).(apikey.APIKey)
	return target, ok
}

// tenantResource resolves an ARN for the tenant object itself. The tenant
// segment is the tenant named in the path, so reading another tenant requires a
// policy that reaches it -- which a tenant-scoped grant does not.
func tenantResource(c *gin.Context) (policy.ARN, error) {
	id := c.Param("id")
	if id == "" {
		// Collection endpoints (list, create) target the tenant namespace as a
		// whole, which only a policy covering every tenant matches.
		id = "*"
	}
	return policy.ARN{Tenant: id, Group: "*", Type: "tenant", ID: id}, nil
}

// apiKeyResource resolves an ARN for the keys belonging to the tenant in the
// path, or for one key by its own id.
func apiKeyResource(c *gin.Context) (policy.ARN, error) {
	tenantID := c.Param("id")
	if tenantID == "" {
		return policy.ARN{Tenant: "*", Group: "*", Type: "apikey", ID: "*"}, nil
	}
	keyID := c.Param("key_id")
	if keyID == "" {
		keyID = "*"
	}
	return policy.ARN{Tenant: tenantID, Group: "*", Type: "apikey", ID: keyID}, nil
}

type requestTenantContextKey struct{}

// requestTenant retains the authorized tenant before JSON binding consumes the
// body. JSON selectors are checked regardless of the declared content type.
func requestTenant(c *gin.Context) (string, error) {
	return resolveRequestTenant(c, "")
}

func resolveRequestTenant(c *gin.Context, boundSelector string) (string, error) {
	boundSelector = strings.TrimSpace(boundSelector)
	if tenantID, ok := c.Request.Context().Value(requestTenantContextKey{}).(string); ok {
		if boundSelector != "" && boundSelector != tenantID {
			return "", middleware.ErrTenantMismatch
		}
		return tenantID, nil
	}
	explicit := strings.TrimSpace(c.Query("tenant_id"))
	merge := func(selector string) error {
		selector = strings.TrimSpace(selector)
		if selector != "" {
			if explicit != "" && explicit != selector {
				return middleware.ErrTenantMismatch
			}
			explicit = selector
		}
		return nil
	}
	if err := merge(boundSelector); err != nil {
		return "", err
	}
	if c.Request.Body != nil {
		raw, err := io.ReadAll(c.Request.Body)
		c.Request.Body = io.NopCloser(bytes.NewReader(raw))
		if err != nil {
			return "", err
		}
		if len(raw) > 0 {
			var body struct {
				TenantID string `json:"tenant_id"`
			}
			if err := json.Unmarshal(raw, &body); err != nil {
				return "", validation.New("body", "must contain exactly one valid JSON object")
			}
			if err := merge(body.TenantID); err != nil {
				return "", err
			}
		}
	}
	tenantID, err := middleware.ResolveEffectiveTenant(c, explicit)
	if err != nil {
		return "", err
	}
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), requestTenantContextKey{}, tenantID))
	return tenantID, nil
}
