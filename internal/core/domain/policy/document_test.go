package policy

import "testing"

func TestParseDocumentAcceptsValid(t *testing.T) {
	raw := []byte(`{"version":"1","statement":[
		{"effect":"Allow","action":["job:Get"],"resource":["orbitjob:T1:ci:job/*"]}
	]}`)
	doc, err := ParseDocument(raw)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	if len(doc.Statement) != 1 || doc.Statement[0].Effect != EffectAllow {
		t.Fatalf("unexpected document %+v", doc)
	}
}

func TestParseDocumentRejectsBadShapes(t *testing.T) {
	cases := map[string]string{
		"not json":       `{`,
		"no statements":  `{"version":"1","statement":[]}`,
		"unknown effect": `{"version":"1","statement":[{"effect":"Maybe","action":["job:Get"],"resource":["orbitjob:T1:ci:job/*"]}]}`,
		"no actions":     `{"version":"1","statement":[{"effect":"Allow","action":[],"resource":["orbitjob:T1:ci:job/*"]}]}`,
		"no resources":   `{"version":"1","statement":[{"effect":"Allow","action":["job:Get"],"resource":[]}]}`,
		"malformed arn":  `{"version":"1","statement":[{"effect":"Allow","action":["job:Get"],"resource":["not-an-arn"]}]}`,
	}
	for name, raw := range cases {
		if _, err := ParseDocument([]byte(raw)); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

func TestARNStringRoundTrips(t *testing.T) {
	in := "orbitjob:T1:ci:job/7"
	arn, err := ParseARN(in)
	if err != nil {
		t.Fatalf("ParseARN: %v", err)
	}
	if got := arn.String(); got != in {
		t.Fatalf("String() = %q, want %q", got, in)
	}
}

func TestARNMatchesGroupSemantics(t *testing.T) {
	target := ARN{Tenant: "T1", Group: "ci", Type: "job", ID: "7"}
	// An empty group pattern means "every group", matching a pre-groups policy.
	if !(ARN{Tenant: "T1", Group: "", Type: "job", ID: "7"}).Matches(target) {
		t.Fatal("an empty group pattern must match any group")
	}
	if (ARN{Tenant: "T1", Group: "prod", Type: "job", ID: "7"}).Matches(target) {
		t.Fatal("a literal group must not match a different group")
	}
}

// A pattern that cannot be parsed at evaluation time is skipped rather than
// treated as a match. ParseDocument rejects these on write, so reaching this
// branch means the row predates validation or was written out of band.
func TestEvaluateSkipsUnparseablePattern(t *testing.T) {
	docs := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"garbage"}},
	}}}
	req := Request{Action: "job:Get", Resource: ARN{Tenant: "T1", Group: "ci", Type: "job", ID: "1"}}
	if got := Evaluate(docs, req); got != DecisionDeny {
		t.Fatalf("an unparseable pattern must not match, got %v", got)
	}
}

func TestPermitsEmptySubsetIsAlwaysAllowed(t *testing.T) {
	if !Permits(nil, nil) {
		t.Fatal("granting nothing is always a valid delegation")
	}
}

func TestPermitsSkipsUnparseableResource(t *testing.T) {
	superset := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}}
	subset := []Document{{Statement: []Statement{
		{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"garbage"}},
	}}}
	if Permits(subset, superset) {
		t.Fatal("a pattern that cannot be compared must not be treated as covered")
	}
}

func TestIntersectEmptyDocs(t *testing.T) {
	boundary := Document{Statement: []Statement{
		{Effect: EffectDeny, Action: []string{"job:Delete"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}
	got := Intersect(nil, boundary)
	if len(got) != 1 {
		t.Fatalf("only the boundary denies should remain, got %+v", got)
	}
}

func TestIntersectKeepsBoundaryDenyWhenNothingElseSurvives(t *testing.T) {
	docs := []Document{{Statement: []Statement{
		{Effect: EffectDeny, Action: []string{"job:Delete"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}}
	boundary := Document{Statement: []Statement{
		{Effect: EffectDeny, Action: []string{"job:Delete"}, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}
	got := Intersect(docs, boundary)
	req := Request{Action: "job:Delete", Resource: ARN{Tenant: "T1", Group: "ci", Type: "job", ID: "1"}}
	if Evaluate(got, req) != DecisionDeny {
		t.Fatal("denies survive from both sides")
	}
}
