package queryfilter

import (
	"strings"
	"testing"
)

func TestRangeInclusiveExclusiveAndOffset(t *testing.T) {
	from, before, err := Range("2026-10-19T08:00:00.123456+08:00", "2026-10-19T02:00:00+01:00")
	if err != nil || from != "2026-10-19T00:00:00.123456Z" || before != "2026-10-19T01:00:00Z" {
		t.Fatalf("%s %s %v", from, before, err)
	}
	for _, pair := range [][2]string{{"2026-10-19T00:00:00.0000010001Z", ""}, {"2026-10-19", ""}, {"", "bad"}, {"2026-10-19T00:00:00+24:00", ""}, {"2026-10-19T00:00:00,123Z", ""}, {"2026-10-19T00:00:00Z", "2026-10-19T08:00:00+08:00"}, {"2026-10-19T01:00:00Z", "2026-10-19T00:00:00Z"}} {
		if _, _, err := Range(pair[0], pair[1]); err == nil {
			t.Fatalf("accepted invalid range %v", pair)
		}
	}
	if a, b, err := Range("", ""); err != nil || a != "" || b != "" {
		t.Fatalf("empty range: %q %q %v", a, b, err)
	}
}
func TestSearchLiteralAndCharacterBound(t *testing.T) {
	if got, err := Search("  API_%  "); err != nil || got != "API_%" {
		t.Fatalf("search: %q %v", got, err)
	}
	if _, err := Search(strings.Repeat("\u754c", 128)); err != nil {
		t.Fatal(err)
	}
	if _, err := Search(strings.Repeat("\u754c", 129)); err == nil {
		t.Fatal("accepted overlong search")
	}
}

func TestMicrosecondBoundPreservesComparisonEdges(t *testing.T) {
	for input, want := range map[string]string{
		"": "", "invalid": "invalid",
		"2026-10-19T00:00:00.000000001Z":      "2026-10-19T00:00:00.000001Z",
		"2026-10-19T00:00:00.000000999Z":      "2026-10-19T00:00:00.000001Z",
		"2026-10-19T00:00:00.000001Z":         "2026-10-19T00:00:00.000001Z",
		"2026-10-19T08:00:00.999999999+08:00": "2026-10-19T00:00:01Z",
		"1969-12-31T23:59:59.999999999Z":      "1970-01-01T00:00:00Z",
	} {
		if got := MicrosecondBound(input); got != want {
			t.Errorf("bound %q: got %q want %q", input, got, want)
		}
	}
}
