package http

import (
	"testing"

	"orbitjob/internal/core/domain/policy"
)

// TestEveryRouteDeclaresAPermission is the guard that keeps this design from
// eroding. Adding a route without naming the action it needs fails here, rather
// than silently shipping an endpoint that the authorization middleware never
// sees because its permission is empty.
func TestEveryRouteDeclaresAPermission(t *testing.T) {
	public := map[string]bool{
		"/healthz":      true,
		"/metrics":      true,
		"/openapi.json": true,
	}
	for _, route := range adminAPIRoutes() {
		if public[route.path] {
			if route.permission != "" {
				t.Errorf("public route %s should carry no permission, got %q", route.path, route.permission)
			}
			continue
		}
		if route.permission == "" {
			t.Errorf("route %s %s declares no permission", route.method, route.path)
		}
		if route.resource == nil {
			t.Errorf("route %s %s declares a permission but no resource resolver", route.method, route.path)
		}
	}
}

// TestEveryRouteUsesAKnownAction closes the loop between the two action lists.
// A route naming an action outside the registry is unreachable: no tenant may
// create a policy that grants it, so no key can ever hold it. The endpoint
// would exist, be documented, and deny everyone -- a bug that looks like a
// permissions problem and gets debugged for an afternoon.
func TestEveryRouteUsesAKnownAction(t *testing.T) {
	for _, route := range adminAPIRoutes() {
		if route.permission == "" {
			continue
		}
		if !policy.IsKnownAction(route.permission) {
			t.Errorf("route %s %s requires %q, which is not in policy.KnownActions",
				route.method, route.path, route.permission)
		}
	}
}
