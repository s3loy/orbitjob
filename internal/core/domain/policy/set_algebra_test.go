package policy

import (
	"math/rand"
	"testing"
)

func TestPermitsRespectsResourceExceptions(t *testing.T) {
	allow := Statement{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:T:*:job/*"}}
	deny := Statement{Effect: EffectDeny, Action: []string{"job:Get"}, Resource: []string{"orbitjob:T:prod:job/42"}}
	caller := []Document{{Statement: []Statement{allow, deny}}}
	if Permits([]Document{{Statement: []Statement{allow}}}, caller) {
		t.Fatal("delegation erased a concrete resource denial")
	}
	if !Permits(caller, caller) {
		t.Fatal("a grant retaining its own denial must remain a subset")
	}
}

func TestIntersectPreservesPartialResourceOverlap(t *testing.T) {
	docs := []Document{{Statement: []Statement{{Effect: EffectAllow, Action: []string{"*"}, Resource: []string{"orbitjob:T:*:job/*"}}}}}
	boundary := Document{Statement: []Statement{{Effect: EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:T:prod:job/42"}}}}
	got := Intersect(docs, boundary)
	for _, id := range []string{"42", "43"} {
		req := Request{Action: "job:Get", Resource: ARN{Tenant: "T", Group: "prod", Type: "job", ID: id}}
		want := DecisionDeny
		if id == "42" {
			want = DecisionAllow
		}
		if Evaluate(got, req) != want {
			t.Fatalf("intersection for %s: got %s, want %s", id, Evaluate(got, req), want)
		}
	}
}

func TestPolicyExpansionLimitsFailClosedWithoutPartialGrants(t *testing.T) {
	allow := Statement{Effect: EffectAllow, Action: []string{"*"}, Resource: []string{"orbitjob:*:*:*/*"}}
	large := Document{Statement: make([]Statement, maxPolicyAtoms+1)}
	for i := range large.Statement {
		large.Statement[i] = allow
	}
	if Permits([]Document{large}, []Document{{Statement: []Statement{allow}}}) {
		t.Fatal("oversized delegation was accepted without a bounded proof")
	}
	// Each input is small, but distributing their overlap would exceed the
	// output limit. Returning a partial intersection would silently alter grants.
	expand := Document{Statement: make([]Statement, 65)}
	for i := range expand.Statement {
		expand.Statement[i] = allow
	}
	if got := Intersect([]Document{expand}, expand); len(got) != 0 {
		t.Fatal("expansion limit returned a partial grant set")
	}
}

func TestPermissionSetAlgebraMatchesExhaustiveEvaluation(t *testing.T) {
	rng := rand.New(rand.NewSource(17))
	choices := [][]string{{"a", "b", "*"}, {"T", "U", "*"}, {"g", "h", "*"}, {"job", "check", "*"}, {"1", "2", "*"}}
	makeDoc := func() Document {
		d := Document{}
		for range 5 {
			v := make([]string, 5)
			for i := range v {
				v[i] = choices[i][rng.Intn(len(choices[i]))]
			}
			effect := EffectAllow
			if rng.Intn(3) == 0 {
				effect = EffectDeny
			}
			d.Statement = append(d.Statement, Statement{Effect: effect, Action: []string{v[0]}, Resource: []string{(ARN{Tenant: v[1], Group: v[2], Type: v[3], ID: v[4]}).String()}})
		}
		return d
	}
	for trial := range 80 {
		a, b := makeDoc(), makeDoc()
		intersection := Intersect([]Document{a}, b)
		isSubset := true
		for _, action := range []string{"a", "b", "other"} {
			for _, tenant := range []string{"T", "U", "other"} {
				for _, group := range []string{"g", "h", "other"} {
					for _, typ := range []string{"job", "check", "other"} {
						for _, id := range []string{"1", "2", "other"} {
							req := Request{Action: action, Resource: ARN{Tenant: tenant, Group: group, Type: typ, ID: id}}
							allowedA := Evaluate([]Document{a}, req) == DecisionAllow
							allowedB := Evaluate([]Document{b}, req) == DecisionAllow
							if allowedA && !allowedB {
								isSubset = false
							}
							if (Evaluate(intersection, req) == DecisionAllow) != (allowedA && allowedB) {
								t.Fatalf("trial %d intersection disagrees for %+v", trial, req)
							}
						}
					}
				}
			}
		}
		if Permits([]Document{a}, []Document{b}) != isSubset {
			t.Fatalf("trial %d subset disagrees with exhaustive authorization", trial)
		}
	}
}
