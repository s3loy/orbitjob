package http

import (
	"strings"

	"github.com/gin-gonic/gin"

	"orbitjob/internal/domain/validation"
)

const actorIDHeader = "X-Actor-ID"

func requiredActorID(c *gin.Context) (string, error) {
	actorID := strings.TrimSpace(c.GetHeader(actorIDHeader))
	if actorID == "" {
		return "", validation.New("actor_id", "is required")
	}
	if len(actorID) > 128 {
		return "", validation.New("actor_id", "must be <= 128 characters")
	}

	return actorID, nil
}
