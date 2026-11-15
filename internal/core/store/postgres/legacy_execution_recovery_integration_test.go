//go:build integration

package postgres_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/workflow"
	store "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/scan"
)

func TestLegacyExecutionIsolationRetainsOnlyTrustedDeletionIdentity(t *testing.T) {
	for _, trusted := range []bool{true, false} {
		t.Run(fmt.Sprintf("trusted=%t", trusted), func(t *testing.T) {
			f := seedAdmissionWorkflow(t)
			step := f.step(t)
			repo := store.NewControlPlaneRepository(f.application)
			uid := scan.GenerateID()
			mark := repo.MarkConflictingUnrecordedExecutionUnknown
			if trusted {
				mark = repo.MarkUnrecordedExecutionUnknown
			}
			if err := mark(t.Context(), f.tenant, step.ID, uid); err != nil {
				t.Fatal(err)
			}
			if err := mark(t.Context(), f.tenant, step.ID, uid); err != nil {
				t.Fatalf("repeat observation: %v", err)
			}
			// A new repository represents restart. Only a validated original UID
			// may authorize delete retries; a diagnostic conflict UID never does.
			restarted := store.NewControlPlaneRepository(f.application)
			err := restarted.MarkUnrecordedExecutionUnknown(t.Context(), f.tenant, step.ID, uid)
			if (trusted && err != nil) || (!trusted && !errors.Is(err, store.ErrAttemptOwnership)) {
				t.Fatalf("restart deletion authority trusted=%t err=%v", trusted, err)
			}
			if err := restarted.MarkUnrecordedExecutionUnknown(t.Context(), f.tenant, step.ID, scan.GenerateID()); !errors.Is(err, store.ErrAttemptOwnership) {
				t.Fatalf("foreign UID gained authority: %v", err)
			}
			got, found, err := repo.RunByOccurrence(t.Context(), f.tenant, step.SourceUID, step.OccurrenceKey)
			if err != nil || !found || got.Phase != jobrun.CancelUnknown || got.Attempt != 0 {
				t.Fatalf("legacy gap was released: run=%+v found=%t err=%v", got, found, err)
			}
			var attempts, trustedAudits, conflictAudits int
			if err := f.owner.QueryRowContext(t.Context(), `SELECT count(*) FROM job_run_attempts_control_plane WHERE run_id=$1`, step.ID).Scan(&attempts); err != nil || attempts != 0 {
				t.Fatalf("legacy marker fabricated attempt: count=%d err=%v", attempts, err)
			}
			if err := f.owner.QueryRowContext(t.Context(), `SELECT count(*) FILTER (WHERE event_type='execution.legacy_unknown'),count(*) FILTER (WHERE event_type='execution.legacy_conflict') FROM audit_events WHERE tenant_id=$1 AND resource_type='job_run' AND resource_id=$2`, f.tenant, fmt.Sprint(step.ID)).Scan(&trustedAudits, &conflictAudits); err != nil {
				t.Fatal(err)
			}
			if (trusted && (trustedAudits != 1 || conflictAudits != 0)) || (!trusted && (trustedAudits != 0 || conflictAudits != 1)) {
				t.Fatalf("identity evidence changed or duplicated: trusted=%d conflict=%d", trustedAudits, conflictAudits)
			}
			parent, found, err := store.NewWorkflowRunRepository(f.application).RunByID(t.Context(), f.tenant, f.parent.ID)
			if err != nil || !found || parent.Phase != workflow.PhaseExecutionUnknown {
				t.Fatalf("legacy gap did not atomically isolate parent: parent=%+v found=%t err=%v", parent, found, err)
			}
			if _, _, err := store.NewWorkflowRunRepository(f.application).CreateStepForTenant(t.Context(), f.tenant, f.parent.ID, workflow.StepRun{
				SourceUID: f.source, RevisionID: f.revision, OccurrenceKey: strings.Repeat("d", 64), Trigger: jobrun.Workflow,
			}, 2, "review"); !errors.Is(err, store.ErrWorkflowBlocked) {
				t.Fatalf("legacy child allowed new sibling: %v", err)
			}
			if err := mark(t.Context(), scan.GenerateID(), step.ID, uid); err == nil {
				t.Fatal("foreign tenant wrote legacy isolation")
			}
		})
	}
}
