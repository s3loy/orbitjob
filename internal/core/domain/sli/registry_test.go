package sli

import "testing"

// TestValidSetsCoverEveryExportedConstant guards against registry drift: a
// new type/source/aggregation constant that is not added to its Valid* set is
// rejected by NormalizeCreate, so the constant is dead weight and the API
// documents a value it cannot accept. The sets must contain exactly the
// exported constants — no more, no fewer.
func TestValidSetsCoverEveryExportedConstant(t *testing.T) {
	tests := []struct {
		name    string
		members []string
		valid   map[string]bool
	}{
		{"sli types", []string{TypeAvailability, TypeLatency, TypeQuality, TypeCustom}, ValidSLITypes},
		{"source types", []string{SourceTypeJobRun}, ValidSourceTypes},
		{"aggregations", []string{AggregationRatio, AggregationCount}, ValidAggregations},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, member := range tt.members {
				if !tt.valid[member] {
					t.Errorf("constant %q is exported but missing from its valid set", member)
				}
			}
			if len(tt.valid) != len(tt.members) {
				t.Errorf("valid set has %d entries for %d constants: %v", len(tt.valid), len(tt.members), keysOf(tt.valid))
			}
		})
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
