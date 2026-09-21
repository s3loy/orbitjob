package v1alpha1

// RunObjectName derives the JobRun object name for one occurrence: the owning
// ScheduledJob's name plus the first eight characters of the occurrence key.
// The occurrence key is a sha256 digest, so its first eight characters are
// stable and form a valid object-name suffix; a malformed or legacy key falls
// back to "unknown" rather than panicking a reconcile or schedule loop.
//
// Every writer of a JobRun (scheduler, manual trigger, cancel request) derives
// the name through this one function, so a run stored in the ledger is
// addressable in Kubernetes by any component that knows the definition name
// and the occurrence key.
func RunObjectName(scheduledJobName, occurrenceKey string) string {
	suffix := occurrenceKey
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	if suffix == "" {
		suffix = "unknown"
	}
	return scheduledJobName + "-" + suffix
}

// WorkflowRunObjectName derives the WorkflowRun object name for one manual
// workflow run: the WorkflowJob's name plus the first eight characters of the
// occurrence key, the same derivation as RunObjectName. The two kinds live in
// separate resource namespaces, so a workflow run and a step run may share a
// generated suffix without colliding. Every writer of a WorkflowRun derives
// the name through this one function, so a run stored in the ledger is
// addressable in Kubernetes by any component that knows the workflow name and
// the occurrence key.
func WorkflowRunObjectName(workflowJobName, occurrenceKey string) string {
	return RunObjectName(workflowJobName, occurrenceKey)
}
