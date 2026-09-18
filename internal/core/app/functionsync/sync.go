// Package functionsync materializes function definitions as immutable
// definition revisions. A function is a tenant-owned row, but an invocation is
// CR-first and the CRD requires a definition revision id before anything can
// be published — so the row's executable form has to exist before the first
// invoke arrives. This loop is what keeps the two in step: each tick it
// projects one revision per active function row (source_uid function-<id>,
// generation = the row's version), idempotently.
//
// The gap it manages is a bounded one-tick delay by design: between saving a
// definition and the next tick its previous revision stays active and an
// invocation in that window pins it — invoke-what-you-read, the same
// generation lag every CR-first path exposes.
package functionsync

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"orbitjob/internal/core/app/projection"
	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/core/domain/revision"
)

// ActorFunctionSync is the identity recorded on every revision this loop
// projects. The loop is definition-level bookkeeping, not run firing, so it
// shares the operator's identity rather than minting a scheduler-shaped one.
const ActorFunctionSync = "orbitjob-operator"

// Scope is one tenant and the namespace its function invocations publish
// into. The JobRun custom resource and the Kubernetes Job both live there,
// which the tenancy mapping must resolve back to the same tenant — the same
// derivation the check scheduler's scopes use.
type Scope struct {
	TenantID  string
	Namespace string
}

// FunctionSource reads a tenant's active, live function definitions. It is
// the FunctionStore contract's ActiveForTenant.
type FunctionSource interface {
	ActiveForTenant(ctx context.Context, tenantID string) ([]function.Definition, error)
}

// RevisionWriter materializes immutable definition revisions.
type RevisionWriter interface {
	ApplyRevisionForTenant(ctx context.Context, tenantID string, rev revision.Revision) (int64, error)
}

// Sync projects one revision per active function row per tick.
type Sync struct {
	Functions FunctionSource
	Revisions RevisionWriter
	// Now supplies the revision creation time. It defaults to time.Now; tests
	// inject a fixed clock to assert revision identity is stable.
	Now func() time.Time
}

// RunTenant syncs one tenant scope and reports how many active functions it
// projected. A definition that fails is logged and skipped so one bad row
// cannot stall the tenant; only a failure to list functions aborts the tick.
//
// A clean tick is deliberately silent at the caller's level: the steady state
// is an idempotent no-op for every definition (the store records nothing for
// a replayed revision), and logging it on the schedule interval would drown
// the signals around it.
func (s *Sync) RunTenant(ctx context.Context, scope Scope) (int, error) {
	if s.Functions == nil || s.Revisions == nil {
		return 0, fmt.Errorf("function sync dependencies are required")
	}
	defs, err := s.Functions.ActiveForTenant(ctx, scope.TenantID)
	if err != nil {
		return 0, fmt.Errorf("list active functions for tenant %s: %w", scope.TenantID, err)
	}
	projector := projection.Service{Revisions: s.Revisions, Now: s.Now}
	synced := 0
	for _, def := range defs {
		if _, err := projector.ApplyFunction(ctx, def, scope.Namespace, scope.TenantID, ActorFunctionSync); err != nil {
			slog.Error("syncing function revision failed; skipping it",
				"tenant", scope.TenantID,
				"function_id", def.ID,
				"source_uid", function.SourceUID(def.ID),
				"error", err,
			)
			continue
		}
		synced++
	}
	return synced, nil
}
