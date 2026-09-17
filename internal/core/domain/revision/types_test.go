package revision

import (
	"errors"
	"testing"
	"time"
)

func TestNewRevision(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	identity := Identity{SourceMode: "kubernetes", SourceUID: "uid-1", Namespace: "finance", Name: "report"}
	got, err := New(identity, 3, `{"schedule":"0 2 * * *"}`, "operator", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != 3 || got.SpecHash == "" || got.CreatedAt != now {
		t.Fatalf("unexpected revision: %+v", got)
	}
	again, err := New(identity, 3, `{"schedule":"0 2 * * *"}`, "operator", "", now)
	if err != nil || !got.IsImmutableComparedTo(again) {
		t.Fatalf("revision is not deterministic: %+v %+v %v", got, again, err)
	}
}

func TestNewRevisionRejectsRemainingInvalidInput(t *testing.T) {
	valid := Identity{SourceMode: "kubernetes", SourceUID: "uid-1", Namespace: "finance", Name: "report"}
	now := time.Unix(100, 0).UTC()
	tests := []struct {
		name       string
		identity   Identity
		generation int64
		spec       string
		at         time.Time
		want       error
	}{
		{"empty source mode", Identity{SourceUID: "u", Namespace: "n", Name: "j"}, 1, "spec", now, ErrInvalidIdentity},
		{"empty source uid", Identity{SourceMode: "kubernetes", Namespace: "n", Name: "j"}, 1, "spec", now, ErrInvalidIdentity},
		{"empty namespace", Identity{SourceMode: "kubernetes", SourceUID: "u", Name: "j"}, 1, "spec", now, ErrInvalidIdentity},
		{"empty name", Identity{SourceMode: "kubernetes", SourceUID: "u", Namespace: "n"}, 1, "spec", now, ErrInvalidIdentity},
		{"zero generation", valid, 0, "spec", now, ErrInvalidInput},
		{"negative generation", valid, -1, "spec", now, ErrInvalidInput},
		// A whitespace-only spec would hash to a real value while carrying no
		// content, letting an empty definition pass for a definition.
		{"blank spec", valid, 1, "   \n", now, ErrInvalidInput},
		{"empty spec", valid, 1, "", now, ErrInvalidInput},
		{"zero clock", valid, 1, "spec", time.Time{}, ErrInvalidInput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.identity, tt.generation, tt.spec, "actor", "", tt.at); !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestIsImmutableComparedToDetectsEveryDifference(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	base, err := New(Identity{SourceMode: "kubernetes", SourceUID: "u", Namespace: "n", Name: "j"}, 1, "spec", "actor", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if !base.IsImmutableComparedTo(base) {
		t.Fatal("a revision must equal itself")
	}

	other, err := New(Identity{SourceMode: "kubernetes", SourceUID: "u", Namespace: "n", Name: "j"}, 1, "other-spec", "actor", "", now)
	if err != nil {
		t.Fatal(err)
	}
	// Different spec means different revision, which is what makes the hash a
	// usable identity for deduplication.
	if base.IsImmutableComparedTo(other) {
		t.Fatal("differing specs must not compare equal")
	}

	newer, err := New(Identity{SourceMode: "kubernetes", SourceUID: "u", Namespace: "n", Name: "j"}, 2, "spec", "actor", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if base.IsImmutableComparedTo(newer) {
		t.Fatal("differing generations must not compare equal")
	}
}
