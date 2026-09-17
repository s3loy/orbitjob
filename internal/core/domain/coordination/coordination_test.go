package coordination

import (
	"testing"
	"time"
)

func TestOccurrenceKeyIsDeterministicAndDistinct(t *testing.T) {
	at := time.Date(2026, 3, 1, 2, 0, 0, 0, time.UTC)
	first := OccurrenceKey("def-1", 7, at)
	if first != OccurrenceKey("def-1", 7, at) {
		t.Fatal("the same occurrence must produce the same key across processes")
	}
	if len(first) != 64 {
		t.Fatalf("key length = %d, want a full sha256 hex digest", len(first))
	}

	// Timezone must not change the key: the scheduler and a manual trigger can
	// observe the same instant expressed differently.
	offset := at.In(time.FixedZone("UTC+8", 8*3600))
	if OccurrenceKey("def-1", 7, offset) != first {
		t.Fatal("key depends on the time zone of the caller")
	}

	others := []string{
		OccurrenceKey("def-2", 7, at),
		OccurrenceKey("def-1", 8, at),
		OccurrenceKey("def-1", 7, at.Add(time.Minute)),
	}
	for _, other := range others {
		if other == first {
			t.Fatal("distinct occurrences must not collide")
		}
	}
}
