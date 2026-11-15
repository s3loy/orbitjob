package http

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin/binding"

	checkdomain "orbitjob/internal/core/domain/check"
)

func TestOpenAPISchemaModesDoNotLeakValidation(t *testing.T) {
	for _, first := range []schemaMode{schemaModeRequest, schemaModeResponse} {
		t.Run(string(first), func(t *testing.T) {
			registry := newSchemaRegistry()
			registry.schemaForModel(checkdomain.AssertionRule{}, first)
			request := registry.schemaForModel(checkdomain.AssertionRule{}, schemaModeRequest)
			response := registry.schemaForModel(checkdomain.AssertionRule{}, schemaModeResponse)
			if request.Ref == response.Ref {
				t.Fatal("request and response validation share a component")
			}
			input := registry.components[strings.TrimPrefix(request.Ref, "#/components/schemas/")]
			output := registry.components[strings.TrimPrefix(response.Ref, "#/components/schemas/")]
			if len(input.Properties["operator"].Enum) == 0 || len(output.Properties["operator"].Enum) != 0 {
				t.Fatal("request-only normalized operator validation leaked into stored output")
			}
			if containsString(input.Required, "threshold") || !containsString(output.Required, "threshold") {
				t.Fatal("omittable input zero and always-serialized output zero were conflated")
			}
			encoded, err := json.Marshal(checkdomain.AssertionRule{})
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]any
			if err := json.Unmarshal(encoded, &wire); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"metric", "operator", "severity", "threshold"} {
				if _, emitted := wire[field]; !emitted || !containsString(output.Required, field) {
					t.Errorf("response %s requiredness disagrees with encoding/json", field)
				}
			}
		})
	}
}

func TestOpenAPIDefaultsRemainOmittableInputs(t *testing.T) {
	for _, tc := range []struct {
		model  any
		input  string
		fields []string
	}{
		{CreateCheckRequest{}, `{"name":"Probe","check_type":"http_health","check_config":{"url":"https://example.com/health"},"schedule_type":"interval","interval_sec":60}`, []string{"priority", "timezone", "timeout_sec", "retry_limit"}},
		{CreateSLORequest{}, `{"name":"Availability","sli_id":1,"target":0.99,"window_duration":"24h"}`, []string{"window_type", "alert_fast_burn_rate", "alert_slow_burn_rate"}},
		{CreateSLIRequest{}, `{"name":"Availability","sli_type":"availability","source_config":{"source_uid":"check-1"},"good_event_criteria":{"status":"success"}}`, []string{"source_type", "aggregation"}},
	} {
		t.Run(reflect.TypeOf(tc.model).Name(), func(t *testing.T) {
			value := reflect.New(reflect.TypeOf(tc.model)).Interface()
			if err := json.Unmarshal([]byte(tc.input), value); err != nil {
				t.Fatal(err)
			}
			if err := binding.Validator.ValidateStruct(value); err != nil {
				t.Fatalf("omitted defaults rejected by actual binding: %v", err)
			}
			registry := newSchemaRegistry()
			reference := registry.schemaForModel(tc.model, schemaModeRequest)
			schema := registry.components[strings.TrimPrefix(reference.Ref, "#/components/schemas/")]
			for _, field := range tc.fields {
				if containsString(schema.Required, field) || schema.Properties[field].Default == nil {
					t.Errorf("%s default must stay documented and omittable", field)
				}
			}
		})
	}
}
