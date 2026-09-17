package policy

import "testing"

func TestIntersectNarrowsToBoundary(t *testing.T) {
	docs := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get", "job:Delete"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}}
	boundary := Document{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}
	got := Intersect(docs, boundary)
	req := Request{Action: "job:Delete", Resource: ARN{Tenant: "T1", Group: "ci", Type: "job", ID: "1"}}
	if Evaluate(got, req) != DecisionDeny {
		t.Fatal("the boundary does not allow job:Delete, so it must not survive")
	}
	req = Request{Action: "job:Get", Resource: ARN{Tenant: "T1", Group: "ci", Type: "job", ID: "1"}}
	if Evaluate(got, req) != DecisionAllow {
		t.Fatal("the boundary allows job:Get, so it survives")
	}
}

func TestIntersectWithDenyBoundary(t *testing.T) {
	docs := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Delete"}, Resource: []string{"orbitjob:T1:prod:job/*"}},
	}}}
	boundary := Document{Statement: []Statement{
		{Effect: EffectDeny, Action: []string{"job:Delete"}, Resource: []string{"orbitjob:T1:prod:job/*"}},
	}}
	got := Intersect(docs, boundary)
	req := Request{Action: "job:Delete", Resource: ARN{Tenant: "T1", Group: "prod", Type: "job", ID: "1"}}
	if Evaluate(got, req) != DecisionDeny {
		t.Fatal("a deny in the boundary must survive as a deny")
	}
}

// A boundary that allows nothing removes every allow; only its denies remain.
func TestIntersectWithEmptyBoundaryRemovesAllows(t *testing.T) {
	docs := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}}
	boundary := Document{Statement: []Statement{
		{Effect: EffectDeny, Action: []string{"job:Delete"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}
	got := Intersect(docs, boundary)
	req := Request{Action: "job:Get", Resource: ARN{Tenant: "T1", Group: "ci", Type: "job", ID: "1"}}
	if Evaluate(got, req) != DecisionDeny {
		t.Fatal("an allow the boundary does not permit must not survive")
	}
}
