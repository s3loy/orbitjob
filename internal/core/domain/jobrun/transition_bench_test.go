package jobrun

import "testing"

// benchRun keeps benchmark results alive so the compiler cannot drop the calls.
var benchRun JobRun

// Transition benchmarks cover the run lifecycle the ledger enforces on every
// phase write: the operator's observation loop calls Transition (or its
// refusing path) once per observed Job condition, so the guard is on the hot
// path of every run. None of this is expensive — CanTransition is a switch —
// and the benchmark exists to keep a future rewrite (a map-based table, added
// validation, epoch checks) from silently costing more than the switch it
// replaced. Invalid transitions are benched because late observations of
// already-terminal runs make refusals a steady-state event, not an error case.
func BenchmarkTransition(b *testing.B) {
	b.Run("valid/single-edge", func(b *testing.B) {
		run := JobRun{ID: "run-1", Phase: Running}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			benchRun, _ = Transition(run, Succeeded)
		}
	})

	b.Run("invalid/terminal-rewrite", func(b *testing.B) {
		run := JobRun{ID: "run-1", Phase: Succeeded}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			benchRun, _ = Transition(run, Running)
		}
	})

	// One attempt cycle including a platform retry: Pending -> CreatingAttempt
	// -> Running -> RetryWaiting -> CreatingAttempt -> Running -> Succeeded.
	// Seven guard evaluations per iteration is one realistic operator walk.
	b.Run("valid/full-lifecycle", func(b *testing.B) {
		start := pendingRun()
		steps := []Phase{CreatingAttempt, Running, RetryWaiting, CreatingAttempt, Running, Succeeded}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			run := start
			for _, to := range steps {
				run, _ = Transition(run, to)
			}
			benchRun = run
		}
	})

	// CanTransition answers every (from, to) pair the nine phases define; the
	// matrix is the full decision surface a caller such as RequestCancel
	// consults before writing.
	b.Run("can-transition/full-matrix", func(b *testing.B) {
		phases := []Phase{
			Pending, CreatingAttempt, Running, RetryWaiting, Succeeded,
			Failed, CancelRequested, Canceled, CancelUnknown,
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, from := range phases {
				for _, to := range phases {
					if CanTransition(from, to) {
						benchSinkPhase = to
					}
				}
			}
		}
	})

	// Terminal is trivially a three-way comparison; it is benched only because
	// the observation loop asks it per event and the check costs nothing to keep.
	b.Run("terminal/all-phases", func(b *testing.B) {
		phases := []Phase{
			Pending, CreatingAttempt, Running, RetryWaiting, Succeeded,
			Failed, CancelRequested, Canceled, CancelUnknown,
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, p := range phases {
				if Terminal(p) {
					benchSinkPhase = p
				}
			}
		}
	})
}

var benchSinkPhase Phase
