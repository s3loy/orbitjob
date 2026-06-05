package sli

// CreateSpec is the normalized, validated specification for creating an SLI.
type CreateSpec struct {
	Name              string
	Description       *string
	SLIType           string
	SourceType        string
	SourceConfig      map[string]any
	Aggregation       string
	GoodEventCriteria map[string]any
}
