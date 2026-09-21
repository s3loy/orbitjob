package operator

import (
	"fmt"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sort"
)

type TenantResolver interface {
	Resolve(unstructured.Unstructured) (string, error)
}
type NamespaceTenantResolver struct{ Tenants map[string]string }

func (r NamespaceTenantResolver) Resolve(obj unstructured.Unstructured) (string, error) {
	tenant := r.Tenants[obj.GetNamespace()]
	if tenant == "" {
		return "", fmt.Errorf("tenant not configured for namespace %q", obj.GetNamespace())
	}
	return tenant, nil
}

// DefaultTenantResolver maps every namespace onto one tenant. It exists for
// single-tenant installations where a namespace table would be noise, and it
// still requires the tenant to be configured explicitly so a missing setting
// fails at startup rather than silently writing into a default tenant.
type DefaultTenantResolver struct{ Tenant string }

func (r DefaultTenantResolver) Resolve(obj unstructured.Unstructured) (string, error) {
	if r.Tenant == "" {
		return "", fmt.Errorf("default tenant is not configured")
	}
	if obj.GetNamespace() == "" {
		return "", fmt.Errorf("object has no namespace")
	}
	return r.Tenant, nil
}

// TenantIDs lists the tenants this resolver can map namespaces onto. The
// scheduler iterates this set, so a namespace without a mapping is invisible
// rather than silently scheduled into a default tenant.
func (r NamespaceTenantResolver) TenantIDs() []string {
	seen := make(map[string]struct{}, len(r.Tenants))
	out := make([]string, 0, len(r.Tenants))
	for _, tenant := range r.Tenants {
		if _, ok := seen[tenant]; ok {
			continue
		}
		seen[tenant] = struct{}{}
		out = append(out, tenant)
	}
	sort.Strings(out)
	return out
}

// Namespaces lists the exact Kubernetes namespaces this resolver accepts.
func (r NamespaceTenantResolver) Namespaces() []string {
	out := make([]string, 0, len(r.Tenants))
	for namespace := range r.Tenants {
		out = append(out, namespace)
	}
	sort.Strings(out)
	return out
}

// TenantIDs implements the scheduler's tenant listing for a single-tenant
// installation.
func (r DefaultTenantResolver) TenantIDs() []string {
	if r.Tenant == "" {
		return nil
	}
	return []string{r.Tenant}
}

// Namespaces is empty because the legacy single-tenant resolver accepts any
// namespace and therefore requires the controller's cluster-wide mode.
func (r DefaultTenantResolver) Namespaces() []string { return nil }
