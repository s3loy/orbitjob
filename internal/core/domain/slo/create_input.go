package slo

import "time"

// Window type constants.
const (
	WindowTypeRolling  = "rolling"
	WindowTypeCalendar  = "calendar"
	WindowTypeQuarterly = "quarterly"
)

// Status constants.
const (
	StatusActive = "active"
	StatusPaused = "paused"
)

// ValidWindowTypes is the set of supported window types.
var ValidWindowTypes = map[string]bool{
	WindowTypeRolling:  true,
	WindowTypeCalendar:  true,
	WindowTypeQuarterly: true,
}

// ValidStatuses is the set of supported SLO statuses.
var ValidStatuses = map[string]bool{
	StatusActive: true,
	StatusPaused: true,
}

// MaxNameLength is the maximum length of an SLO name.
const MaxNameLength = 128

// MinTarget is the minimum valid SLO target (> 0).
const MinTarget = 0.0001

// MaxTarget is the maximum valid SLO target (<= 1).
const MaxTarget = 1.0

// DefaultFastBurnRate is the default fast burn rate threshold.
const DefaultFastBurnRate = 14.4

// DefaultSlowBurnRate is the default slow burn rate threshold.
const DefaultSlowBurnRate = 2.0

// Budget status constants.
const (
	BudgetHealthy   = "healthy"
	BudgetAtRisk    = "at_risk"
	BudgetExhausted = "exhausted"
)

// CreateInput is the raw input for creating an SLO.
type CreateInput struct {
	Name               string
	Description        *string
	SLIID              int64
	Target             float64
	WindowType         string
	WindowDuration     time.Duration
	AlertFastBurnRate  float64
	AlertSlowBurnRate  float64
}

// CreateSpec is the normalized, validated specification for creating an SLO.
type CreateSpec struct {
	Name              string
	Description       *string
	SLIID             int64
	Target            float64
	WindowType        string
	WindowDuration    time.Duration
	AlertFastBurnRate float64
	AlertSlowBurnRate float64
}
