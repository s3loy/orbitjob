package sli

import (
	"math"
	"testing"
)

func TestCreateRejectsCriteriaWithNoOutcomeProducer(t *testing.T) {
	for _, tt := range []struct {
		name, kind string
		criteria   map[string]any
	}{
		{"latency missing", TypeLatency, nil},
		{"latency status only", TypeLatency, map[string]any{"status": "success"}},
		{"unsupported field", TypeQuality, map[string]any{"severity": "ok"}},
		{"unsupported operator", TypeLatency, map[string]any{"duration_ms": map[string]any{"op": "approximately", "value": 1}}},
		{"nonfinite", TypeLatency, map[string]any{"duration_ms": math.Inf(1)}},
		{"bad status", TypeAvailability, map[string]any{"status": "maybe"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeCreate(CreateInput{Name: "availability", SLIType: tt.kind, SourceConfig: map[string]any{"source_uid": "check-42"}, GoodEventCriteria: tt.criteria})
			if err == nil {
				t.Fatal("unusable criteria accepted")
			}
		})
	}
}
