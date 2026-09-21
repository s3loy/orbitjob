package command

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

// normalizeTriggerInput (trigger.go:129) is the write gate in front of the run
// ledger's actor column (job_run_control_plane.actor, VARCHAR(255) NOT NULL with
// a non-empty CHECK, db/migrations/0001_baseline.up.sql:275-283). These tests
// pin its bounds: what it refuses, and what it normalizes on the way through.

type boundsCase struct {
	name string
	in   TriggerInput

	// expectField, when non-empty, is the *validation.Error field the
	// normalizer must refuse with; the message must be non-empty.
	expectField string
	// expectScopeRefusal requires a *resource.ScopeError naming the resource
	// "job definition" and the caller's scope (resource.RequireUnscoped,
	// internal/domain/resource/errors.go:55).
	expectScopeRefusal bool
	// On acceptance the normalized output must match these exactly.
	wantTenant string
	wantActor  string
	wantIdem   string
}

// validTenant is a well-formed 26-character ULID, the shape of tenants.id.
const validTenant = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

func triggerBoundsCases() []boundsCase {
	return []boundsCase{
		{
			name:        "empty actor is refused",
			in:          TriggerInput{JobID: 7, TenantID: validTenant, ActorID: ""},
			expectField: "actor_id",
		},
		{
			name:        "whitespace-only actor is refused",
			in:          TriggerInput{JobID: 7, TenantID: validTenant, ActorID: " \t\n"},
			expectField: "actor_id",
		},
		{
			name:        "an actor wider than the ledger column is refused",
			in:          TriggerInput{JobID: 7, TenantID: validTenant, ActorID: strings.Repeat("a", 256)},
			expectField: "actor_id",
		},
		{
			name:       "an actor of exactly 255 characters is the accepted bound",
			in:         TriggerInput{JobID: 7, TenantID: validTenant, ActorID: strings.Repeat("a", 255)},
			wantTenant: validTenant,
			wantActor:  strings.Repeat("a", 255),
			wantIdem:   "",
		},
		{
			name:        "a tenant that is not 26 characters is refused",
			in:          TriggerInput{JobID: 7, TenantID: strings.Repeat("t", 27), ActorID: "key-9"},
			expectField: "tenant_id",
		},
		{
			name:       "a tenant of exactly 26 characters is the accepted bound",
			in:         TriggerInput{JobID: 7, TenantID: strings.Repeat("t", 26), ActorID: "key-9"},
			wantTenant: strings.Repeat("t", 26),
			wantActor:  "key-9",
			wantIdem:   "",
		},
		{
			name:        "job id zero is refused",
			in:          TriggerInput{JobID: 0, TenantID: validTenant, ActorID: "key-9"},
			expectField: "id",
		},
		{
			name:        "a negative job id is refused",
			in:          TriggerInput{JobID: -3, TenantID: validTenant, ActorID: "key-9"},
			expectField: "id",
		},
		{
			name:        "a blank tenant is refused; there is no default tenant",
			in:          TriggerInput{JobID: 7, TenantID: "", ActorID: "key-9"},
			expectField: "tenant_id",
		},
		{
			name:       "the tenant is trimmed",
			in:         TriggerInput{JobID: 7, TenantID: "  " + validTenant + "  ", ActorID: "key-9"},
			wantTenant: validTenant,
			wantActor:  "key-9",
			wantIdem:   "",
		},
		{
			name:       "the actor is trimmed",
			in:         TriggerInput{JobID: 7, TenantID: validTenant, ActorID: "  key-9  "},
			wantTenant: validTenant,
			wantActor:  "key-9",
			wantIdem:   "",
		},
		{
			name:       "the idempotency key is trimmed",
			in:         TriggerInput{JobID: 7, TenantID: validTenant, ActorID: "key-9", IdempotencyKey: "  idem-7  "},
			wantTenant: validTenant,
			wantActor:  "key-9",
			wantIdem:   "idem-7",
		},
		{
			name:               "a scoped resource group is refused",
			in:                 TriggerInput{JobID: 7, TenantID: validTenant, ActorID: "key-9", ResourceGroupID: "rg-7"},
			expectScopeRefusal: true,
		},
	}
}

type boundsViolation struct {
	caseName string
	problem  string
}

// triggerBoundsViolations runs every bounds case through the given normalizer
// and reports the cases whose behavior departs from the contract. It is shared
// by the shipped normalizer and by the mutant in the red proof below, so the
// red proof exercises the same assertions the real test runs.
func triggerBoundsViolations(normalize func(TriggerInput) (TriggerInput, error)) []boundsViolation {
	var violations []boundsViolation
	for _, tc := range triggerBoundsCases() {
		out, err := normalize(tc.in)
		switch {
		case tc.expectField != "":
			if err == nil {
				violations = append(violations, boundsViolation{tc.name,
					fmt.Sprintf("accepted %+v, want refusal on field %q", tc.in, tc.expectField)})
				continue
			}
			var verr *validation.Error
			if !errors.As(err, &verr) {
				violations = append(violations, boundsViolation{tc.name,
					fmt.Sprintf("refusal %v (%T) is not a *validation.Error", err, err)})
				continue
			}
			if verr.Field != tc.expectField {
				violations = append(violations, boundsViolation{tc.name,
					fmt.Sprintf("refusal names field %q, want %q", verr.Field, tc.expectField)})
			}
			if strings.TrimSpace(verr.Message) == "" {
				violations = append(violations, boundsViolation{tc.name, "refusal carries an empty message"})
			}
		case tc.expectScopeRefusal:
			var serr *resource.ScopeError
			if !errors.As(err, &serr) {
				violations = append(violations, boundsViolation{tc.name,
					fmt.Sprintf("scoped caller got %v (%T), want *resource.ScopeError", err, err)})
				continue
			}
			if serr.Resource != "job definition" || serr.Scope != tc.in.ResourceGroupID {
				violations = append(violations, boundsViolation{tc.name,
					fmt.Sprintf("scope refusal = %+v, want resource %q scope %q",
						serr, "job definition", tc.in.ResourceGroupID)})
			}
		default:
			if err != nil {
				violations = append(violations, boundsViolation{tc.name,
					fmt.Sprintf("refused with %v, want acceptance", err)})
				continue
			}
			if out.TenantID != tc.wantTenant || out.ActorID != tc.wantActor || out.IdempotencyKey != tc.wantIdem {
				violations = append(violations, boundsViolation{tc.name,
					fmt.Sprintf("normalized to tenant=%q actor=%q idempotency=%q, want tenant=%q actor=%q idempotency=%q",
						out.TenantID, out.ActorID, out.IdempotencyKey, tc.wantTenant, tc.wantActor, tc.wantIdem)})
			}
		}
	}
	return violations
}

func TestNormalizeTriggerInputBounds(t *testing.T) {
	if violations := triggerBoundsViolations(normalizeTriggerInput); len(violations) > 0 {
		var b strings.Builder
		for _, v := range violations {
			b.WriteString("\n  " + v.caseName + ": " + v.problem)
		}
		t.Fatalf("normalizer departs from the bounds contract:%s", b.String())
	}
}

// normalizeTriggerInputWithoutActorCheck is a copy of normalizeTriggerInput
// (trigger.go:129) with the actor block removed. It exists only to prove the
// actor assertions above can fail: if production ever loses that check, the
// shared case table must catch exactly the actor cases and nothing else.
func normalizeTriggerInputWithoutActorCheck(in TriggerInput) (TriggerInput, error) {
	if in.JobID < 1 {
		return TriggerInput{}, validation.New("id", "must be >= 1")
	}

	tenantID := strings.TrimSpace(in.TenantID)
	if len(tenantID) != 26 {
		return TriggerInput{}, validation.New("tenant_id", "must be a 26-character tenant id")
	}

	if err := resource.RequireUnscoped(in.ResourceGroupID, "job definition"); err != nil {
		return TriggerInput{}, err
	}

	return TriggerInput{
		JobID:           in.JobID,
		TenantID:        tenantID,
		ActorID:         strings.TrimSpace(in.ActorID),
		IdempotencyKey:  strings.TrimSpace(in.IdempotencyKey),
		ResourceGroupID: in.ResourceGroupID,
	}, nil
}

func TestNormalizeTriggerInputBoundsRedProof(t *testing.T) {
	actorCases := map[string]bool{
		"empty actor is refused":                           true,
		"whitespace-only actor is refused":                 true,
		"an actor wider than the ledger column is refused": true,
	}
	caught := map[string]string{}
	for _, v := range triggerBoundsViolations(normalizeTriggerInputWithoutActorCheck) {
		caught[v.caseName] = v.problem
	}
	for name := range actorCases {
		if caught[name] == "" {
			t.Fatalf("mutant without the actor check was not caught on %q: the assertion cannot fail", name)
		}
	}
	for name, problem := range caught {
		if !actorCases[name] {
			t.Fatalf("mutant was caught outside the actor cases, on %q: %s", name, problem)
		}
	}
}
