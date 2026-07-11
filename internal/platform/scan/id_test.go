package scan

import (
	"testing"
)

func TestGenerateID(t *testing.T) {
	id1 := GenerateID()
	id2 := GenerateID()
	if id1 == "" {
		t.Fatal("expected non-empty id")
	}
	if len(id1) != 26 {
		t.Fatalf("expected id length 26, got %d", len(id1))
	}
	if id1 == id2 {
		t.Fatal("expected unique ids")
	}
}
