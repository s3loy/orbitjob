package execution

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"orbitjob/internal/domain/executionpolicy"
)

// ResourceRequirements converts a previously pinned resource declaration. It
// never introduces per-run defaults and rejects unsupported dimensions.
func ResourceRequirements(requested *executionpolicy.Resources) (corev1.ResourceRequirements, error) {
	if requested == nil {
		return corev1.ResourceRequirements{}, nil
	}
	requests, err := resourceList(requested.Requests)
	if err != nil {
		return corev1.ResourceRequirements{}, err
	}
	limits, err := resourceList(requested.Limits)
	if err != nil {
		return corev1.ResourceRequirements{}, err
	}
	for name, request := range requests {
		if limit, ok := limits[name]; ok && request.Cmp(limit) > 0 {
			return corev1.ResourceRequirements{}, fmt.Errorf("pinned request exceeds limit for %s", name)
		}
	}
	return corev1.ResourceRequirements{Requests: requests, Limits: limits}, nil
}

func resourceList(values map[string]string) (corev1.ResourceList, error) {
	if values == nil {
		return nil, nil
	}
	out := make(corev1.ResourceList, len(values))
	for name, text := range values {
		if name != "cpu" && name != "memory" && name != "ephemeral-storage" {
			return nil, fmt.Errorf("unsupported pinned resource %s", name)
		}
		q, err := resource.ParseQuantity(text)
		if err != nil || q.Sign() <= 0 {
			return nil, fmt.Errorf("invalid pinned resource %s", name)
		}
		out[corev1.ResourceName(name)] = q
	}
	return out, nil
}
