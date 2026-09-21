package policy

import (
	"encoding/json"
	"fmt"
)

// Effect is the outcome a statement asks for.
type Effect string

const (
	EffectAllow Effect = "Allow"
	EffectDeny  Effect = "Deny"
)

// Statement grants or denies a set of actions on a set of resource patterns.
type Statement struct {
	Effect   Effect   `json:"effect"`
	Action   []string `json:"action"`
	Resource []string `json:"resource"`
}

// Document is one policy body, stored verbatim in policies.document.
type Document struct {
	Version   string      `json:"version"`
	Statement []Statement `json:"statement"`
}

// ParseDocument decodes a stored policy body and rejects the shapes that would
// make evaluation ambiguous. An unknown effect is an error rather than a
// default, because defaulting it could turn a typo into a silent grant.
func ParseDocument(raw []byte) (Document, error) {
	var doc Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Document{}, fmt.Errorf("decode policy document: %w", err)
	}
	if len(doc.Statement) == 0 {
		return Document{}, fmt.Errorf("policy document has no statements")
	}
	for i, st := range doc.Statement {
		if st.Effect != EffectAllow && st.Effect != EffectDeny {
			return Document{}, fmt.Errorf("statement %d has unknown effect %q", i, st.Effect)
		}
		if len(st.Action) == 0 {
			return Document{}, fmt.Errorf("statement %d has no actions", i)
		}
		if len(st.Resource) == 0 {
			return Document{}, fmt.Errorf("statement %d has no resources", i)
		}
		for _, pattern := range st.Resource {
			if _, err := ParseARN(pattern); err != nil {
				return Document{}, fmt.Errorf("statement %d: %w", i, err)
			}
		}
	}
	return doc, nil
}
