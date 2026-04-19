package instance

import "testing"

func TestDecideDispatch_AllowAlwaysDispatches(t *testing.T) {
	decision := DecideDispatch(DispatchInput{
		ConcurrencyPolicy: "allow",
		RunningCount:      5,
	})
	if decision.Action != DispatchActionDispatch {
		t.Fatalf("expected dispatch, got %q", decision.Action)
	}
}

func TestDecideDispatch_AllowWithZeroRunning(t *testing.T) {
	decision := DecideDispatch(DispatchInput{
		ConcurrencyPolicy: "allow",
		RunningCount:      0,
	})
	if decision.Action != DispatchActionDispatch {
		t.Fatalf("expected dispatch, got %q", decision.Action)
	}
}

func TestDecideDispatch_Forbid_SkipsWhenRunning(t *testing.T) {
	decision := DecideDispatch(DispatchInput{
		ConcurrencyPolicy: "forbid",
		RunningCount:      1,
	})
	if decision.Action != DispatchActionSkip {
		t.Fatalf("expected skip, got %q", decision.Action)
	}
}

func TestDecideDispatch_Forbid_DispatchesWhenNoRunning(t *testing.T) {
	decision := DecideDispatch(DispatchInput{
		ConcurrencyPolicy: "forbid",
		RunningCount:      0,
	})
	if decision.Action != DispatchActionDispatch {
		t.Fatalf("expected dispatch, got %q", decision.Action)
	}
}

func TestDecideDispatch_Forbid_SkipsWithDispatchingCount(t *testing.T) {
	decision := DecideDispatch(DispatchInput{
		ConcurrencyPolicy: "forbid",
		RunningCount:      3,
	})
	if decision.Action != DispatchActionSkip {
		t.Fatalf("expected skip when running_count=3, got %q", decision.Action)
	}
}

func TestDecideDispatch_Replace_ReplacesWhenRunning(t *testing.T) {
	decision := DecideDispatch(DispatchInput{
		ConcurrencyPolicy: "replace",
		RunningCount:      2,
	})
	if decision.Action != DispatchActionReplace {
		t.Fatalf("expected replace, got %q", decision.Action)
	}
}

func TestDecideDispatch_Replace_DispatchesWhenNoRunning(t *testing.T) {
	decision := DecideDispatch(DispatchInput{
		ConcurrencyPolicy: "replace",
		RunningCount:      0,
	})
	if decision.Action != DispatchActionDispatch {
		t.Fatalf("expected dispatch, got %q", decision.Action)
	}
}

func TestDecideDispatch_UnknownPolicyDefaultsToAllow(t *testing.T) {
	decision := DecideDispatch(DispatchInput{
		ConcurrencyPolicy: "unknown",
		RunningCount:      0,
	})
	if decision.Action != DispatchActionDispatch {
		t.Fatalf("expected dispatch for unknown policy, got %q", decision.Action)
	}
}

func TestDecideDispatch_EmptyPolicyDefaultsToAllow(t *testing.T) {
	decision := DecideDispatch(DispatchInput{
		ConcurrencyPolicy: "",
		RunningCount:      10,
	})
	if decision.Action != DispatchActionDispatch {
		t.Fatalf("expected dispatch for empty policy, got %q", decision.Action)
	}
}
