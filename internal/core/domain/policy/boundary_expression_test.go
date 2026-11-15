package policy

import (
	"encoding/json"
	"testing"
)

func boundaryDoc(effect Effect, actions []string, resource string) Document {
	return Document{Version: "1", Statement: []Statement{{Effect: effect, Action: actions, Resource: []string{resource}}}}
}
func TestBoundaryEvaluatesActualWildcardIntersection(t *testing.T) {
	grants := []Document{boundaryDoc(EffectAllow, []string{"*"}, "orbitjob:*:*:*/*")}
	boundary := boundaryDoc(EffectAllow, []string{"job:Get"}, "orbitjob:T1:ci:job/allowed")
	effective := Intersect(grants, boundary)
	for _, tc := range []struct {
		action, tenant, group, id string
		want                      Decision
	}{
		{"job:Get", "T1", "ci", "allowed", DecisionAllow},
		{"job:List", "T1", "ci", "allowed", DecisionDeny},
		{"job:Get", "T2", "ci", "allowed", DecisionDeny},
		{"job:Get", "T1", "prod", "allowed", DecisionDeny},
		{"job:Get", "T1", "ci", "other", DecisionDeny},
	} {
		request := Request{Action: tc.action, Resource: ARN{Tenant: tc.tenant, Group: tc.group, Type: "job", ID: tc.id}}
		if got := Evaluate(effective, request); got != tc.want {
			t.Fatalf("%+v got %s", tc, got)
		}
	}
	// In-memory effective expressions cannot persist their raw wildcard grants.
	raw, err := json.Marshal(effective)
	if err != nil {
		t.Fatal(err)
	}
	var persisted []Document
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	request := Request{Action: "job:Get", Resource: ARN{Tenant: "T2", Group: "ci", Type: "job", ID: "other"}}
	if Evaluate(persisted, request) != DecisionDeny {
		t.Fatal("serialization dropped boundary and broadened grant")
	}
}
func TestNestedBoundariesAndGlobalDenies(t *testing.T) {
	grants := []Document{boundaryDoc(EffectAllow, []string{"*"}, "orbitjob:*:*:*/*")}
	first := boundaryDoc(EffectAllow, []string{"job:Get", "job:List"}, "orbitjob:T1:*:job/*")
	second := boundaryDoc(EffectAllow, []string{"job:Get"}, "orbitjob:*:ci:job/*")
	effective := Intersect(Intersect(grants, first), second)
	request := Request{Action: "job:Get", Resource: ARN{Tenant: "T1", Group: "ci", Type: "job", ID: "1"}}
	if Evaluate(effective, request) != DecisionAllow {
		t.Fatal("nested intersection lost allowed request")
	}
	request.Action = "job:List"
	if Evaluate(effective, request) != DecisionDeny {
		t.Fatal("nested boundary broadened action")
	}
	request.Action = "job:Get"
	// A deny remains global even if a sibling grant would allow it.
	deny := boundaryDoc(EffectDeny, []string{"job:Get"}, "orbitjob:T1:ci:job/1")
	denied := Intersect([]Document{deny}, second)
	if Evaluate(append(effective, denied...), request) != DecisionDeny {
		t.Fatal("nested deny hidden by sibling allow")
	}
	if Evaluate(Intersect(nil, second), request) != DecisionDeny {
		t.Fatal("boundary granted without source grants")
	}
}
func TestDelegationCannotProbePastBoundaryOrDeny(t *testing.T) {
	grants := []Document{boundaryDoc(EffectAllow, []string{"*"}, "orbitjob:*:*:*/*")}
	boundary := boundaryDoc(EffectAllow, []string{"job:Get"}, "orbitjob:T1:*:job/*")
	boundary.Statement = append(boundary.Statement, Statement{Effect: EffectDeny, Action: []string{"job:Get"}, Resource: []string{"orbitjob:T1:*:job/secret"}})
	caller := Intersect(grants, boundary)
	for _, resource := range []string{"orbitjob:*:*:job/*", "orbitjob:T1:*:job/*", "orbitjob:T1:ci:job/secret"} {
		if Permits([]Document{boundaryDoc(EffectAllow, []string{"job:Get"}, resource)}, caller) {
			t.Fatalf("unsafe delegation %s", resource)
		}
	}
	if !Permits([]Document{boundaryDoc(EffectAllow, []string{"job:Get"}, "orbitjob:T1:ci:job/public")}, caller) {
		t.Fatal("safe narrow delegation refused")
	}
	if Permits([]Document{boundaryDoc(EffectAllow, []string{"job:List"}, "orbitjob:T1:ci:job/public")}, caller) {
		t.Fatal("action escaped boundary")
	}
}
