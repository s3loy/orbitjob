// Package executionpolicy validates explicit execution resource and retry policy.
package executionpolicy

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/resource"
)

const Version1 int32 = 1

// Resources contains only resource dimensions supported by the workload API.
// Quantities remain strings at the wire boundary and are parsed without floats.
type Resources struct {
	Requests map[string]string `json:"requests,omitempty"`
	Limits   map[string]string `json:"limits,omitempty"`
}

// Profile is an installation boundary, not a measurement of node capacity.
// Callers must pin the normalized values, rather than reread defaults per run.
type Profile struct {
	Defaults Resources
	Maximum  map[string]string
}

func StandardProfile() Profile {
	return Profile{
		Defaults: Resources{
			Requests: map[string]string{"cpu": "100m", "memory": "128Mi", "ephemeral-storage": "100Mi"},
			Limits:   map[string]string{"cpu": "1", "memory": "256Mi", "ephemeral-storage": "1Gi"},
		},
		Maximum: map[string]string{"cpu": "16", "memory": "64Gi", "ephemeral-storage": "100Gi"},
	}
}

func Normalize(version int32, retrySafe bool, backoff, maxAttempts int32, requested *Resources) (*Resources, error) {
	return NormalizeWithProfile(version, retrySafe, backoff, maxAttempts, requested, StandardProfile())
}

// ValidateResolved validates a previously accepted v1 revision without adding
// defaults or applying today's installation ceilings to its pinned resources.
func ValidateResolved(version int32, retrySafe bool, backoff, maxAttempts int32, pinned *Resources) error {
	if version != Version1 || maxAttempts < 1 || pinned == nil || len(pinned.Requests) != 3 || len(pinned.Limits) != 3 {
		return fmt.Errorf("a resolved version 1 execution policy is required")
	}
	for _, name := range []string{"cpu", "memory", "ephemeral-storage"} {
		if pinned.Requests[name] == "" || pinned.Limits[name] == "" {
			return fmt.Errorf("pinned resources must contain requests and limits for %s", name)
		}
	}
	_, err := NormalizeWithProfile(version, retrySafe, backoff, maxAttempts, pinned, Profile{Maximum: pinned.Limits})
	return err
}

func NormalizeWithProfile(version int32, retrySafe bool, backoff, maxAttempts int32, requested *Resources, profile Profile) (*Resources, error) {
	if version != 0 && version != Version1 {
		return nil, fmt.Errorf("execution policy version must be 0 or 1")
	}
	if backoff < 0 || maxAttempts < 0 {
		return nil, fmt.Errorf("retry limits must be nonnegative")
	}
	if version == Version1 && (backoff > 0 || maxAttempts > 1) && !retrySafe {
		return nil, fmt.Errorf("retrySafe is required before enabling repeated execution")
	}
	if requested == nil && version == 0 {
		return nil, nil
	}
	out := &Resources{}
	if version == Version1 {
		out.Requests = clone(profile.Defaults.Requests)
		out.Limits = clone(profile.Defaults.Limits)
	}
	if requested != nil {
		out.Requests = overlay(out.Requests, requested.Requests)
		out.Limits = overlay(out.Limits, requested.Limits)
	}
	for group, values := range map[string]map[string]string{"requests": out.Requests, "limits": out.Limits} {
		for name, text := range values {
			if len(text) == 0 || len(text) > 64 {
				return nil, fmt.Errorf("resources.%s.%s quantity must contain 1 to 64 characters", group, name)
			}
			maximum, supported := profile.Maximum[name]
			if !supported || (name != "cpu" && name != "memory" && name != "ephemeral-storage") {
				return nil, fmt.Errorf("resources.%s.%s is not supported", group, name)
			}
			q, err := resource.ParseQuantity(text)
			if err != nil || q.Sign() <= 0 {
				return nil, fmt.Errorf("resources.%s.%s must be a positive Kubernetes quantity", group, name)
			}
			ceiling, err := resource.ParseQuantity(maximum)
			if err != nil || ceiling.Sign() <= 0 {
				return nil, fmt.Errorf("installation maximum for %s is invalid", name)
			}
			if q.Cmp(ceiling) > 0 {
				return nil, fmt.Errorf("resources.%s.%s exceeds installation maximum %s", group, name, maximum)
			}
			if name == "cpu" {
				if q.Cmp(*resource.NewMilliQuantity(q.MilliValue(), resource.DecimalSI)) != 0 {
					return nil, fmt.Errorf("resources.%s.cpu must use at most millicore precision", group)
				}
			} else if q.Cmp(*resource.NewQuantity(q.Value(), resource.DecimalSI)) != 0 {
				return nil, fmt.Errorf("resources.%s.%s must contain whole bytes", group, name)
			}
			values[name] = q.String()
		}
	}
	for _, name := range []string{"cpu", "memory", "ephemeral-storage"} {
		request, hasRequest := out.Requests[name]
		limit, hasLimit := out.Limits[name]
		if version == Version1 && (!hasRequest || !hasLimit) {
			return nil, fmt.Errorf("installation profile must resolve requests and limits for %s", name)
		}
		if hasRequest && hasLimit {
			rq, _ := resource.ParseQuantity(request)
			lq, _ := resource.ParseQuantity(limit)
			if rq.Cmp(lq) > 0 {
				return nil, fmt.Errorf("resources.requests.%s exceeds its limit", name)
			}
		}
	}
	return out, nil
}

func clone(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	out := make(map[string]string, len(values))
	for name, value := range values {
		out[name] = value
	}
	return out
}

func overlay(base, values map[string]string) map[string]string {
	if values == nil {
		return base
	}
	if base == nil {
		base = make(map[string]string, len(values))
	}
	for name, value := range values {
		base[name] = value
	}
	return base
}
