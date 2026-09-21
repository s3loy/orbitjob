package main

import (
	"strings"
	"testing"
)

func TestPrintTableColumnAlignment(t *testing.T) {
	out := captureStdout(t, func() {
		printTable(
			[]string{"ID", "PHASE"},
			[][]string{
				{"1", "Running"},
				{"100", "Failed"},
			},
		)
	})
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines (header, rule, two rows), got %d:\n%s", len(lines), out)
	}
	// Columns line up: the second column starts at the same offset in every
	// line (header "ID" is padded to the widest cell "100").
	offset := strings.Index(lines[0], "PHASE")
	if offset < 0 {
		t.Fatalf("header missing PHASE: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1][offset:], "--") {
		t.Fatalf("separator dashes not aligned at column offset %d: %q", offset, lines[1])
	}
	if got := strings.Index(lines[2], "Running"); got != offset {
		t.Fatalf("row 1 second column at %d, want %d: %q", got, offset, lines[2])
	}
	if got := strings.Index(lines[3], "Failed"); got != offset {
		t.Fatalf("row 2 second column at %d, want %d: %q", got, offset, lines[3])
	}
	if strings.HasSuffix(lines[0], " ") || strings.HasSuffix(lines[2], " ") {
		t.Fatalf("trailing padding must be trimmed: %q", out)
	}
}

func TestPrintTableEmptyRows(t *testing.T) {
	out := captureStdout(t, func() {
		printTable([]string{"A", "B"}, nil)
	})
	if !strings.Contains(out, "A") {
		t.Fatalf("header missing: %q", out)
	}
}

func TestPrintDetail(t *testing.T) {
	out := captureStdout(t, func() {
		printDetail([]detailField{
			field("id", "7"),
			fieldTime("created_at", ""),
			fieldPtr("next_run_at", nil),
			fieldBool("suspend", true),
		})
	})
	for _, want := range []string{"id: 7", "created_at: -", "next_run_at: -", "suspend: true"} {
		if !strings.Contains(out, want) {
			t.Fatalf("detail output missing %q:\n%s", want, out)
		}
	}
}
