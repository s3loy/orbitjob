package slo

import (
	"fmt"
	"math"
	"strings"
	"time"

	"orbitjob/internal/domain/validation"
)

// NormalizeCreate validates and normalizes a CreateInput into a CreateSpec.
func NormalizeCreate(in CreateInput) (CreateSpec, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return CreateSpec{}, validation.New("name", "name is required")
	}
	if len(name) > MaxNameLength {
		return CreateSpec{}, validation.New("name", fmt.Sprintf("name must be at most %d characters", MaxNameLength))
	}

	if in.SLIID <= 0 {
		return CreateSpec{}, validation.New("sli_id", "sli_id must be positive")
	}

	if math.IsNaN(in.Target) || math.IsInf(in.Target, 0) || in.Target < MinTarget || in.Target > MaxTarget {
		return CreateSpec{}, validation.New("target", fmt.Sprintf("target must be in range [%.4f, %.1f]", MinTarget, MaxTarget))
	}

	windowType := in.WindowType
	if windowType == "" {
		windowType = WindowTypeRolling
	}
	if !ValidWindowTypes[windowType] {
		return CreateSpec{}, validation.New("window_type", fmt.Sprintf("unsupported window_type: %s", windowType))
	}

	const maxWindowDuration = 8760 * time.Hour // 1 year

	windowDuration := in.WindowDuration
	if windowDuration <= 0 {
		return CreateSpec{}, validation.New("window_duration", "window_duration must be positive")
	}
	if windowDuration > maxWindowDuration {
		return CreateSpec{}, validation.New("window_duration", "window_duration must be at most 1 year")
	}

	if math.IsNaN(in.AlertFastBurnRate) || math.IsInf(in.AlertFastBurnRate, 0) || math.IsNaN(in.AlertSlowBurnRate) || math.IsInf(in.AlertSlowBurnRate, 0) {
		return CreateSpec{}, validation.New("alert_fast_burn_rate", "burn thresholds must be finite")
	}
	fastBurn := in.AlertFastBurnRate
	if fastBurn <= 0 {
		fastBurn = DefaultFastBurnRate
	}

	slowBurn := in.AlertSlowBurnRate
	if slowBurn <= 0 {
		slowBurn = DefaultSlowBurnRate
	}

	if slowBurn >= fastBurn {
		return CreateSpec{}, validation.New("alert_slow_burn_rate", "slow burn rate must be less than fast burn rate")
	}

	return CreateSpec{
		Name:              name,
		Description:       in.Description,
		ResourceGroupID:   strings.TrimSpace(in.ResourceGroupID),
		SLIID:             in.SLIID,
		Target:            in.Target,
		WindowType:        windowType,
		WindowDuration:    windowDuration,
		AlertFastBurnRate: fastBurn,
		AlertSlowBurnRate: slowBurn,
	}, nil
}
