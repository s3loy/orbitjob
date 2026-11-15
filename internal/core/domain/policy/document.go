package policy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	// Effective documents are internal expressions, never persisted policies.
	grants   []Document
	boundary *Document
}

// ParseDocument decodes a stored policy body and rejects the shapes that would
// make evaluation ambiguous. An unknown effect is an error rather than a
// default, because defaulting it could turn a typo into a silent grant.
func ParseDocument(raw []byte) (Document, error) {
	if err := checkDocumentFields(raw); err != nil {
		return Document{}, fmt.Errorf("decode policy document: %w", err)
	}
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

// Never ignore authorization semantics the evaluator does not implement.
// DisallowUnknownFields alone still accepts case-insensitive struct keys and
// duplicate members, which can make a UI's displayed grant differ from Go's.
func checkDocumentFields(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	var walk func(string, int) error
	walk = func(path string, depth int) error {
		if depth > 8 {
			return fmt.Errorf("policy document nesting is invalid")
		}
		token, err := d.Token()
		if err != nil {
			return err
		}
		switch token {
		case json.Delim('{'):
			var allowed map[string]bool
			switch path {
			case "$":
				allowed = map[string]bool{"version": true, "statement": true}
			case "$.statement[]":
				allowed = map[string]bool{"effect": true, "action": true, "resource": true}
			default:
				return fmt.Errorf("unsupported policy object at %s", path)
			}
			seen := map[string]bool{}
			for d.More() {
				keyToken, err := d.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok || !allowed[key] {
					return fmt.Errorf("unsupported policy field %s.%v; only the documented lowercase policy fields are supported", path, keyToken)
				}
				if seen[key] {
					return fmt.Errorf("duplicate policy field %s.%s", path, key)
				}
				seen[key] = true
				if err := walk(path+"."+key, depth+1); err != nil {
					return err
				}
			}
			_, err = d.Token()
			return err
		case json.Delim('['):
			for d.More() {
				if err := walk(path+"[]", depth+1); err != nil {
					return err
				}
			}
			_, err = d.Token()
			return err
		default:
			return nil
		}
	}
	if err := walk("$", 0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("exactly one policy document is required")
	}
	return nil
}
