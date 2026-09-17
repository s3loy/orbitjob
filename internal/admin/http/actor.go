package http

import (
	"github.com/gin-gonic/gin"

	"orbitjob/internal/admin/http/middleware"
)

// actorID identifies who performed an action, for the audit trail and the run
// ledger's actor column. It is always the authenticated key's id: a
// client-supplied actor header is unverified, and a trail naming whoever the
// caller claimed to be answers the wrong question.
func actorID(c *gin.Context) string {
	p, ok := middleware.PrincipalFrom(c)
	if !ok {
		return ""
	}
	return p.KeyID
}
