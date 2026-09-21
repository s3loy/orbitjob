package http

import (
	"github.com/gin-gonic/gin"

	"orbitjob/internal/admin/http/middleware"
)

// permissionRouter prepends an authorization guard to every route registered
// through it. Wrapping the router rather than rewriting each register closure
// keeps the route table unchanged and makes the guard impossible to forget: a
// route is protected by virtue of declaring a permission, not by remembering to
// add a middleware.
type permissionRouter struct {
	gin.IRouter
	guard gin.HandlerFunc
}

func (p permissionRouter) GET(path string, handlers ...gin.HandlerFunc) gin.IRoutes {
	return p.IRouter.GET(path, prepend(p.guard, handlers)...)
}

func (p permissionRouter) POST(path string, handlers ...gin.HandlerFunc) gin.IRoutes {
	return p.IRouter.POST(path, prepend(p.guard, handlers)...)
}

func (p permissionRouter) PUT(path string, handlers ...gin.HandlerFunc) gin.IRoutes {
	return p.IRouter.PUT(path, prepend(p.guard, handlers)...)
}

func (p permissionRouter) PATCH(path string, handlers ...gin.HandlerFunc) gin.IRoutes {
	return p.IRouter.PATCH(path, prepend(p.guard, handlers)...)
}

func (p permissionRouter) DELETE(path string, handlers ...gin.HandlerFunc) gin.IRoutes {
	return p.IRouter.DELETE(path, prepend(p.guard, handlers)...)
}

// Group returns a router whose routes are guarded the same way, so a future
// nested group cannot escape the check.
func (p permissionRouter) Group(relativePath string, handlers ...gin.HandlerFunc) *gin.RouterGroup {
	return p.IRouter.Group(relativePath, handlers...)
}

// Any routes registered through Handle must be guarded too; the embedded
// interface supplies every other method unchanged.
func (p permissionRouter) Handle(httpMethod, relativePath string, handlers ...gin.HandlerFunc) gin.IRoutes {
	return p.IRouter.Handle(httpMethod, relativePath, prepend(p.guard, handlers)...)
}

func prepend(guard gin.HandlerFunc, handlers []gin.HandlerFunc) []gin.HandlerFunc {
	out := make([]gin.HandlerFunc, 0, len(handlers)+1)
	out = append(out, guard)
	return append(out, handlers...)
}

// guardFor builds the authorization guard for a route, or nil for the public
// endpoints that declare no permission.
func guardFor(route routeDefinition) gin.HandlerFunc {
	if route.permission == "" {
		return nil
	}
	return middleware.Require(route.permission, route.resource)
}
