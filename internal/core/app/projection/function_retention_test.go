package projection

import (
	"orbitjob/internal/core/domain/function"
	"testing"
)

func TestFunctionRevisionPreservesConfiguredHistory(t *testing.T) {
	spec := FunctionJobSpec(function.Definition{HistorySuccess: 11, HistoryFailed: 17})
	if spec.History.SuccessfulRuns != 11 || spec.History.FailedRuns != 17 {
		t.Fatalf("function history lost during execution projection: %+v", spec.History)
	}
}
