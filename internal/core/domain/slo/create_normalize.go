package slo

import (
	"fmt"
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

	if in.Target <= MinTarget || in.Target > MaxTarget {
		return CreateSpec{}, validation.New("target", fmt.Sprintf("target must be in range (%.4f, %.1f]", MinTarget, MaxTarget))
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
		SLIID:             in.SLIID,
		Target:            in.Target,
		WindowType:        windowType,
		WindowDuration:    windowDuration,
		AlertFastBurnRate: fastBurn,
		AlertSlowBurnRate: slowBurn,
	}, nil
}
