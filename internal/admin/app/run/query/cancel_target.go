package query

// CancelTarget is the read model a cancel request needs: the run's current
// phase, plus the identity that names its JobRun Custom Resource. The ledger
// row carries the occurrence key and the revision it pinned, but not the
// definition's name or namespace; those live on the revision row, so the store
// joins to job_definition_revisions to assemble the full target.
type CancelTarget struct {
	Phase string
	// OccurrenceKey is the deduplication key the JobRun object name is derived
	// from.
	OccurrenceKey string
	// ScheduledJobName is the name of the ScheduledJob Custom Resource the run
	// was created from, which prefixes every JobRun object name.
	ScheduledJobName string
	// Namespace is the scheduling namespace the JobRun was published into.
	Namespace string
}
