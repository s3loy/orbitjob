package sli

// SLI type constants.
const (
	TypeAvailability = "availability"
	TypeLatency      = "latency"
	TypeQuality      = "quality"
	TypeCustom       = "custom"
)

// Source type constants.
const (
	// SourceTypeJobRun marks an SLI whose events derive from the run ledger:
	// source_config names its source definition by source_uid, e.g.
	// {"source_uid": "check-42"} for a check, or the ScheduledJob CR's UID for
	// a declared job. It replaced the check_run source, which read the old
	// check_runs work queue that no longer executes anything.
	SourceTypeJobRun = "job_run"
)

// Aggregation constants.
const (
	AggregationRatio = "ratio"
	AggregationCount = "count"
)

// ValidSLITypes is the set of supported SLI types.
var ValidSLITypes = map[string]bool{
	TypeAvailability: true,
	TypeLatency:      true,
	TypeQuality:      true,
	TypeCustom:       true,
}

// ValidSourceTypes is the set of supported source types.
var ValidSourceTypes = map[string]bool{
	SourceTypeJobRun: true,
}

// ValidAggregations is the set of supported aggregation methods.
var ValidAggregations = map[string]bool{
	AggregationRatio: true,
	AggregationCount: true,
}

// MaxNameLength is the maximum length of an SLI name.
const MaxNameLength = 128

// CreateInput is the raw input for creating an SLI.
type CreateInput struct {
	Name string
	// ResourceGroupID is the creating key's own scope.
	ResourceGroupID   string
	Description       *string
	SLIType           string
	SourceType        string
	SourceConfig      map[string]any
	Aggregation       string
	GoodEventCriteria map[string]any
}
