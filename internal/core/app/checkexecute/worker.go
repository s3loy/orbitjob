package checkexecute

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"orbitjob/internal/core/app/evaluate"
	"orbitjob/internal/core/app/execute"
	"orbitjob/internal/core/app/execute/handler"
	"orbitjob/internal/core/domain/check"
	"orbitjob/internal/core/domain/checkrun"
	"orbitjob/internal/platform/metrics"
)

// TickUseCase executes check runs by claiming pending runs, running handlers,
// evaluating results, and persisting outcomes.
type TickUseCase struct {
	checkRepo    checkReader
	checkRunRepo checkRunRepository
	evaluator    *evaluate.Evaluator
	clock        func() time.Time
}

// NewTickUseCase creates a new check execution use case.
func NewTickUseCase(checkRepo checkReader, checkRunRepo checkRunRepository) *TickUseCase {
	return &TickUseCase{
		checkRepo:    checkRepo,
		checkRunRepo: checkRunRepo,
		evaluator:    evaluate.NewEvaluator(),
		clock:        func() time.Time { return time.Now().UTC() },
	}
}

type checkReader interface {
	GetByID(ctx context.Context, tenantID string, id int64) (check.Snapshot, error)
}

type checkRunRepository interface {
	ClaimNext(ctx context.Context, tenantID string, limit int, now time.Time) ([]checkrun.Snapshot, error)
	Complete(ctx context.Context, tenantID string, id int64, status, severity string, output, evaluationResult map[string]any, durationMs int, now time.Time) error
}

// RunBatch claims and executes pending check runs.
func (uc *TickUseCase) RunBatch(ctx context.Context, tenantID string, limit int) (int, error) {
	now := uc.clock()

	runs, err := uc.checkRunRepo.ClaimNext(ctx, tenantID, limit, now)
	if err != nil {
		return 0, fmt.Errorf("claim check runs: %w", err)
	}

	var executed int
	for _, run := range runs {
		if err := uc.executeRun(ctx, tenantID, run); err != nil {
			slog.Error("failed to execute check run", "run_id", run.RunID, "error", err)
			continue
		}
		executed++
	}

	return executed, nil
}

func (uc *TickUseCase) executeRun(ctx context.Context, tenantID string, run checkrun.Snapshot) error {
	start := uc.clock()

	// Load check definition for config and assertion rules.
	chk, err := uc.checkRepo.GetByID(ctx, tenantID, run.CheckID)
	if err != nil {
		return fmt.Errorf("get check %d: %w", run.CheckID, err)
	}

	// Build assigned task for handler.
	task := execute.AssignedTask{
		InstanceID:     run.ID,
		RunID:          run.RunID,
		TenantID:       tenantID,
		HandlerType:    "check_" + chk.CheckType,
		HandlerPayload: chk.CheckConfig,
		TimeoutSec:     chk.TimeoutSec,
		Attempt:        1,
		MaxAttempt:     1,
	}

	// Look up and execute handler.
	var result execute.Result
	if h, ok := handler.GetRegistered()[task.HandlerType]; ok {
		result = h.Execute(ctx, task)
	} else {
		// Fall back to built-in handlers.
		switch chk.CheckType {
		case check.CheckTypeHTTPHealth:
			hh := handler.NewHTTPHealth(nil)
			result = hh.Execute(ctx, task)
		default:
			result = execute.Result{
				Success:    false,
				ResultCode: "unknown_handler",
				ErrorMsg:   fmt.Sprintf("unknown check type: %s", chk.CheckType),
			}
		}
	}

	durationMs := int(uc.clock().Sub(start).Milliseconds())

	// Evaluate result.
	severity := evaluate.SeverityOK
	var evalResult evaluate.Result
	if result.Output != nil {
		evalResult = uc.evaluator.Evaluate(result.Output, chk.AssertionRules)
		severity = evalResult.OverallSeverity
	} else if !result.Success {
		severity = evaluate.SeverityCritical
		evalResult = evaluate.Result{
			OverallSeverity: evaluate.SeverityCritical,
			Passed:          0,
			Failed:          1,
			Results: []evaluate.RuleResult{
				{Metric: "execution", Passed: false, Severity: evaluate.SeverityCritical},
			},
		}
	}

	status := checkrun.StatusSuccess
	if !result.Success {
		status = checkrun.StatusFailed
	}

	evalResultMap := map[string]any{
		"overall_severity": evalResult.OverallSeverity,
		"passed":           evalResult.Passed,
		"failed":           evalResult.Failed,
	}
	if len(evalResult.Results) > 0 {
		evalResultMap["results"] = evalResult.Results
	}

	if err := uc.checkRunRepo.Complete(ctx, tenantID, run.ID, status, severity, result.Output, evalResultMap, durationMs, uc.clock()); err != nil {
		return fmt.Errorf("complete check run: %w", err)
	}

	metrics.CheckRunsCompletedTotal.WithLabelValues(tenantID, chk.CheckType, severity).Inc()
	if durationMs > 0 {
		metrics.CheckRunDurationSeconds.WithLabelValues(tenantID, chk.CheckType).Observe(float64(durationMs) / 1000.0)
	}

	return nil
}
