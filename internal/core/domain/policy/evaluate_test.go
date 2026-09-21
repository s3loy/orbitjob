package policy

import "testing"

func TestEvaluateDenyBeatsAllow(t *testing.T) {
	docs := []Document{
		{Version: "1", Statement: []Statement{
			{Effect: EffectAllow, Action: []string{"job:Delete"}, Resource: []string{"orbitjob:T1:prod:job/*"}},
		}},
		{Version: "1", Statement: []Statement{
			{Effect: EffectDeny, Action: []string{"job:Delete"}, Resource: []string{"orbitjob:T1:prod:job/*"}},
		}},
	}
	req := Request{Action: "job:Delete", Resource: ARN{Tenant: "T1", Group: "prod", Type: "job", ID: "7"}}
	if got := Evaluate(docs, req); got != DecisionDeny {
		t.Fatalf("expected deny to win, got %v", got)
	}
}

func TestEvaluateDefaultsToDeny(t *testing.T) {
	docs := []Document{{Version: "1", Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:T1:ci:job/*"}},
	}}}
	req := Request{Action: "job:Delete", Resource: ARN{Tenant: "T1", Group: "ci", Type: "job", ID: "7"}}
	if got := Evaluate(docs, req); got != DecisionDeny {
		t.Fatalf("an unmatched action must deny, got %v", got)
	}
}

func TestEvaluateActionIsExact(t *testing.T) {
	// "job:*" is not a valid action and must not match; actions are exact strings.
	docs := []Document{{Version: "1", Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:*"}, Resource: []string{"orbitjob:T1:ci:job/*"}},
	}}}
	req := Request{Action: "job:Delete", Resource: ARN{Tenant: "T1", Group: "ci", Type: "job", ID: "7"}}
	if got := Evaluate(docs, req); got != DecisionDeny {
		t.Fatalf("wildcard actions are not supported, got %v", got)
	}
}

// The administrator preset carries the single wildcard the engine understands.
// It is an explicit exception, not a general facility: a resource-scoped form
// like "job:*" still matches nothing.
func TestEvaluateSupportsTheAdministratorWildcard(t *testing.T) {
	docs := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{WildcardAction}, Resource: []string{"orbitjob:*:*:*/*"}},
	}}}
	req := Request{Action: "job:Delete", Resource: ARN{Tenant: "T1", Group: "ci", Type: "job", ID: "1"}}
	if got := Evaluate(docs, req); got != DecisionAllow {
		t.Fatalf("the administrator wildcard must match every action, got %v", got)
	}
}
