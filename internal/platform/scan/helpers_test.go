package scan

import (
	"database/sql"
	"testing"
	"time"
)

// The NULL converters sit between every repository scan and the API's JSON
// responses. The contract to pin: SQL NULL becomes a nil pointer (omitted in
// JSON), and a valid value becomes a pointer the caller owns — a copy, not an
// alias into scanner state.

func TestNullTimePtr(t *testing.T) {
	t.Run("SQL NULL becomes nil", func(t *testing.T) {
		if got := NullTimePtr(sql.NullTime{}); got != nil {
			t.Fatalf("got %v, want nil", got)
		}
	})

	t.Run("a valid time becomes a set pointer", func(t *testing.T) {
		want := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
		got := NullTimePtr(sql.NullTime{Time: want, Valid: true})
		if got == nil {
			t.Fatal("got nil, want the time")
		}
		if !got.Equal(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
}

func TestNullStringPtr(t *testing.T) {
	t.Run("SQL NULL becomes nil", func(t *testing.T) {
		if got := NullStringPtr(sql.NullString{}); got != nil {
			t.Fatalf("got %q, want nil", *got)
		}
	})

	t.Run("a valid string becomes a set pointer", func(t *testing.T) {
		got := NullStringPtr(sql.NullString{String: "uid-1", Valid: true})
		if got == nil || *got != "uid-1" {
			t.Fatalf("got %v, want uid-1", got)
		}
	})

	t.Run("an empty but valid string is not NULL", func(t *testing.T) {
		// An empty string that the database actually stored must survive as a
		// pointer to empty, not collapse into a nil that would drop the field.
		got := NullStringPtr(sql.NullString{String: "", Valid: true})
		if got == nil || *got != "" {
			t.Fatalf("got %v, want a pointer to an empty string", got)
		}
	})
}
