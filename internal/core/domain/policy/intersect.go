package policy

// Intersect retains the boundary as an internal expression. Denies remain
// global across nested constraints, and serialization cannot discard a boundary
// while retaining wider grants. Bound the same expansion used for delegation.
func Intersect(docs []Document, boundary Document) []Document {
	effective := []Document{{Version: boundary.Version, grants: append([]Document(nil), docs...), boundary: &boundary}}
	budget := maxPolicySetWork
	// Evaluation ignores malformed resource patterns; persisted documents have
	// already passed ParseDocument. Keep that behavior for internal expressions.
	if _, ok := effectivePolicyAtoms(effective, &budget, 0, true); !ok {
		return nil
	}
	return effective
}

func intersectPolicyAtoms(grants, ceiling []policyAtom, budget *int) ([]policyAtom, bool) {
	var out []policyAtom
	appendAtom := func(atom policyAtom) bool {
		if len(out) >= maxPolicyAtoms {
			return false
		}
		out = append(out, atom)
		return true
	}
	for _, a := range grants {
		if a.effect == EffectDeny {
			if !appendAtom(a) {
				return nil, false
			}
			continue
		}
		for _, b := range ceiling {
			*budget--
			if *budget < 0 {
				return nil, false
			}
			if b.effect != EffectAllow {
				continue
			}
			if atom, overlap := intersectAtoms(a, b); overlap {
				if !appendAtom(atom) {
					return nil, false
				}
			}
		}
	}
	for _, atom := range ceiling {
		if atom.effect == EffectDeny {
			if !appendAtom(atom) {
				return nil, false
			}
		}
	}
	return out, true
}

func intersectAtoms(a, b policyAtom) (policyAtom, bool) {
	out := a
	for i, v := range a.parts {
		switch {
		case v == b.parts[i], b.parts[i] == "*":
		case v == "*":
			out.parts[i] = b.parts[i]
		default:
			return policyAtom{}, false
		}
	}
	return out, true
}
