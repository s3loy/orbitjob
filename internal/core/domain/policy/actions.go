package policy

import (
	"fmt"
)

// KnownActions is every action the platform implements. A policy may only name
// actions from this set.
//
// The list is explicit rather than derived from the route table because the two
// serve different purposes: this is what a policy is allowed to say, the route
// table is what the server actually enforces. Keeping them separate lets a
// seeded preset reference an action the API has no route for yet, and lets the
// route table be checked against this list (TestEveryRouteUsesAKnownAction)
// rather than the other way round.
//
// Rejecting unknown actions at policy creation is what stops a typo from
// producing a policy that grants nothing: such a policy looks like it works,
// and the natural next move is to widen it until something happens.
var KnownActions = []string{
	// Tenant administration.
	"tenant:Create",
	"tenant:Get",
	"tenant:List",

	// API keys.
	"apikey:Create",
	"apikey:List",
	"apikey:Revoke",

	// Authorization documents and resource groups.
	"policy:Create",
	"policy:Delete",
	"policy:Get",
	"policy:List",
	"group:Create",
	"group:List",

	// Jobs.
	"job:Get",
	"job:List",
	"job:Trigger",

	// Instances.
	"instance:Cancel",
	"instance:Get",
	"instance:List",

	// Checks and their runs.
	"check:Create",
	"check:Delete",
	"check:Get",
	"check:List",
	"check:Pause",
	"check:Resume",
	"checkrun:Get",
	"checkrun:List",

	// Service level indicators and objectives.
	"sli:Create",
	"sli:Delete",
	"sli:Get",
	"sli:List",
	"slo:Create",
	"slo:Delete",
	"slo:Get",
	"slo:List",
	"slo:Pause",
	"slo:Resume",
	"slo:GetBudget",
	"slo:ListBudgets",
	"sloalert:Get",
	"sloalert:List",

	// Functions. The definition surface is read-only plus invoke in v1: a
	// function is a tenant-owned row read through its read paths and executed
	// by an invocation, which publishes a JobRun custom resource exactly as a
	// manual job trigger does. Create, update and delete routes would need
	// actions of their own and land together with those routes.
	"function:Get",
	"function:Invoke",
	"function:List",

	// Workflows. A WorkflowJob definition is declared as a Custom Resource and
	// read here through its projection, so like jobs the surface is read-only
	// plus one mutation verb: workflow:Trigger covers creating a manual run
	// (a WorkflowRun custom resource) and requesting a run's cancel (a
	// cancelRequested patch on the same kind of resource) -- both are CR
	// writes that ask the operator to do ledger work.
	"workflow:Get",
	"workflow:List",
	"workflow:Trigger",
}

var knownActionSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(KnownActions))
	for _, action := range KnownActions {
		set[action] = struct{}{}
	}
	return set
}()

// IsKnownAction reports whether an action is one the platform implements.
func IsKnownAction(action string) bool {
	_, ok := knownActionSet[action]
	return ok
}

// ValidateTenantDocument checks a policy body before a tenant stores it.
//
// It rejects three things ParseDocument cannot:
//
//   - an action no route implements, so a typo cannot produce a policy that
//     reads as a working grant and invites being widened until something
//     happens;
//   - the "*" wildcard, which is a platform-only capability: it is what makes
//     AdministratorAccess an administrator, and a tenant able to author it
//     could mint itself platform scope through the front door;
//   - a resource pattern naming a tenant other than "self". Every resolver
//     builds the tenant segment from the authenticated principal, so such a
//     pattern matches nothing today -- but it would silently become live if a
//     resolver were ever written to honour the caller's input, and a stored
//     document is not re-reviewed when that happens.
func ValidateTenantDocument(doc Document) error {
	for i, st := range doc.Statement {
		for _, action := range st.Action {
			if action == WildcardAction {
				return fmt.Errorf("statement %d: %q is reserved for platform policies", i, WildcardAction)
			}
			if !IsKnownAction(action) {
				return fmt.Errorf("statement %d: unknown action %q", i, action)
			}
		}
		for _, pattern := range st.Resource {
			arn, err := ParseARN(pattern)
			if err != nil {
				return fmt.Errorf("statement %d: %w", i, err)
			}
			if arn.Tenant != SelfToken {
				return fmt.Errorf(
					"statement %d: resource %q must name the %q tenant, got %q",
					i, pattern, SelfToken, arn.Tenant)
			}
		}
	}
	return nil
}
