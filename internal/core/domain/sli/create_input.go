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
	SourceTypeCheckRun = "check_run"
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
	SourceTypeCheckRun: true,
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
	Name              string
	Description       *string
	SLIType           string
	SourceType        string
	SourceConfig      map[string]any
	Aggregation       string
	GoodEventCriteria map[string]any
}
