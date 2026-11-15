package operator

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func namespacedObject(namespace string) unstructured.Unstructured {
	return unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "workloads.orbitjob.io/v1alpha1",
		"kind":       "ScheduledJob",
		"metadata":   map[string]any{"name": "nightly", "namespace": namespace},
	}}
}

func TestNamespaceTenantResolverMapsConfiguredNamespaces(t *testing.T) {
	resolver := NamespaceTenantResolver{Tenants: map[string]string{"finance": executionTenant}}
	tenant, err := resolver.Resolve(namespacedObject("finance"))
	if err != nil {
		t.Fatal(err)
	}
	if tenant != executionTenant {
		t.Fatalf("tenant = %q", tenant)
	}

	// An unmapped namespace must fail loudly: defaulting it would schedule a
	// tenant's work under another tenant's identity.
	if _, err := resolver.Resolve(namespacedObject("unknown")); err == nil {
		t.Fatal("expected an unmapped namespace to be rejected")
	}
}

func TestNamespaceTenantResolverTenantIDs(t *testing.T) {
	resolver := NamespaceTenantResolver{Tenants: map[string]string{
		"finance": executionTenantB, "platform": executionTenant, "ops": executionTenant,
	}}
	got := resolver.TenantIDs()
	if len(got) != 2 || (got[0] != executionTenant && got[1] != executionTenant) || (got[0] != executionTenantB && got[1] != executionTenantB) || got[0] >= got[1] {
		t.Fatalf("tenant ids = %v, want deduplicated and sorted", got)
	}
	if ids := (NamespaceTenantResolver{}).TenantIDs(); len(ids) != 0 {
		t.Fatalf("empty resolver returned %v", ids)
	}
}

func TestDefaultTenantResolver(t *testing.T) {
	resolver := DefaultTenantResolver{Tenant: executionTenant}
	tenant, err := resolver.Resolve(namespacedObject("anything"))
	if err != nil {
		t.Fatal(err)
	}
	if tenant != executionTenant {
		t.Fatalf("tenant = %q", tenant)
	}
	if ids := resolver.TenantIDs(); len(ids) != 1 || ids[0] != executionTenant {
		t.Fatalf("tenant ids = %v", ids)
	}

	// A missing tenant is a configuration error, not a wildcard.
	if _, err := (DefaultTenantResolver{}).Resolve(namespacedObject("anything")); err == nil {
		t.Fatal("expected an unconfigured tenant to be rejected")
	}
	if ids := (DefaultTenantResolver{}).TenantIDs(); ids != nil {
		t.Fatalf("unconfigured resolver returned %v", ids)
	}
	// A cluster-scoped object has no namespace to bind to.
	if _, err := resolver.Resolve(unstructured.Unstructured{}); err == nil {
		t.Fatal("expected a namespaceless object to be rejected")
	}
}

func TestNoTenantMappingLeaksAcrossNamespaces(t *testing.T) {
	// The point of the mapping is isolation: two namespaces must not collapse
	// onto one tenant unless the operator explicitly configured it.
	resolver := NamespaceTenantResolver{Tenants: map[string]string{
		"team-a": executionTenant, "team-b": executionTenantB,
	}}
	first, err := resolver.Resolve(namespacedObject("team-a"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolver.Resolve(namespacedObject("team-b"))
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("two namespaces resolved to the same tenant %q", first)
	}
	if strings.TrimSpace(first) == "" || strings.TrimSpace(second) == "" {
		t.Fatal("resolved tenants must be non-empty")
	}
}
