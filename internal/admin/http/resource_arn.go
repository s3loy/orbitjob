package http

import (
	"errors"

	"github.com/gin-gonic/gin"

	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/domain/policy"
)

// errNoPrincipal is returned when a resource resolver runs before auth. It is a
// wiring defect: every non-public route must sit behind the auth middleware.
var errNoPrincipal = errors.New("no authenticated principal")

// tenantScoped resolves an ARN for a resource owned by the caller's own tenant.
//
// The tenant segment is the authenticated tenant, so a policy naming another
// tenant never matches. The group segment is the caller's own scope, so a
// policy that names a specific group only matches a key scoped to it; an
// unscoped key carries "*" and is therefore unaffected by group-qualified
// policies. Which rows a key actually sees is enforced by the query layer, not
// here -- this decides whether the call is permitted at all.
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
		return policy.ARN{Tenant: p.TenantID, Group: group, Type: resourceType, ID: id}, nil
	}
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
