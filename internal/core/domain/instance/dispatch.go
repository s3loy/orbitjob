package instance

// DispatchAction constants
const (
	DispatchActionDispatch = "dispatch"
	DispatchActionSkip    = "skip"
	DispatchActionReplace = "replace"
)

// DispatchInput contains the data needed to decide whether to dispatch one instance.
type DispatchInput struct {
	InstanceSnapshot  Snapshot
	ConcurrencyPolicy string // from jobs table: "allow", "forbid", "replace"
	RunningCount      int    // count of dispatched+running instances for same job
}

// DispatchDecision describes what the dispatcher should do with a claimed instance.
type DispatchDecision struct {
	Action            string
	CancelInstanceIDs []int64 // instance IDs to cancel (for replace policy)
}

// DecideDispatch determines whether to dispatch, skip, or replace based on concurrency policy.
// Pure function — no side effects, deterministic, easy to test.
func DecideDispatch(in DispatchInput) DispatchDecision {
	switch in.ConcurrencyPolicy {
	case "allow":
		return DispatchDecision{Action: DispatchActionDispatch}
	case "forbid":
		if in.RunningCount > 0 {
			return DispatchDecision{Action: DispatchActionSkip}
		}
		return DispatchDecision{Action: DispatchActionDispatch}
	case "replace":
		if in.RunningCount > 0 {
			return DispatchDecision{Action: DispatchActionReplace}
		}
		return DispatchDecision{Action: DispatchActionDispatch}
	default:
		return DispatchDecision{Action: DispatchActionDispatch}
	}
}
