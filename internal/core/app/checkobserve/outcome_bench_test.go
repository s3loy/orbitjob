package checkobserve

import (
	"testing"
	"time"

	"orbitjob/internal/core/domain/checkrun"
	"orbitjob/internal/core/domain/sli"
)

// benchGood keeps benchmark results alive so the compiler cannot drop the calls.
var benchGood bool
var benchRunID string

// When a check run reaches a terminal phase, the recorder derives what the
// outcome means for every SLI watching that source: one isGoodEvent decision
// per SLI per run, and one occurrenceRunID per read-model upsert. Both are
// pure derivation over small inputs and cost almost nothing; the benchmark
// pins them because they sit under RecordTerminalPhase, whose budget is
// "negligible next to the store round trips it bookkeeps". The criteria
// shapes are the ones criteria authors actually write: a bare status match,
// a latency threshold, and the unknown-producer key that must keep refusing.
func BenchmarkTerminalOutcomeDerivation(b *testing.B) {
	event := outcomeEvent{
		Status:    checkrun.StatusSuccess,
		Duration:  250 * time.Millisecond,
		hasTiming: true,
	}

	b.Run("is-good/availability-default", func(b *testing.B) {
		s := sli.Snapshot{SLIType: sli.TypeAvailability}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			benchGood, _ = isGoodEvent(event, s)
		}
	})

	b.Run("is-good/latency-threshold", func(b *testing.B) {
		s := sli.Snapshot{
			SLIType:           sli.TypeLatency,
			GoodEventCriteria: map[string]any{"duration_ms": map[string]any{"op": "<=", "value": 1000}},
		}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			benchGood, _ = isGoodEvent(event, s)
		}
	})

	b.Run("is-good/status-criterion", func(b *testing.B) {
		s := sli.Snapshot{
			SLIType:           sli.TypeQuality,
			GoodEventCriteria: map[string]any{"status": "success"},
		}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			benchGood, _ = isGoodEvent(event, s)
		}
	})

	// Severity has no producer in exit-code-only evaluation; this criteria
	// shape must take the error path that skips the snapshot, never count.
	b.Run("is-good/unknown-producer-key", func(b *testing.B) {
		s := sli.Snapshot{
			SLIType:           sli.TypeQuality,
			GoodEventCriteria: map[string]any{"severity": "critical"},
		}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			benchGood, _ = isGoodEvent(event, s)
		}
	})

	// occurrenceRunID shapes the read model's run id from the ledger's sha256
	// occurrence key; the 64-hex input is the only shape production produces.
	b.Run("occurrence-run-id", func(b *testing.B) {
		key := "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			benchRunID = occurrenceRunID(key)
		}
	})
}
