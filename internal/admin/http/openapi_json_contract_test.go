package http

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenAPIMatchesJSONSerialization(t *testing.T) {
	doc := ServiceOpenAPIDocument()
	for name, model := range map[string]any{"WorkflowGetItem": WorkflowGetItem{}, "WorkflowRunGetItem": WorkflowRunGetItem{}} {
		raw, err := json.Marshal(model)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		for key := range fields {
			if _, ok := doc.Components.Schemas[name].Properties[key]; !ok {
				t.Errorf("%s misses serialized field %s", name, key)
			}
		}
	}
	document := doc.Components.Schemas["CreatePolicyRequest"].Properties["document"]
	if document.Ref != "" {
		document = doc.Components.Schemas[strings.TrimPrefix(document.Ref, "#/components/schemas/")]
	}
	if document.Type != "object" {
		t.Errorf("policy document schema is %s, want JSON object", document.Type)
	}
	target := doc.Components.Schemas["CreateSLORequest"].Properties["target"]
	if target.Minimum == nil || *target.Minimum != 0.0001 {
		t.Errorf("target minimum = %v", target.Minimum)
	}
}
