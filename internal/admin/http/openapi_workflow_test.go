package http

import (
	"reflect"
	"testing"
)

func TestFunctionRunParameterDeclaresUUID(t *testing.T) {
	for _, parameter := range parametersFromModel(GetFunctionRunRequest{}) {
		if parameter.Name == "run_id" {
			if parameter.Schema.Format != "uuid" {
				t.Fatalf("run_id format = %q, want uuid", parameter.Schema.Format)
			}
			return
		}
	}
	t.Fatal("run_id path parameter missing")
}

func TestWorkflowDetailSchemasIncludePromotedIdentity(t *testing.T) {
	for _, tc := range []struct {
		model  any
		fields []string
	}{
		{WorkflowGetItem{}, []string{"id", "name", "source_uid", "generation", "tasks"}},
		{WorkflowRunGetItem{}, []string{"id", "revision_id", "phase", "actor", "steps"}},
	} {
		schema := newSchemaRegistry().structSchema(reflect.TypeOf(tc.model), schemaModeResponse)
		for _, name := range tc.fields {
			if _, ok := schema.Properties[name]; !ok {
				t.Fatalf("%T missing promoted property %q", tc.model, name)
			}
			found := false
			for _, required := range schema.Required {
				found = found || required == name
			}
			if !found {
				t.Fatalf("%T missing required property %q", tc.model, name)
			}
		}
	}
}
