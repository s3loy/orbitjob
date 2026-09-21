package policy

import "slices"

// Request is one authorization question.
type Request struct {
	Action   string
	Resource ARN
}

// WildcardAction is the one wildcard the engine understands. It is reserved for
// the platform administrator preset, which is not editable through the API.
// Resource-scoped wildcards such as "job:*" are deliberately not supported:
// they would silently absorb every action added to that resource later.
const WildcardAction = "*"

// Decision is the answer.
type Decision string

const (
	DecisionAllow Decision = "Allow"
	DecisionDeny  Decision = "Deny"
)

// Evaluate answers one request against a set of documents.
//
// Every statement is examined before deciding: any Deny match rejects outright,
// otherwise the first Allow match permits, and a request matching nothing is
// denied. Actions compare as exact strings so a typo cannot silently widen a
// grant. Deny is checked across documents rather than per-document, because a
// deny in one policy must override an allow in another.
func Evaluate(docs []Document, req Request) Decision {
	allowed := false
	for _, doc := range docs {
		for _, st := range doc.Statement {
			if !st.matches(req) {
				continue
			}
			if st.Effect == EffectDeny {
				return DecisionDeny
			}
			allowed = true
		}
	}
	if allowed {
		return DecisionAllow
	}
	return DecisionDeny
}

func (st Statement) matches(req Request) bool {
	if !slices.Contains(st.Action, req.Action) && !slices.Contains(st.Action, WildcardAction) {
		return false
	}
	for _, pattern := range st.Resource {
		arn, err := ParseARN(pattern)
		if err != nil {
			// ParseDocument rejects malformed patterns on write, so an
			// unparseable one here cannot match anything.
			continue
		}
		if arn.Matches(req.Resource) {
			return true
		}
	}
	return false
}
