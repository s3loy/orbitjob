package policy

import "testing"

func TestPermitsRejectsWiderAction(t *testing.T) {
	superset := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}}
	subset := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Delete"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}}
	if Permits(subset, superset) {
		t.Fatal("job:Delete is not covered by job:Get")
	}
}

func TestPermitsRejectsWiderResource(t *testing.T) {
	superset := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:T1:ci:job/*"}},
	}}}
	subset := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:T1:prod:job/*"}},
	}}}
	if Permits(subset, superset) {
		t.Fatal("prod is not covered by a ci-only grant")
	}
}

func TestPermitsAcceptsNarrower(t *testing.T) {
	superset := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get", "job:List"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}}
	subset := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:T1:ci:job/*"}},
	}}}
	if !Permits(subset, superset) {
		t.Fatal("a narrower action and resource set is a valid subset")
	}
}

// A Deny in the superset does not make the subset valid; deny statements only
// ever remove permissions, so they cannot be the source of an allow.
func TestPermitsIgnoresDenyOnlySuperset(t *testing.T) {
	superset := []Document{{Statement: []Statement{
		{Effect: EffectDeny, Action: []string{"job:Get"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}}
	subset := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:T1:ci:job/*"}},
	}}}
	if Permits(subset, superset) {
		t.Fatal("deny-only statements grant nothing")
	}
}

// A single statement may list several actions; every one of them must be
// covered, not just the first. This is the shape that turns a delegation check
// into a rubber stamp when it is wrong.
func TestPermitsRequiresEveryActionCovered(t *testing.T) {
	superset := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}}
	subset := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get", "job:Delete"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}}
	if Permits(subset, superset) {
		t.Fatal("every action in the subset statement must be covered")
	}
}

// Likewise a statement may list several resource patterns, and each must be
// matched by the superset.
func TestPermitsRequiresEveryResourceCovered(t *testing.T) {
	superset := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:T1:ci:job/*"}},
	}}}
	subset := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"},
			Resource: []string{"orbitjob:T1:ci:job/*", "orbitjob:T1:prod:job/*"}},
	}}}
	if Permits(subset, superset) {
		t.Fatal("every resource pattern in the subset statement must be covered")
	}
}

// A deny in the subset needs no covering: it only removes permission, so it is
// never the thing being delegated.
func TestPermitsSkipsDenyInSubset(t *testing.T) {
	superset := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}}
	subset := []Document{{Statement: []Statement{
		{Effect: EffectDeny, Action: []string{"job:Delete"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}}
	if !Permits(subset, superset) {
		t.Fatal("a deny in the subset needs no covering")
	}
}

// A superset pattern that cannot be parsed covers nothing, so it must not be
// treated as a blanket approval.
func TestPermitsSkipsUnparseableSupersetPattern(t *testing.T) {
	superset := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"garbage"}},
	}}}
	subset := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:T1:ci:job/*"}},
	}}}
	if Permits(subset, superset) {
		t.Fatal("an unparseable superset pattern covers nothing")
	}
}

// A permission the granter has been explicitly denied must not be grantable.
// Comparing Allow lists alone would miss this: the granter's Allow covers
// job:Delete, but the Deny means it does not actually hold it.
func TestPermitsRespectsDenyInSuperset(t *testing.T) {
	superset := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{WildcardAction}, Resource: []string{"orbitjob:T1:*:job/*"}},
		{Effect: EffectDeny, Action: []string{"job:Delete"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}}
	subset := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Delete"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}}
	if Permits(subset, superset) {
		t.Fatal("a permission the caller is denied must not be grantable")
	}
}

// The same rule on the boundary side: a boundary that allows everything except
// one action must not let that action survive into the effective grants.
func TestIntersectRespectsDenyBehindWildcard(t *testing.T) {
	docs := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get", "job:Delete"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}}
	boundary := Document{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{WildcardAction}, Resource: []string{"orbitjob:T1:*:job/*"}},
		{Effect: EffectDeny, Action: []string{"job:Delete"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}
	got := Intersect(docs, boundary)
	req := Request{Action: "job:Delete", Resource: ARN{Tenant: "T1", Group: "ci", Type: "job", ID: "1"}}
	if Evaluate(got, req) != DecisionDeny {
		t.Fatal("a boundary deny must survive into the effective grants")
	}
	req = Request{Action: "job:Get", Resource: ARN{Tenant: "T1", Group: "ci", Type: "job", ID: "1"}}
	if Evaluate(got, req) != DecisionAllow {
		t.Fatal("the allowed action must survive")
	}
}

// A pattern that cannot be parsed cannot be shown to be permitted, so it must
// fail the subset check rather than be skipped over.
func TestPermitsRejectsUnparseableSubsetPattern(t *testing.T) {
	superset := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}}
	subset := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"not-an-arn"}},
	}}}
	if Permits(subset, superset) {
		t.Fatal("an unreadable pattern must not pass the subset check")
	}
}

// concreteTarget substitutes the probe into every wildcard segment and leaves
// literal segments alone.
func TestConcreteTargetSubstitutesOnlyWildcards(t *testing.T) {
	got, err := concreteTarget("orbitjob:T1:*:job/*")
	if err != nil {
		t.Fatalf("concreteTarget: %v", err)
	}
	if got.Tenant != "T1" {
		t.Fatalf("a literal tenant must be preserved, got %q", got.Tenant)
	}
	if got.Group != probeSegment || got.ID != probeSegment {
		t.Fatalf("wildcards must become the probe, got %+v", got)
	}
	if _, err := concreteTarget("garbage"); err == nil {
		t.Fatal("a malformed pattern must be reported")
	}
}
