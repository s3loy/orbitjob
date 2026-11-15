package policy

import "slices"

// TenantScope describes tenant metadata reads: Include when All is false,
// otherwise every tenant except Exclude. The unused slice is empty.
type TenantScope struct {
	All     bool
	Include []string
	Exclude []string
}

// TenantReadScope is equivalent, for each tenant T, to Evaluate(docs, Request{
// Action: "tenant:Get", Resource: ARN{Tenant: T, Group: "*", Type: "tenant", ID: T}}).
// Only whole-segment wildcards exist, so each statement selects either a finite
// set of tenants or all tenants. Finite sets and their complements are closed
// under union, intersection and difference, including nested boundaries.
//
// Keep Deny separate throughout recursion: evaluateState propagates a nested
// Deny even when that wrapper has no applicable Allow. Subtracting it only at
// the end preserves its precedence over sibling documents. The caller resolves
// self before invoking this function; principal ownership/group rules are not
// policy semantics and belong to the caller.
func TenantReadScope(docs []Document) TenantScope {
	allowed, denied := tenantReadSets(docs)
	visible := allowed.intersect(denied.complement())
	values := make([]string, 0, len(visible.values))
	for value := range visible.values {
		values = append(values, value)
	}
	slices.Sort(values)
	if visible.all {
		return TenantScope{All: true, Exclude: values}
	}
	return TenantScope{Include: values}
}

// tenantSet is immutable after construction. values contains members for a
// finite set, or exclusions for its complement when all is true.
type tenantSet struct {
	all    bool
	values map[string]struct{}
}

func tenantReadSets(docs []Document) (allowed, denied tenantSet) {
	for _, doc := range docs {
		if doc.boundary != nil {
			grant, grantDeny := tenantReadSets(doc.grants)
			bound, boundaryDeny := tenantReadSets([]Document{*doc.boundary})
			allowed = allowed.union(grant.intersect(bound))
			denied = denied.union(grantDeny).union(boundaryDeny)
		}
		for _, statement := range doc.Statement {
			if !slices.Contains(statement.Action, "tenant:Get") &&
				!slices.Contains(statement.Action, WildcardAction) {
				continue
			}
			selected := tenantSet{}
			for _, resource := range statement.Resource {
				selected = selected.union(tenantResourceSet(resource))
			}
			if statement.Effect == EffectDeny {
				denied = denied.union(selected)
			} else {
				// Match evaluateState; ParseDocument validates persisted effects.
				allowed = allowed.union(selected)
			}
		}
	}
	return allowed, denied
}

func tenantResourceSet(resource string) tenantSet {
	arn, err := ParseARN(resource)
	if err != nil || !groupMatches(arn.Group, "*") || !segmentMatches(arn.Type, "tenant") {
		return tenantSet{}
	}
	// Both the tenant and ID segments must match the same actual tenant T.
	return tenantSegmentSet(arn.Tenant).intersect(tenantSegmentSet(arn.ID))
}

func tenantSegmentSet(value string) tenantSet {
	if value == "*" {
		return tenantSet{all: true}
	}
	return tenantSet{values: map[string]struct{}{value: {}}}
}

func (set tenantSet) complement() tenantSet {
	return tenantSet{all: !set.all, values: set.values}
}

func (set tenantSet) union(other tenantSet) tenantSet {
	switch {
	case set.all && other.all:
		return tenantSet{all: true, values: tenantValuesIntersection(set.values, other.values)}
	case set.all:
		return tenantSet{all: true, values: tenantValuesDifference(set.values, other.values)}
	case other.all:
		return tenantSet{all: true, values: tenantValuesDifference(other.values, set.values)}
	default:
		return tenantSet{values: tenantValuesUnion(set.values, other.values)}
	}
}

func (set tenantSet) intersect(other tenantSet) tenantSet {
	switch {
	case set.all && other.all:
		return tenantSet{all: true, values: tenantValuesUnion(set.values, other.values)}
	case set.all:
		return tenantSet{values: tenantValuesDifference(other.values, set.values)}
	case other.all:
		return tenantSet{values: tenantValuesDifference(set.values, other.values)}
	default:
		return tenantSet{values: tenantValuesIntersection(set.values, other.values)}
	}
}

func tenantValuesUnion(first, second map[string]struct{}) map[string]struct{} {
	values := make(map[string]struct{}, len(first)+len(second))
	for value := range first {
		values[value] = struct{}{}
	}
	for value := range second {
		values[value] = struct{}{}
	}
	return values
}

func tenantValuesIntersection(first, second map[string]struct{}) map[string]struct{} {
	values := make(map[string]struct{})
	for value := range first {
		if _, exists := second[value]; exists {
			values[value] = struct{}{}
		}
	}
	return values
}

func tenantValuesDifference(first, second map[string]struct{}) map[string]struct{} {
	values := make(map[string]struct{})
	for value := range first {
		if _, exists := second[value]; !exists {
			values[value] = struct{}{}
		}
	}
	return values
}
