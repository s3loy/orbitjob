package command

import (
	"context"
	"errors"
	"testing"

	"orbitjob/internal/core/domain/apikey"
	"orbitjob/internal/core/domain/policy"
)

// policyStub serves canned documents by id.
type policyStub struct {
	docs map[string]policy.Document
	err  error
}

func (s policyStub) Get(_ context.Context, _, id string) (policy.Record, error) {
	if s.err != nil {
		return policy.Record{}, s.err
	}
	doc, ok := s.docs[id]
	if !ok {
		return policy.Record{}, errors.New("policy not found")
	}
	return policy.Record{ID: id, Document: doc}, nil
}

func allow(actions ...string) policy.Document {
	return policy.Document{Statement: []policy.Statement{
		{Effect: policy.EffectAllow, Action: actions, Resource: []string{"orbitjob:T1:*:job/*"}},
	}}
}

func TestCreateRejectsEscalation(t *testing.T) {
	repo := &stubAPIKeyCreator{}
	c := NewCreator(repo).WithPolicies(policyStub{docs: map[string]policy.Document{
		"p-admin": allow("job:Delete"),
	}})

	_, err := c.Create(context.Background(), CreateInput{
		TenantID:  "T1",
		PolicyIDs: []string{"p-admin"},
		Caller:    Caller{TenantID: "T1", Documents: []policy.Document{allow("job:Get")}},
	})
	if !errors.Is(err, policy.ErrPrivilegeEscalation) {
		t.Fatalf("expected ErrPrivilegeEscalation, got %v", err)
	}
	if repo.called {
		t.Fatal("nothing may be written when the grant is rejected")
	}
}

func TestCreateAllowsSubsetGrant(t *testing.T) {
	repo := &stubAPIKeyCreator{}
	c := NewCreator(repo).WithPolicies(policyStub{docs: map[string]policy.Document{
		"p-read": allow("job:Get"),
	}})

	if _, err := c.Create(context.Background(), CreateInput{
		TenantID:  "T1",
		PolicyIDs: []string{"p-read"},
		Caller:    Caller{TenantID: "T1", Documents: []policy.Document{allow("job:Get", "job:List")}},
	}); err != nil {
		t.Fatalf("a subset grant must succeed, got %v", err)
	}
	if !repo.called {
		t.Fatal("the key should have been written")
	}
}

func TestCreateInheritsCallerBoundary(t *testing.T) {
	repo := &stubAPIKeyCreator{}
	c := NewCreator(repo).WithPolicies(policyStub{docs: map[string]policy.Document{
		"p-read":     allow("job:Get"),
		"p-boundary": allow("job:Get"),
	}})

	if _, err := c.Create(context.Background(), CreateInput{
		TenantID:  "T1",
		PolicyIDs: []string{"p-read"},
		Caller: Caller{
			TenantID:         "T1",
			Documents:        []policy.Document{allow("job:Get")},
			BoundaryPolicyID: "p-boundary",
		},
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if repo.last.BoundaryPolicyID != "p-boundary" {
		t.Fatalf("the new key must inherit the caller's boundary, got %q", repo.last.BoundaryPolicyID)
	}
}

func TestCreateRejectsWiderBoundary(t *testing.T) {
	repo := &stubAPIKeyCreator{}
	c := NewCreator(repo).WithPolicies(policyStub{docs: map[string]policy.Document{
		"p-read":       allow("job:Get"),
		"p-caller-cap": allow("job:Get"),
		"p-wider-cap": {Statement: []policy.Statement{
			{Effect: policy.EffectAllow, Action: []string{"job:Get", "job:Delete"}, Resource: []string{"orbitjob:T1:*:job/*"}},
		}},
	}})

	_, err := c.Create(context.Background(), CreateInput{
		TenantID:         "T1",
		PolicyIDs:        []string{"p-read"},
		BoundaryPolicyID: "p-wider-cap",
		Caller: Caller{
			TenantID:         "T1",
			Documents:        []policy.Document{allow("job:Get")},
			BoundaryPolicyID: "p-caller-cap",
		},
	})
	if !errors.Is(err, policy.ErrPrivilegeEscalation) {
		t.Fatalf("a wider boundary must be rejected, got %v", err)
	}
}

func TestCreateWithoutPolicyReaderRejectsPolicies(t *testing.T) {
	c := NewCreator(&stubAPIKeyCreator{})
	_, err := c.Create(context.Background(), CreateInput{
		TenantID:  "T1",
		PolicyIDs: []string{"p-read"},
	})
	if err == nil {
		t.Fatal("requesting policies without a reader must fail, not silently drop them")
	}
}

func TestCreateWithNoPoliciesSucceeds(t *testing.T) {
	repo := &stubAPIKeyCreator{}
	c := NewCreator(repo)
	if _, err := c.Create(context.Background(), CreateInput{TenantID: "T1"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(repo.last.PolicyIDs) != 0 {
		t.Fatal("no policies were requested")
	}
}

func TestCreateSurfacesPolicyLoadFailure(t *testing.T) {
	c := NewCreator(&stubAPIKeyCreator{}).WithPolicies(policyStub{err: errors.New("db down")})
	_, err := c.Create(context.Background(), CreateInput{
		TenantID:  "T1",
		PolicyIDs: []string{"p-read"},
	})
	if err == nil {
		t.Fatal("a policy that cannot be loaded must fail the create")
	}
}

func TestCreateRecordsBinding(t *testing.T) {
	repo := &stubAPIKeyCreator{}
	c := NewCreator(repo).
		WithPolicies(policyStub{docs: map[string]policy.Document{"p-read": allow("job:Get")}}).
		WithGroups(groupStub{owned: map[string]bool{"g1": true}})
	if _, err := c.Create(context.Background(), CreateInput{
		TenantID:        "T1",
		ResourceGroupID: "g1",
		PolicyIDs:       []string{"p-read"},
		Caller:          Caller{TenantID: "T1", Documents: []policy.Document{allow("job:Get")}},
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	in := repo.last
	if in.TenantID != "T1" || in.ResourceGroupID != "g1" || len(in.PolicyIDs) != 1 {
		t.Fatalf("unexpected persist input %+v", in)
	}
	if in.BoundBy != "T1" {
		t.Fatalf("who bound the policy must be recorded, got %q", in.BoundBy)
	}
}

var _ = apikey.PersistInput{}

// groupStub reports which group ids the tenant owns.
type groupStub struct {
	owned map[string]bool
	err   error
}

func (g groupStub) Exists(_ context.Context, _, groupID string) (bool, error) {
	if g.err != nil {
		return false, g.err
	}
	return g.owned[groupID], nil
}

// A key scoped to a group the tenant does not own must be refused. The schema's
// foreign key only proves the group exists somewhere, so without this a tenant
// could scope a key to another tenant's group -- and then see nothing, because
// its own rows never carry that group.
func TestCreateRejectsAForeignResourceGroup(t *testing.T) {
	repo := &stubAPIKeyCreator{}
	c := NewCreator(repo).WithGroups(groupStub{})

	_, err := c.Create(context.Background(), CreateInput{
		TenantID:        "T1",
		ResourceGroupID: "someone-elses",
	})
	if err == nil {
		t.Fatal("a key was scoped to a group the tenant does not own")
	}
	if repo.called {
		t.Fatal("nothing may be written when the group check fails")
	}
}

// Without the lookup wired, naming a group fails closed rather than storing an
// unchecked id that would then filter every query the key makes.
func TestCreateRejectsAGroupWhenTheReaderIsMissing(t *testing.T) {
	_, err := NewCreator(&stubAPIKeyCreator{}).Create(context.Background(), CreateInput{
		TenantID:        "T1",
		ResourceGroupID: "g1",
	})
	if err == nil {
		t.Fatal("an unchecked group id was accepted")
	}
}

func TestCreateSurfacesAGroupLookupFailure(t *testing.T) {
	_, err := NewCreator(&stubAPIKeyCreator{}).
		WithGroups(groupStub{err: errors.New("db down")}).
		Create(context.Background(), CreateInput{
			TenantID:        "T1",
			ResourceGroupID: "g1",
		})
	if err == nil {
		t.Fatal("a failed group lookup must fail the create")
	}
}

// A caller with no group is not checked at all: there is nothing to look up.
func TestCreateSkipsTheGroupCheckWhenNoGroupIsNamed(t *testing.T) {
	repo := &stubAPIKeyCreator{}
	if _, err := NewCreator(repo).Create(context.Background(), CreateInput{
		TenantID: "T1",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !repo.called {
		t.Fatal("the key should have been written")
	}
}

// A key scoped to one resource group must stay inside it. Without this the
// route guard would allow the call -- the caller does hold apikey:Create where
// its policy says it does -- and the grant would be nominal: the caller could
// mint a key in another group, or one with no group at all, and act through it.
func TestCreateRefusesToLeaveTheCallersResourceGroup(t *testing.T) {
	cases := map[string]string{
		"a different group": "prod",
		"no group at all":   "",
	}
	for name, requested := range cases {
		t.Run(name, func(t *testing.T) {
			repo := &stubAPIKeyCreator{}
			c := NewCreator(repo).
				WithGroups(groupStub{owned: map[string]bool{"ci": true, "prod": true}})

			_, err := c.Create(context.Background(), CreateInput{
				TenantID:        "T1",
				ResourceGroupID: requested,
				Caller:          Caller{TenantID: "T1", ResourceGroupID: "ci"},
			})
			if err == nil {
				t.Fatal("a scoped caller minted a key outside its scope")
			}
			if !errors.Is(err, policy.ErrPrivilegeEscalation) {
				t.Fatalf("expected an escalation refusal, got %v", err)
			}
			if repo.called {
				t.Fatal("nothing may be written when the scope check fails")
			}
		})
	}
}

// Staying inside its scope is allowed: the caller's own group is the ceiling,
// not a prohibition.
func TestCreateAllowsTheCallersOwnResourceGroup(t *testing.T) {
	repo := &stubAPIKeyCreator{}
	c := NewCreator(repo).WithGroups(groupStub{owned: map[string]bool{"ci": true}})

	if _, err := c.Create(context.Background(), CreateInput{
		TenantID:        "T1",
		ResourceGroupID: "ci",
		Caller:          Caller{TenantID: "T1", ResourceGroupID: "ci"},
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !repo.called {
		t.Fatal("the key should have been written")
	}
}

// An unscoped caller is not restricted by this rule; it is already at the top
// of the group dimension and is bounded by its policies instead.
func TestCreateLetsAnUnscopedCallerChooseAGroup(t *testing.T) {
	repo := &stubAPIKeyCreator{}
	c := NewCreator(repo).WithGroups(groupStub{owned: map[string]bool{"ci": true}})

	if _, err := c.Create(context.Background(), CreateInput{
		TenantID:        "T1",
		ResourceGroupID: "ci",
		Caller:          Caller{TenantID: "T1"},
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !repo.called {
		t.Fatal("the key should have been written")
	}
}
