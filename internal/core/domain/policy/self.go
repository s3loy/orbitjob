package policy

import "strings"

// SelfToken is the placeholder a platform preset uses in place of a tenant id,
// so one preset row serves every tenant without being copied per tenant.
const SelfToken = "self"

// ResolveSelf returns a copy of doc with the self token replaced by tenantID.
//
// The copy is deliberate: presets are shared rows read concurrently by every
// tenant, so resolving in place would race and would corrupt the preset for
// whoever reads it next.
func ResolveSelf(doc Document, tenantID string) Document {
	out := Document{Version: doc.Version, Statement: make([]Statement, 0, len(doc.Statement))}
	for _, st := range doc.Statement {
		next := Statement{
			Effect:   st.Effect,
			Action:   st.Action,
			Resource: make([]string, 0, len(st.Resource)),
		}
		for _, pattern := range st.Resource {
			segments := strings.Split(pattern, ":")
			// Only the tenant segment is ever self, and only when the pattern
			// has the full four segments; anything else is left untouched.
			if len(segments) == 4 && segments[1] == SelfToken {
				segments[1] = tenantID
			}
			next.Resource = append(next.Resource, strings.Join(segments, ":"))
		}
		out.Statement = append(out.Statement, next)
	}
	return out
}
