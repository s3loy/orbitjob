package policy_test

import (
	"strings"
	"testing"

	"orbitjob/internal/core/domain/policy"
)

func docWith(actions ...string) policy.Document {
	return policy.Document{
		Version: "1",
		Statement: []policy.Statement{{
			Effect:   policy.EffectAllow,
			Action:   actions,
			Resource: []string{"orbitjob:self:*:job/*"},
		}},
	}
}

func TestValidateTenantDocumentAcceptsKnownActions(t *testing.T) {
	if err := policy.ValidateTenantDocument(docWith("job:Trigger", "job:List")); err != nil {
		t.Fatalf("known actions rejected: %v", err)
	}
}

// A typo must fail loudly at creation. A policy that silently grants nothing
// looks functional, and the next step is to widen it until it does something.
func TestValidateTenantDocumentRejectsUnknownAction(t *testing.T) {
	err := policy.ValidateTenantDocument(docWith("job:Creat"))
	if err == nil {
		t.Fatal("a misspelled action was accepted")
	}
	if !strings.Contains(err.Error(), "job:Creat") {
		t.Fatalf("error should name the offending action, got %v", err)
	}
}

func TestValidateTenantDocumentRejectsWildcard(t *testing.T) {
	err := policy.ValidateTenantDocument(docWith(policy.WildcardAction))
	if err == nil {
		t.Fatal("a tenant-authored policy was allowed to grant every action")
	}
}

// Several actions in one statement are all checked, not just the first.
func TestValidateTenantDocumentChecksEveryActionInAStatement(t *testing.T) {
	if err := policy.ValidateTenantDocument(docWith("job:Trigger", "job:Destroy")); err == nil {
		t.Fatal("an unknown action after a valid one was accepted")
	}
}

func TestIsKnownAction(t *testing.T) {
	if !policy.IsKnownAction("job:Trigger") {
		t.Fatal("job:Trigger should be known")
	}
	if policy.IsKnownAction("job:Destroy") {
		t.Fatal("job:Destroy should not be known")
	}
}

// The registry is the contract between policy documents and the routes that
// enforce them; an empty or duplicated entry means the check is not a check.
func TestKnownActionsAreUniqueAndNonEmpty(t *testing.T) {
	seen := make(map[string]bool, len(policy.KnownActions))
	for _, action := range policy.KnownActions {
		if action == "" {
			t.Fatal("registry contains an empty action")
		}
		if !strings.Contains(action, ":") {
			t.Fatalf("action %q is not in resource:Verb form", action)
		}
		if seen[action] {
			t.Fatalf("action %q appears twice", action)
		}
		seen[action] = true
	}
}

// A tenant-authored resource pattern must name the "self" tenant. Today such a
// pattern matches nothing, because every resolver builds the tenant segment
// from the authenticated principal -- but a stored document is not re-reviewed
// when a resolver changes, so the check belongs at authoring time.
func TestValidateTenantDocumentRejectsAForeignTenantSegment(t *testing.T) {
	doc := policy.Document{
		Version: "1",
		Statement: []policy.Statement{{
			Effect:   policy.EffectAllow,
			Action:   []string{"job:Trigger"},
			Resource: []string{"orbitjob:another-tenant:*:job/*"},
		}},
	}
	err := policy.ValidateTenantDocument(doc)
	if err == nil {
		t.Fatal("a policy naming another tenant was accepted")
	}
	if !strings.Contains(err.Error(), "self") {
		t.Fatalf("error should name the expected segment, got %v", err)
	}
}

func TestValidateTenantDocumentAcceptsTheSelfTenant(t *testing.T) {
	if err := policy.ValidateTenantDocument(docWith("job:Trigger")); err != nil {
		t.Fatalf("a self-scoped policy was rejected: %v", err)
	}
}
