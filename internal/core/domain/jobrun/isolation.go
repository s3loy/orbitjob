package jobrun

import "time"

// IsolationSummary describes the current ledger obligations for one phase.
// It includes runs whose Kubernetes representation has disappeared.
type IsolationSummary struct {
	Phase    Phase
	Count    int64
	OldestAt time.Time
}
