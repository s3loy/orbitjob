package http

import (
	"github.com/gin-gonic/gin"

	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/domain/policy"
)

// testTenantMiddleware injects a simulated tenant onto the request and grants
// it everything, so handler unit tests exercise business logic without also
// satisfying the authorization layer.
func testTenantMiddleware(tenantID string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := middleware.WithTenantID(c.Request.Context(), tenantID, middleware.TenantSourceHeader)
		c.Request = c.Request.WithContext(ctx)
		grantAllPolicies(c, tenantID)
		c.Next()
	}
}

// grantAllPolicies installs a permissive grant on the request, so handler unit
// tests can exercise business logic without also satisfying the authorization
// layer.
//
// This is not a hole in the tests: the authorization rules are covered where
// they live. middleware/authorize_test.go tests the guard's decisions, and the
// integration suite exercises real keys, real policies and real boundaries
// against a database. Here the question is only "does the handler do its job".
func grantAllPolicies(c *gin.Context, tenantID string) {
	middleware.SetPrincipal(c, middleware.Principal{TenantID: tenantID, Kind: middleware.KindTenant})
	middleware.SetDocuments(c, []policy.Document{{Statement: []policy.Statement{
		{
			Effect:   policy.EffectAllow,
			Action:   []string{policy.WildcardAction},
			Resource: []string{"orbitjob:*:*:*/*"},
		},
	}}})
}

// testRouter returns a gin engine with a simulated tenant already injected,
// suitable for handler-level tests that bypass production auth middleware.
func testRouter(handler *Handler) *gin.Engine {
	r := gin.New()
	r.Use(testTenantMiddleware("tenant-a"))
	handler.Register(r)
	return r
}
