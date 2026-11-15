package policy

import (
	"fmt"
	"slices"
	"testing"
)

func tenantScopeDocument(effect Effect, resources ...string) Document {
	return Document{Statement: []Statement{{
		Effect: effect, Action: []string{"tenant:Get"}, Resource: resources,
	}}}
}

func tenantScopeAllows(scope TenantScope, tenant string) bool {
	if scope.All {
		return !slices.Contains(scope.Exclude, tenant)
	}
	return slices.Contains(scope.Include, tenant)
}

func checkTenantScopeEquivalent(t *testing.T, docs []Document) {
	t.Helper()
	scope := TenantReadScope(docs)
	if (scope.All && len(scope.Include) != 0) || (!scope.All && len(scope.Exclude) != 0) {
		t.Fatalf("scope is not normalized: %+v", scope)
	}
	if !slices.IsSorted(scope.Include) || !slices.IsSorted(scope.Exclude) {
		t.Fatalf("scope is not deterministic: %+v", scope)
	}
	// These are symbolic ARN literals, not stored tenant fixtures. The fresh
	// value also checks the complement outside all literal matching points.
	for _, tenant := range []string{"T1", "T2", "T3", "self", "tenant-*", "outside-policy-literals"} {
		want := Evaluate(docs, Request{
			Action: "tenant:Get", Resource: ARN{Tenant: tenant, Group: "*", Type: "tenant", ID: tenant},
		}) == DecisionAllow
		if got := tenantScopeAllows(scope, tenant); got != want {
			t.Fatalf("tenant %q scope %+v: got %v, Evaluate allowed %v", tenant, scope, got, want)
		}
	}
}

func TestTenantReadScope(t *testing.T) {
	allowAll := tenantScopeDocument(EffectAllow, "orbitjob:*:*:tenant/*")
	allowT1 := tenantScopeDocument(EffectAllow, "orbitjob:T1:*:tenant/*")
	denyT1 := tenantScopeDocument(EffectDeny, "orbitjob:*:*:tenant/T1")
	allowT2 := tenantScopeDocument(EffectAllow, "orbitjob:T2:*:tenant/T2")
	denyAll := tenantScopeDocument(EffectDeny, "orbitjob:*:*:tenant/*")
	wrapperWithStatement := Intersect([]Document{denyT1}, allowT2)[0]
	wrapperWithStatement.Statement = allowAll.Statement
	cases := []struct {
		name string
		docs []Document
		want TenantScope
	}{
		{"empty", nil, TenantScope{}},
		{"global allow", []Document{allowAll}, TenantScope{All: true}},
		{"finite union", []Document{allowT2, allowT1, allowT1}, TenantScope{Include: []string{"T1", "T2"}}},
		{"finite deny", []Document{allowAll, denyT1}, TenantScope{All: true, Exclude: []string{"T1"}}},
		{"deny all", []Document{allowT1, denyAll}, TenantScope{}},
		{"tenant and ID literals must agree", []Document{tenantScopeDocument(EffectAllow, "orbitjob:T1:*:tenant/T2")}, TenantScope{}},
		{"ID alone narrows tenant", []Document{tenantScopeDocument(EffectAllow, "orbitjob:*:*:tenant/T2")}, TenantScope{Include: []string{"T2"}}},
		{"empty group compatibility", []Document{tenantScopeDocument(EffectAllow, "orbitjob:*::tenant/*")}, TenantScope{All: true}},
		{"literal group cannot match metadata", []Document{tenantScopeDocument(EffectAllow, "orbitjob:*:prod:tenant/*")}, TenantScope{}},
		{"wrong resource type", []Document{tenantScopeDocument(EffectAllow, "orbitjob:*:*:job/*")}, TenantScope{}},
		{"wildcard resource type", []Document{tenantScopeDocument(EffectAllow, "orbitjob:*:*:*/*")}, TenantScope{All: true}},
		{"partial wildcard is literal", []Document{tenantScopeDocument(EffectAllow, "orbitjob:tenant-*:*:tenant/*")}, TenantScope{Include: []string{"tenant-*"}}},
		{"malformed resources ignored", []Document{tenantScopeDocument(EffectAllow, "invalid", "orbitjob:T1:*:tenant/*")}, TenantScope{Include: []string{"T1"}}},
		{"finite boundary", Intersect([]Document{allowAll}, allowT1), TenantScope{Include: []string{"T1"}}},
		{"no grants", Intersect(nil, allowAll), TenantScope{}},
		{"disjoint boundaries", Intersect(Intersect([]Document{allowAll}, allowT1), allowT2), TenantScope{}},
		{"boundary deny remains global", append(Intersect([]Document{denyT1}, allowT2), allowAll), TenantScope{All: true, Exclude: []string{"T1"}}},
		{"deny in nested boundary", append(Intersect([]Document{allowT2}, Intersect([]Document{denyT1}, allowT2)[0]), allowAll), TenantScope{All: true, Exclude: []string{"T1"}}},
		{"wrapper statements preserve nested deny", []Document{wrapperWithStatement}, TenantScope{All: true, Exclude: []string{"T1"}}},
		{"boundary does not grant alone", Intersect(nil, denyAll), TenantScope{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := TenantReadScope(tc.docs)
			if got.All != tc.want.All || !slices.Equal(got.Include, tc.want.Include) || !slices.Equal(got.Exclude, tc.want.Exclude) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
			checkTenantScopeEquivalent(t, tc.docs)
		})
	}
}

func TestTenantReadScopeActionsAndResolvedSelf(t *testing.T) {
	for _, action := range []string{"tenant:Get", WildcardAction, "tenant:List", "tenant:*", "Tenant:Get"} {
		doc := tenantScopeDocument(EffectAllow, "orbitjob:*:*:tenant/*")
		doc.Statement[0].Action = []string{action}
		scope := TenantReadScope([]Document{doc})
		if scope.All != (action == "tenant:Get" || action == WildcardAction) {
			t.Fatalf("action %q produced %+v", action, scope)
		}
		checkTenantScopeEquivalent(t, []Document{doc})
	}
	self := tenantScopeDocument(EffectAllow, "orbitjob:self:*:tenant/*")
	effective := Intersect(Intersect([]Document{self}, self), self)
	for _, home := range []string{"T1", "T2"} {
		resolved := []Document{ResolveSelf(effective[0], home)}
		if got := TenantReadScope(resolved); got.All || !slices.Equal(got.Include, []string{home}) {
			t.Fatalf("resolved home %s produced %+v", home, got)
		}
		checkTenantScopeEquivalent(t, resolved)
	}
	if self.Statement[0].Resource[0] != "orbitjob:self:*:tenant/*" {
		t.Fatal("scope planning or self resolution mutated the shared preset")
	}
}

func TestTenantReadScopeMatchesEvaluateForComposedDocuments(t *testing.T) {
	resources := []string{
		"orbitjob:*:*:tenant/*", "orbitjob:T1:*:tenant/*", "orbitjob:*:*:tenant/T1",
		"orbitjob:T2:*:tenant/T2", "orbitjob:T1:*:tenant/T2", "orbitjob:T3::tenant/*",
		"orbitjob:*::tenant/*", "orbitjob:*:prod:tenant/*", "orbitjob:*:*:*/*",
		"orbitjob:*:*:job/*", "orbitjob:tenant-*:*:tenant/*", "invalid",
	}
	atoms := []Document{{}}
	for _, effect := range []Effect{EffectAllow, EffectDeny} {
		for _, resource := range resources {
			atoms = append(atoms, tenantScopeDocument(effect, resource))
		}
	}
	all := tenantScopeDocument(EffectAllow, "orbitjob:*:*:tenant/*")
	for i, grant := range atoms {
		for j, boundary := range atoms {
			third := atoms[(i+j)%len(atoms)]
			variants := [][]Document{
				{grant, boundary, third},
				Intersect([]Document{grant}, boundary),
				append(Intersect([]Document{grant}, boundary), all),
				Intersect(Intersect([]Document{grant}, boundary), third),
				append(Intersect([]Document{grant}, Intersect([]Document{boundary}, third)[0]), all),
			}
			for k, docs := range variants {
				t.Run(fmt.Sprintf("grant-%d/boundary-%d/variant-%d", i, j, k), func(t *testing.T) {
					checkTenantScopeEquivalent(t, docs)
				})
			}
		}
	}
}
