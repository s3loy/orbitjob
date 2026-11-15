package http

import (
	"context"
	"errors"
	"fmt"
	"sort"

	tenantquery "orbitjob/internal/admin/app/tenant/query"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/domain/resource"
)

// SetWorkloadNamespaceTenants uses the same server-owned mapping supplied to
// definition writes. It describes configuration, not cluster readiness or a
// grant to apply resources. Copy it so later caller mutations cannot widen it.
func (h *Handler) SetWorkloadNamespaceTenants(mapping map[string]string) {
	h.workloadNamespaceTenants = make(map[string]string, len(mapping))
	for namespace, tenantID := range mapping {
		h.workloadNamespaceTenants[namespace] = tenantID
	}
}

func (h *Handler) identityMetadata(ctx context.Context, p middleware.Principal, docs []policy.Document, tenantID string, out *IdentityResponse) error {
	// A tenant learns its own labels without obtaining tenant management
	// privileges. Platform selectors alone never authorize tenant disclosure.
	if p.IsPlatform() && policy.Evaluate(docs, policy.Request{
		Action: "tenant:Get", Resource: policy.ARN{Tenant: tenantID, Group: "*", Type: "tenant", ID: tenantID},
	}) != policy.DecisionAllow {
		return nil
	}
	if h.getTenantUC != nil {
		t, err := h.getTenantUC.Get(ctx, tenantquery.GetInput{TenantID: tenantID, ID: tenantID})
		if err != nil && !metadataMissing(err) {
			return fmt.Errorf("read identity workspace metadata: %w", err)
		}
		if err == nil {
			out.TenantName, out.TenantSlug = t.Name, t.Slug
		}
	}
	if !p.IsHuman() && !p.IsPlatform() && h.apiKeyMetadataReader != nil {
		meta, err := h.apiKeyMetadataReader.Metadata(ctx, p.TenantID, p.KeyID)
		if err != nil && !metadataMissing(err) {
			return fmt.Errorf("read identity credential metadata: %w", err)
		}
		if err == nil {
			out.KeyName = meta.Name
		}
	}
	for namespace, owner := range h.workloadNamespaceTenants {
		if owner == tenantID {
			out.WorkloadNamespaces = append(out.WorkloadNamespaces, namespace)
		}
	}
	sort.Strings(out.WorkloadNamespaces)
	return nil
}

func metadataMissing(err error) bool {
	var missing *resource.NotFoundError
	return errors.As(err, &missing)
}
