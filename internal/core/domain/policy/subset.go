package policy

import "sort"

// The proof is exact within these limits. Exhaustion refuses delegation;
// it never changes a universal authorization question into a sampled one.
const maxPolicySetWork = 250000
const maxPolicyAtoms = 4096

type policyAtom struct {
	effect Effect
	parts  [5]string // action, tenant, group, type, id; "*" is the whole segment
}

func policyAtoms(docs []Document) ([]policyAtom, bool) {
	budget := maxPolicySetWork
	return effectivePolicyAtoms(docs, &budget, 0, false)
}

// Materialize internal intersections before the exact symbolic inclusion proof.
// Every nested level shares one work budget; exhaustion refuses delegation.
func effectivePolicyAtoms(docs []Document, budget *int, depth int, ignoreMalformed bool) ([]policyAtom, bool) {
	if depth > 32 {
		return nil, false
	}
	var out []policyAtom
	for _, doc := range docs {
		*budget--
		if *budget < 0 {
			return nil, false
		}
		if doc.boundary != nil {
			grants, ok := effectivePolicyAtoms(doc.grants, budget, depth+1, ignoreMalformed)
			if !ok {
				return nil, false
			}
			ceiling, ok := effectivePolicyAtoms([]Document{*doc.boundary}, budget, depth+1, ignoreMalformed)
			if !ok {
				return nil, false
			}
			atoms, ok := intersectPolicyAtoms(grants, ceiling, budget)
			if !ok || len(out)+len(atoms) > maxPolicyAtoms {
				return nil, false
			}
			out = append(out, atoms...)
		}
		for _, st := range doc.Statement {
			if st.Effect != EffectAllow && st.Effect != EffectDeny {
				return nil, false
			}
			for _, pattern := range st.Resource {
				arn, err := ParseARN(pattern)
				if err != nil {
					if ignoreMalformed {
						continue
					}
					return nil, false
				}
				if arn.Group == "" {
					arn.Group = "*"
				}
				for _, action := range st.Action {
					*budget--
					if *budget < 0 || len(out) == maxPolicyAtoms {
						return nil, false
					}
					out = append(out, policyAtom{effect: st.Effect, parts: [5]string{action, arn.Tenant, arn.Group, arn.Type, arn.ID}})
				}
			}
		}
	}
	return out, true
}

// symbolicValue is either one named equality class or the complement of every
// named literal in that dimension. The complement is a set, never a fabricated
// identifier: exact predicates cannot match it, while a wildcard can.
type symbolicValue struct {
	literal string
	other   bool
}

func symbolicAllowed(atoms []policyAtom, point [5]symbolicValue, budget *int) (bool, bool) {
	allowed := false
	for _, atom := range atoms {
		*budget -= 1
		if *budget < 0 {
			return false, false
		}
		matches := true
		for i, pattern := range atom.parts {
			if pattern != "*" && (point[i].other || pattern != point[i].literal) {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		if atom.effect == EffectDeny {
			return false, true
		}
		allowed = true
	}
	return allowed, true
}

// Permits proves effective(subset) is contained in effective(superset), including
// explicit denies on both sides. Literals partition each segment into singleton
// classes and one complement. Every policy predicate is constant on each product
// cell, so checking every relevant cell proves inclusion over the whole universe.
func Permits(subset, superset []Document) bool {
	child, ok := policyAtoms(subset)
	if !ok {
		return false
	}
	parent, ok := policyAtoms(superset)
	if !ok {
		return false
	}
	var literals [5]map[string]bool
	for i := range literals {
		literals[i] = make(map[string]bool)
	}
	for _, atoms := range [][]policyAtom{child, parent} {
		for _, atom := range atoms {
			for i, value := range atom.parts {
				if value != "*" {
					literals[i][value] = true
				}
			}
		}
	}
	var classes [5][]symbolicValue
	for i, set := range literals {
		keys := make([]string, 0, len(set))
		for key := range set {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			classes[i] = append(classes[i], symbolicValue{literal: key})
		}
		classes[i] = append(classes[i], symbolicValue{other: true})
	}
	budget := maxPolicySetWork
	for _, seed := range child {
		if seed.effect != EffectAllow {
			continue
		}
		var point [5]symbolicValue
		var covered func(int) bool
		covered = func(dimension int) bool {
			budget--
			if budget < 0 {
				return false
			}
			if dimension == len(point) {
				allowed, proved := symbolicAllowed(child, point, &budget)
				if !proved {
					return false
				}
				if !allowed {
					return true
				}
				allowed, proved = symbolicAllowed(parent, point, &budget)
				return proved && allowed
			}
			if seed.parts[dimension] != "*" {
				point[dimension] = symbolicValue{literal: seed.parts[dimension]}
				return covered(dimension + 1)
			}
			for _, class := range classes[dimension] {
				point[dimension] = class
				if !covered(dimension + 1) {
					return false
				}
			}
			return true
		}
		if !covered(0) {
			return false
		}
	}
	return true
}
