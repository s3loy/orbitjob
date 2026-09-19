package policy

import (
	"errors"
	"testing"
)

func TestCheckGrantableRejectsEscalation(t *testing.T) {
	caller := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}}
	granted := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Delete"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}}
	if err := CheckGrantable(caller, granted); !errors.Is(err, ErrPrivilegeEscalation) {
		t.Fatalf("expected ErrPrivilegeEscalation, got %v", err)
	}
}

func TestCheckGrantableAllowsSubset(t *testing.T) {
	caller := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get", "job:List"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}}
	granted := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:T1:ci:job/*"}},
	}}}
	if err := CheckGrantable(caller, granted); err != nil {
		t.Fatalf("a subset must be grantable, got %v", err)
	}
}

func TestCheckGrantableTreatsEmptyGrantAsSubset(t *testing.T) {
	caller := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}}
	if err := CheckGrantable(caller, nil); err != nil {
		t.Fatalf("granting nothing is always allowed, got %v", err)
	}
}

func TestPropagateBoundaryInheritsCallerBoundary(t *testing.T) {
	callerBoundary := &Document{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get", "job:List"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}
	got, err := PropagateBoundary(callerBoundary, nil)
	if err != nil {
		t.Fatalf("PropagateBoundary: %v", err)
	}
	if got == nil {
		t.Fatal("a bounded caller must not create an unbounded key")
	}
	req := Request{Action: "job:Delete", Resource: ARN{Tenant: "T1", Group: "ci", Type: "job", ID: "1"}}
	if Evaluate([]Document{*got}, req) != DecisionDeny {
		t.Fatal("the inherited boundary must still cap the new key")
	}
}

func TestPropagateBoundaryRejectsWidening(t *testing.T) {
	callerBoundary := &Document{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:T1:ci:job/*"}},
	}}
	requested := &Document{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:T1:prod:job/*"}},
	}}
	if _, err := PropagateBoundary(callerBoundary, requested); !errors.Is(err, ErrPrivilegeEscalation) {
		t.Fatalf("a wider boundary must be rejected, got %v", err)
	}
}

func TestPropagateBoundaryAllowsNarrowing(t *testing.T) {
	callerBoundary := &Document{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get", "job:List"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}
	requested := &Document{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:T1:ci:job/*"}},
	}}
	got, err := PropagateBoundary(callerBoundary, requested)
	if err != nil {
		t.Fatalf("a narrower boundary is allowed, got %v", err)
	}
	if got != requested {
		t.Fatal("the requested boundary should be used when it is narrower")
	}
}

func TestPropagateBoundaryUnboundedCaller(t *testing.T) {
	got, err := PropagateBoundary(nil, nil)
	if err != nil {
		t.Fatalf("PropagateBoundary: %v", err)
	}
	if got != nil {
		t.Fatal("an unbounded caller creates an unbounded key")
	}
}
