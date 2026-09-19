package checkobserve

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"orbitjob/internal/core/domain/checkrun"
	"orbitjob/internal/core/domain/sli"
)

// outcomeEvent is the one event a terminal run contributes to SLI evaluation:
// what the exit code decided, and how long the attempt took. Status uses the
// read model's vocabulary ("success"/"failed"), which is what criteria authors
// write against.
type outcomeEvent struct {
	Status    string
	Duration  time.Duration
	hasTiming bool
}

func (e outcomeEvent) durationMs() float64 { return float64(e.Duration.Milliseconds()) }

// isGoodEvent decides whether one run outcome satisfies an SLI's good-event
// criteria. The shape follows the criteria the retired check_run recorder
// parsed, minus the vocabulary that no longer exists: there is no severity and
// no probe output in v1, so criteria referencing them are configuration errors
// that skip the snapshot rather than silently counting every event as good.
func isGoodEvent(event outcomeEvent, s sli.Snapshot) (bool, error) {
	switch s.SLIType {
	case sli.TypeAvailability:
		// Default: a successful probe is the good event.
		if len(s.GoodEventCriteria) == 0 {
			return event.Status == checkrun.StatusSuccess, nil
		}
		return evaluateCriteria(event, s.GoodEventCriteria)

	case sli.TypeLatency:
		// Latency is meaningless without a threshold to judge the duration by,
		// e.g. {"duration_ms": {"op": "<=", "value": 1000}}.
		if len(s.GoodEventCriteria) == 0 {
			return false, fmt.Errorf("latency sli requires good_event_criteria")
		}
		return evaluateCriteria(event, s.GoodEventCriteria)

	case sli.TypeQuality:
		// Quality graded probe severities, which exit-code-only evaluation
		// does not produce. An explicit status or duration criterion still
		// works; anything else cannot be satisfied by this event.
		if len(s.GoodEventCriteria) == 0 {
			return false, fmt.Errorf("quality sli requires good_event_criteria with status or duration_ms")
		}
		return evaluateCriteria(event, s.GoodEventCriteria)

	default:
		return evaluateCriteria(event, s.GoodEventCriteria)
	}
}

// evaluateCriteria evaluates one outcome against generic criteria. Every key
// must hold for the event to count as good.
func evaluateCriteria(event outcomeEvent, criteria map[string]any) (bool, error) {
	for key, expected := range criteria {
		switch key {
		case "status":
			expectedStatus, ok := expected.(string)
			if !ok {
				return false, fmt.Errorf("status criteria must be a string, got %T", expected)
			}
			if event.Status != expectedStatus {
				return false, nil
			}
		case "duration_ms":
			if !event.hasTiming {
				return false, nil
			}
			good, err := evaluateNumericCondition(event.durationMs(), expected)
			if err != nil {
				return false, err
			}
			if !good {
				return false, nil
			}
		default:
			// Severity and probe-output keys have no v1 producer; counting
			// events against them would fabricate a signal.
			return false, fmt.Errorf("criteria key %q has no producer on run outcomes", key)
		}
	}
	return true, nil
}

// evaluateNumericCondition evaluates a numeric condition like
// {"op": "<=", "value": 1000} against an actual value.
func evaluateNumericCondition(actual float64, condition any) (bool, error) {
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

// occurrenceRunID derives the read model's run id from the ledger occurrence
// key. The key is a sha256 hex digest, so its first 32 hex characters, shaped
// as a UUID, are stable and unique per occurrence; a key of any other shape
// falls back to hashing the same way, so the derivation always answers.
func occurrenceRunID(occurrenceKey string) string {
	digest := occurrenceKey
	if len(digest) != sha256.Size*2 {
		sum := sha256.Sum256([]byte(occurrenceKey))
		digest = hex.EncodeToString(sum[:])
	}
	hexRuns := []string{digest[0:8], digest[8:12], digest[12:16], digest[16:20], digest[20:32]}
	return hexRuns[0] + "-" + hexRuns[1] + "-" + hexRuns[2] + "-" + hexRuns[3] + "-" + hexRuns[4]
}

func formatInt(v int64) string {
	return strconv.FormatInt(v, 10)
}
