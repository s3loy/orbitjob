package http

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	apikeycommand "orbitjob/internal/admin/app/apikey/command"
	"orbitjob/internal/admin/http/apperror"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/domain/apikey"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/domain/resource"
)

type apiKeyMetadataReader interface {
	Metadata(context.Context, string, string) (apikey.Metadata, error)
}

func (h *Handler) SetAPIKeyMetadataReader(r apiKeyMetadataReader) { h.apiKeyMetadataReader = r }
func (h *Handler) SetRenameAPIKeyUseCase(uc renameAPIKeyUseCase)  { h.renameAPIKeyUC = uc }

// updateKeyResource first resolves the owner without exposing it, then reads
// scope inside that tenant. Group boundaries apply even to wildcard documents.
func (h *Handler) updateKeyResource(c *gin.Context) (policy.ARN, error) {
	arn, err := h.revokeKeyResource(c)
	if err != nil {
		return policy.ARN{}, err
	}
	if h.apiKeyMetadataReader == nil {
		return policy.ARN{}, middleware.ErrAuthorizationLookup
	}
	meta, err := h.apiKeyMetadataReader.Metadata(c.Request.Context(), arn.Tenant, arn.ID)
	if err != nil {
		var missing *resource.NotFoundError
		if errors.As(err, &missing) {
			return policy.ARN{}, middleware.ErrResourceUnavailable
		}
		return policy.ARN{}, middleware.ErrAuthorizationLookup
	}
	p, _ := middleware.PrincipalFrom(c)
	if p.ResourceGroupID != "" && p.ResourceGroupID != meta.ResourceGroupID {
		return policy.ARN{}, middleware.ErrResourceUnavailable
	}
	if meta.ResourceGroupID != "" {
		arn.Group = meta.ResourceGroupID
	}
	return arn, nil
}

// RenameAPIKey changes only a credential's display name. Its grants, secret
// and lifecycle remain outside this operation's writable model.
func (h *Handler) RenameAPIKey(c *gin.Context) {
	var path APIKeyURI
	if err := c.ShouldBindUri(&path); err != nil {
		apperror.Write(c, http.StatusBadRequest, toBindAPIError(err))
		return
	}
	var req RenameAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apperror.Write(c, http.StatusBadRequest, toBindAPIError(err))
		return
	}
	arn, err := h.updateKeyResource(c)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	out, err := h.renameAPIKeyUC.Rename(c.Request.Context(), apikeycommand.RenameInput{
		TenantID: arn.Tenant, ID: path.ID, Name: *req.Name, ActorID: actorID(c),
	})
	if err != nil {
		writeAPIError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}
