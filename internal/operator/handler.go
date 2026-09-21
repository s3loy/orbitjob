package operator

import (
	"fmt"
	"os"
	"strings"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// NewController assembles the controller for one operator process.
func NewController(k kubernetes.Interface, d dynamic.Interface, cfg Config, handler ReconcileHandler) Controller {
	return Controller{Kubernetes: k, Dynamic: d, Config: cfg, Reconcile: handler}
}

// NamespaceTenantsTableEnv maps watched namespaces to tenant ids. Tenancy is
// configuration, not an annotation: if any user could set the tenant on a CR,
// tenant isolation would be self-service.
const NamespaceTenantsTableEnv = "OPERATOR_NAMESPACE_TENANTS"

// ParseNamespaceTenants reads the "namespace=tenant,namespace=tenant" mapping.
func ParseNamespaceTenants(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("%s is empty", NamespaceTenantsTableEnv)
	}
	out := make(map[string]string)
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		namespace, tenant, ok := strings.Cut(entry, "=")
		if !ok || strings.TrimSpace(namespace) == "" || strings.TrimSpace(tenant) == "" {
			return nil, fmt.Errorf("invalid namespace/tenant mapping %q", entry)
		}
		namespace = strings.TrimSpace(namespace)
		if _, exists := out[namespace]; exists {
			return nil, fmt.Errorf("duplicate namespace/tenant mapping for %q", namespace)
		}
		out[namespace] = strings.TrimSpace(tenant)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s contains no mappings", NamespaceTenantsTableEnv)
	}
	return out, nil
}

// EnvOr returns the environment value or a fallback.
func EnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
