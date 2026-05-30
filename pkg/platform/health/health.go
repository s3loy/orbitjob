// Package health provides health check server utilities.
package health

import internal "orbitjob/internal/platform/health"

// StartComponentHealthServer runs a minimal HTTP server with /healthz and /readyz.
var StartComponentHealthServer = internal.StartComponentHealthServer
