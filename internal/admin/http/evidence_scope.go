package http

import (
	"context"
	"strconv"

	"github.com/gin-gonic/gin"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

type evidenceScopeReader interface {
	CheckRunGroup(context.Context, string, int64) (string, error)
	SLOGroup(context.Context, string, int64) (string, error)
	AlertGroup(context.Context, string, int64) (string, error)
}

func (h *Handler) SetEvidenceScopeReader(reader evidenceScopeReader) { h.evidenceScopes = reader }

type evidenceScopeContextKey struct{}

// Preserve the authorized parent's group in the following query too. Direct
// handler tests and collection reads retain the credential's independent scope.
func evidenceQueryGroup(c *gin.Context) string {
	if group, ok := c.Request.Context().Value(evidenceScopeContextKey{}).(string); ok {
		return group
	}
	return groupFrom(c)
}

func evidenceResource(kind string) func(*Handler) middleware.ResourceResolver {
	return func(h *Handler) middleware.ResourceResolver {
		return func(c *gin.Context) (policy.ARN, error) { return h.resolveEvidenceResource(c, kind) }
	}
}

func (h *Handler) resolveEvidenceResource(c *gin.Context, kind string) (policy.ARN, error) {
	principal, ok := middleware.PrincipalFrom(c)
	if !ok {
		return policy.ARN{}, errNoPrincipal
	}
	tenant, err := requestTenant(c)
	if err != nil {
		return policy.ARN{}, err
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		return policy.ARN{}, validation.New("id", "must be >= 1")
	}
	if h == nil || h.evidenceScopes == nil {
		return policy.ARN{}, middleware.ErrAuthorizationLookup
	}
	var group string
	switch kind {
	case "checkrun":
		group, err = h.evidenceScopes.CheckRunGroup(c.Request.Context(), tenant, id)
	case "slo":
		group, err = h.evidenceScopes.SLOGroup(c.Request.Context(), tenant, id)
	case "sloalert":
		group, err = h.evidenceScopes.AlertGroup(c.Request.Context(), tenant, id)
	default:
		return policy.ARN{}, middleware.ErrAuthorizationLookup
	}
	if err != nil {
		return policy.ARN{}, err
	}
	if principal.ResourceGroupID != "" && principal.ResourceGroupID != group {
		return policy.ARN{}, &resource.NotFoundError{Resource: kind, ID: id}
	}
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), evidenceScopeContextKey{}, group))
	if group == "" {
		group = "*"
	}
	return policy.ARN{Tenant: tenant, Group: group, Type: kind, ID: strconv.FormatInt(id, 10)}, nil
}

// A collection has no single stored group or ID. Prove the caller covers the
// complete query domain, including Deny and boundaries, before any rows/counts
// are read. Budget exhaustion refuses the whole request rather than filtering
// only an already paginated result or exposing an unfiltered count.
func evidenceCollection(kind, action string) middleware.ResourceResolver {
	return func(c *gin.Context) (policy.ARN, error) {
		arn, err := tenantScoped(kind)(c)
		if err != nil {
			return policy.ARN{}, err
		}
		docs, ok := middleware.Documents(c)
		if !ok {
			return policy.ARN{}, middleware.ErrAuthorizationLookup
		}
		requested := []policy.Document{{Statement: []policy.Statement{{
			Effect: policy.EffectAllow, Action: []string{action}, Resource: []string{arn.String()},
		}}}}
		if !policy.Permits(requested, docs) {
			return policy.ARN{}, middleware.ErrResourceUnavailable
		}
		return arn, nil
	}
}
