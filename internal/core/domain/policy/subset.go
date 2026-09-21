package policy

// probeSegment is a stand-in for a wildcard segment when a pattern is evaluated
// as a concrete request. It only has to be a value no real identifier takes.
const probeSegment = "probe00000000000000000000"

// Permits reports whether every Allow statement in subset is already effective
// for superset.
//
// "Effective" is the operative word, and it is why this evaluates rather than
// compares: a superset that allows job:* but denies job:Delete does not hold
// job:Delete, so it must not be able to hand it out. Comparing Allow lists
// alone would miss exactly that case, turning a delegation check into a rubber
// stamp for permissions the granter has been explicitly denied.
func Permits(subset, superset []Document) bool {
	for _, doc := range subset {
		for _, st := range doc.Statement {
			if st.Effect != EffectAllow {
				// A deny only removes permission, so it never needs covering.
				continue
			}
			if !everyAtomEffective(st, superset) {
				return false
			}
		}
	}
	return true
}

// everyAtomEffective splits a statement into single (action, resource) pairs and
// requires each to be permitted by superset.
func everyAtomEffective(st Statement, superset []Document) bool {
	for _, action := range st.Action {
		if !actionEffective(action, st.Resource, superset) {
			return false
		}
	}
	return true
}

func actionEffective(action string, patterns []string, superset []Document) bool {
	for _, pattern := range patterns {
		target, err := concreteTarget(pattern)
		if err != nil {
			// A pattern that cannot be read cannot be shown to be permitted.
			return false
		}
		if Evaluate(superset, Request{Action: action, Resource: target}) != DecisionAllow {
			return false
		}
	}
	return true
}

// concreteTarget turns a resource pattern into a request that a policy can be
// evaluated against, substituting the probe for every wildcard segment.
func concreteTarget(pattern string) (ARN, error) {
	arn, err := ParseARN(pattern)
	if err != nil {
		return ARN{}, err
	}
	if arn.Tenant == "*" {
		arn.Tenant = probeSegment
	}
	if arn.Group == "" || arn.Group == "*" {
		arn.Group = probeSegment
	}
	if arn.Type == "*" {
		arn.Type = probeSegment
	}
	if arn.ID == "*" {
		arn.ID = probeSegment
	}
	return arn, nil
}
