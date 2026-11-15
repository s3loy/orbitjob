package query

import (
	"errors"
	"strings"
	"testing"

	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

func TestNormalizeRunCorrelationFilters(t *testing.T) {
	for _, tc := range []struct {
		name, source, occurrence, field string
	}{
		{"pair", "source-uid", "occurrence-key", ""},
		{"source only", "source-uid", "", ""},
		{"occurrence only", "", "occurrence-key", ""},
		{"oversize source", strings.Repeat("s", 129), "", "source_uid"},
		{"oversize occurrence", "", strings.Repeat("o", 65), "occurrence_key"},
		{"source control", "source\nuid", "", "source_uid"},
		{"occurrence control", "", "key\x00", "occurrence_key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeListInput(ListInput{
				TenantID: ulidTenant, SourceUID: tc.source, OccurrenceKey: tc.occurrence,
			})
			if tc.field != "" {
				var invalid *validation.Error
				if !errors.As(err, &invalid) || invalid.Field != tc.field {
					t.Fatalf("got %v, want validation field %s", err, tc.field)
				}
				return
			}
			if err != nil || got.SourceUID != tc.source || got.OccurrenceKey != tc.occurrence {
				t.Fatalf("filters changed: %+v, %v", got, err)
			}
		})
	}
	_, err := NormalizeListInput(ListInput{
		ResourceGroupID: "group", SourceUID: strings.Repeat("s", 129),
	})
	var scope *resource.ScopeError
	if !errors.As(err, &scope) {
		t.Fatalf("scope must be checked before filters: %v", err)
	}
}
