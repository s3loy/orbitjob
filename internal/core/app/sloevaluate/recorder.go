package sloevaluate

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"orbitjob/internal/core/domain/checkrun"
	"orbitjob/internal/core/domain/sli"
	"orbitjob/internal/platform/metrics"
)

const defaultSnapshotWindow = 5 * time.Minute

// sliReader finds SLIs associated with a check.
type sliReader interface {
	FindByCheckID(ctx context.Context, tenantID string, checkID int64) ([]sli.Snapshot, error)
}

// snapshotWriter increments the pre-aggregated SLI snapshot.
type snapshotWriter interface {
	IncrementSnapshot(ctx context.Context, tenantID string, sliID int64, windowStart time.Time, isGood bool) error
}

// CheckRunRecorder records check run results into SLI snapshots.
type CheckRunRecorder struct {
	sliReader      sliReader
	snapshotWriter snapshotWriter
	clock          func() time.Time
}

// NewCheckRunRecorder creates a new recorder.
func NewCheckRunRecorder(sliReader sliReader, snapshotWriter snapshotWriter) *CheckRunRecorder {
	return &CheckRunRecorder{
		sliReader:      sliReader,
		snapshotWriter: snapshotWriter,
		clock:          func() time.Time { return time.Now().UTC() },
	}
}

// RecordCheckRun evaluates a completed check run against all associated SLIs
// and increments the pre-aggregated snapshots.
func (r *CheckRunRecorder) RecordCheckRun(ctx context.Context, tenantID string, checkID int64, run checkrun.Snapshot) error {
	now := r.clock()

	slis, err := r.sliReader.FindByCheckID(ctx, tenantID, checkID)
	if err != nil {
		return fmt.Errorf("find slis for check %d: %w", checkID, err)
	}
	if len(slis) == 0 {
		return nil
	}

	windowStart := now.Truncate(defaultSnapshotWindow)

	for _, s := range slis {
		isGood, err := r.isGoodEvent(run, s)
		if err != nil {
			slog.Error("invalid sli configuration, skipping snapshot",
				"sli_id", s.ID,
				"check_run_id", run.ID,
				"error", err,
			)
			continue
		}
		if err := r.snapshotWriter.IncrementSnapshot(ctx, tenantID, s.ID, windowStart, isGood); err != nil {
			slog.Error("failed to increment sli snapshot",
				"sli_id", s.ID,
				"check_run_id", run.ID,
				"error", err,
			)
			continue
		}

		result := "bad"
		if isGood {
			result = "good"
		}
		metrics.SLIEvaluationsTotal.WithLabelValues(tenantID, s.SLIType, result).Inc()
		metrics.SLOSnapshotIncrementsTotal.WithLabelValues(tenantID, strconv.FormatInt(s.ID, 10)).Inc()
	}

	return nil
}

// isGoodEvent determines whether a check run satisfies the good event criteria for an SLI.
func (r *CheckRunRecorder) isGoodEvent(run checkrun.Snapshot, s sli.Snapshot) (bool, error) {
	switch s.SLIType {
	case sli.TypeAvailability:
		return r.isGoodAvailability(run, s), nil
	case sli.TypeLatency:
		return r.isGoodLatency(run, s)
	case sli.TypeQuality:
		return r.isGoodQuality(run, s)
	default:
		// For custom SLIs, use the good_event_criteria directly.
		return r.evaluateCriteria(run, s.GoodEventCriteria)
	}
}

func (r *CheckRunRecorder) isGoodAvailability(run checkrun.Snapshot, s sli.Snapshot) bool {
	// Default: success == good.
	if len(s.GoodEventCriteria) == 0 {
		return run.Status == checkrun.StatusSuccess
	}
	good, _ := r.evaluateCriteria(run, s.GoodEventCriteria)
	return good
}

func (r *CheckRunRecorder) isGoodLatency(run checkrun.Snapshot, s sli.Snapshot) (bool, error) {
	// For latency SLI, good_event_criteria should define a threshold.
	// Example: {duration_ms: {op: "<=", value: 1000}}
	if len(s.GoodEventCriteria) == 0 {
		return false, fmt.Errorf("latency sli requires good_event_criteria")
	}
	return r.evaluateCriteria(run, s.GoodEventCriteria)
}

func (r *CheckRunRecorder) isGoodQuality(run checkrun.Snapshot, s sli.Snapshot) (bool, error) {
	// Default: severity == ok is good.
	if len(s.GoodEventCriteria) == 0 {
		if run.Severity == nil {
			return false, fmt.Errorf("quality sli requires severity or good_event_criteria")
		}
		return *run.Severity == checkrun.SeverityOK, nil
	}
	return r.evaluateCriteria(run, s.GoodEventCriteria)
}

// evaluateCriteria evaluates a check run against generic criteria.
// Returns (false, error) if criteria are malformed instead of panicking.
func (r *CheckRunRecorder) evaluateCriteria(run checkrun.Snapshot, criteria map[string]any) (bool, error) {
	for key, expected := range criteria {
		switch key {
		case "status":
			expectedStr, ok := expected.(string)
			if !ok {
				slog.Warn("invalid criteria type for status", "expected_type", fmt.Sprintf("%T", expected))
				return false, fmt.Errorf("status criteria must be a string, got %T", expected)
			}
			if run.Status != expectedStr {
				return false, nil
			}
		case "severity":
			expectedStr, ok := expected.(string)
			if !ok {
				slog.Warn("invalid criteria type for severity", "expected_type", fmt.Sprintf("%T", expected))
				return false, fmt.Errorf("severity criteria must be a string, got %T", expected)
			}
			if run.Severity == nil || *run.Severity != expectedStr {
				return false, nil
			}
		case "duration_ms":
			if run.DurationMs == nil {
				return false, nil
			}
			good, err := r.evaluateNumericCondition(float64(*run.DurationMs), expected)
			if err != nil {
				return false, err
			}
			if !good {
				return false, nil
			}
		default:
			// For arbitrary output metrics, check the output map.
			if run.Output == nil {
				return false, nil
			}
			actual, ok := run.Output[key]
			if !ok {
				return false, nil
			}
			actualFloat, err := toFloat64(actual)
			if err != nil {
				return false, fmt.Errorf("output key %q: %w", key, err)
			}
			good, err := r.evaluateNumericCondition(actualFloat, expected)
			if err != nil {
				return false, err
			}
			if !good {
				return false, nil
			}
		}
	}
	return true, nil
}

// evaluateNumericCondition evaluates a numeric condition like {op: "<=", value: 1000}.
// Returns (false, error) for malformed conditions instead of panicking or silently defaulting.
func (r *CheckRunRecorder) evaluateNumericCondition(actual float64, condition any) (bool, error) {
	cond, ok := condition.(map[string]any)
	if !ok {
		// Simple equality.
		threshold, err := toFloat64(condition)
		if err != nil {
			return false, fmt.Errorf("numeric condition: %w", err)
		}
		return actual == threshold, nil
	}

	op, ok := cond["op"].(string)
	if !ok {
		return false, fmt.Errorf("numeric condition missing 'op' field")
	}
	threshold, err := toFloat64(cond["value"])
	if err != nil {
		return false, fmt.Errorf("numeric condition 'value': %w", err)
	}

	switch op {
	case "<":
		return actual < threshold, nil
	case "<=":
		return actual <= threshold, nil
	case ">":
		return actual > threshold, nil
	case ">=":
		return actual >= threshold, nil
	case "==":
		return actual == threshold, nil
	case "!=":
		return actual != threshold, nil
	default:
		return false, fmt.Errorf("unsupported numeric operator: %q", op)
	}
}

func toFloat64(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case int32:
		return float64(n), nil
	case uint:
		return float64(n), nil
	case uint64:
		return float64(n), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to float64", v)
	}
}
