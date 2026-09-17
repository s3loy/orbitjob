package policy

import "testing"

func TestResolveSelfSubstitutesTenant(t *testing.T) {
	doc := Document{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:self:ci:job/*"}},
	}}
	got := ResolveSelf(doc, "T1")
	req := Request{Action: "job:Get", Resource: ARN{Tenant: "T1", Group: "ci", Type: "job", ID: "1"}}
	if Evaluate([]Document{got}, req) != DecisionAllow {
		t.Fatal("self should resolve to the binding tenant")
	}
	req = Request{Action: "job:Get", Resource: ARN{Tenant: "T2", Group: "ci", Type: "job", ID: "1"}}
	if Evaluate([]Document{got}, req) != DecisionDeny {
		t.Fatal("self must not leak into another tenant")
	}
}

// The substitution must not touch a pattern that merely contains the letters,
// and must leave a platform-wide pattern alone.
func TestResolveSelfLeavesOtherPatternsAlone(t *testing.T) {
	doc := Document{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{
			"orbitjob:*:*:job/*",
			"orbitjob:selfish:ci:job/*",
		}},
	}}
	got := ResolveSelf(doc, "T1")
	want := []string{"orbitjob:*:*:job/*", "orbitjob:selfish:ci:job/*"}
	for i, pattern := range got.Statement[0].Resource {
		if pattern != want[i] {
			t.Fatalf("pattern %d = %q, want %q", i, pattern, want[i])
		}
	}
}

// Resolving must not mutate the input: the shared preset is read concurrently.
func TestResolveSelfDoesNotMutateInput(t *testing.T) {
	doc := Document{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:self:ci:job/*"}},
	}}
	_ = ResolveSelf(doc, "T1")
	if doc.Statement[0].Resource[0] != "orbitjob:self:ci:job/*" {
		t.Fatal("ResolveSelf mutated its input")
	}
}
