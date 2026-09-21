package middleware

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"orbitjob/internal/platform/metrics"
)

// unmatchedRoute is the path label for requests that hit no registered route
// (the 404 path, where gin's FullPath is empty). A literal keeps the series
// bounded instead of turning the raw request path into a label value.
const unmatchedRoute = "unmatched"

// RequestMetrics counts every HTTP request the router serves, including the
// ones later middleware short-circuit: an auth rejection and a rate-limit 429
// are exactly the traffic a 5xx alert must not be blind to, so this
// middleware has to sit at the front of the chain and observe the status the
// client actually received.
//
// The path label is gin's route template (c.FullPath, for example
// /api/v1/jobs/:id), never the raw request path, so ids in URLs cannot grow
// the series without bound.
func RequestMetrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		path := c.FullPath()
		if path == "" {
			path = unmatchedRoute
		}
		metrics.HTTPRequestsTotal.WithLabelValues(
			c.Request.Method,
			path,
			strconv.Itoa(c.Writer.Status()),
		).Inc()
	}
}
