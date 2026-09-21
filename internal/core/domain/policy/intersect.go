package policy

// Intersect returns the documents that survive a permission boundary.
//
// A boundary only ever narrows, so this is a filter, never a grant: an Allow
// statement survives only where the boundary also allows it, while every Deny
// in the boundary is carried over verbatim because a deny is already the
// narrowest possible statement.
//
// Statements are split into single-action, single-resource atoms before the
// boundary is consulted. Deciding whole statements would drop a statement that
// is permitted in part, which would silently remove access the caller holds.
func Intersect(docs []Document, boundary Document) []Document {
	boundaryDocs := []Document{boundary}

	var out []Document
	for _, doc := range docs {
		var kept []Statement
		for _, st := range doc.Statement {
			if st.Effect == EffectDeny {
				kept = append(kept, st)
				continue
			}
			for _, action := range st.Action {
				for _, resource := range st.Resource {
					// Evaluated rather than compared, so a boundary that
					// allows job:* but denies job:Delete does not let
					// job:Delete through into the surviving grants.
					target, err := concreteTarget(resource)
					if err != nil {
						continue
					}
					if Evaluate(boundaryDocs, Request{Action: action, Resource: target}) != DecisionAllow {
						continue
					}
					kept = append(kept, Statement{
						Effect:   EffectAllow,
						Action:   []string{action},
						Resource: []string{resource},
					})
				}
			}
		}
		if len(kept) > 0 {
			out = append(out, Document{Version: doc.Version, Statement: kept})
		}
	}

	var denies []Statement
	for _, st := range boundary.Statement {
		if st.Effect == EffectDeny {
			denies = append(denies, st)
		}
	}
	if len(denies) > 0 {
		out = append(out, Document{Version: boundary.Version, Statement: denies})
	}
	return out
}
