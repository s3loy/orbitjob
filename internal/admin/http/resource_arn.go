package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/gin-gonic/gin"

	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/domain/policy"
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

// Resolve ownership before the guard evaluates the target ARN. The exact same
// resolved identity travels to the handler; no tenant identity grants authority.

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

// requestTenant reads the same tenant selector handlers consume without consuming
// the body. Conflicting selectors fail closed rather than authorizing one target
// and executing against another.
func requestTenant(c *gin.Context) (string, error) {
	explicit := c.Query("tenant_id")
	if c.Request.Body != nil && c.ContentType() == "application/json" {
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
			if body.TenantID != "" {
				if explicit != "" && explicit != body.TenantID {
					return "", middleware.ErrTenantMismatch
				}
				explicit = body.TenantID
			}
		}
	}
	return middleware.ResolveEffectiveTenant(c, explicit)
}
