package sli

// CreateSpec is the normalized, validated specification for creating an SLI.
type CreateSpec struct {
	Name        string
	Description *string
	// ResourceGroupID is the creating key's own scope, recorded so a
	// group-scoped key can see the rows it created.
	ResourceGroupID   string
	SLIType           string
	SourceType        string
	SourceConfig      map[string]any
	Aggregation       string
	GoodEventCriteria map[string]any
}
