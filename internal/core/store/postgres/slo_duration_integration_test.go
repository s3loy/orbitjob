//go:build integration

package postgres

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"orbitjob/internal/core/domain/audit"
	"orbitjob/internal/core/domain/slo"
)

func TestSLODurationPersistenceAndLegacyManagement(t *testing.T) {
	f := newMonitoringAuditFixture(t, "api_key")
	ctx := audit.WithActor(t.Context(), f.actorID, "api_key")
	for _, duration := range []time.Duration{time.Second, 2 * time.Second, slo.MaxWindowDuration} {
		t.Run("exact "+duration.String(), func(t *testing.T) {
			spec := f.sloSpec("exact-" + duration.String())
			spec.WindowDuration = duration
			created, err := f.slos.Create(ctx, f.tenantID, spec)
			if err != nil {
				t.Fatal(err)
			}
			var seconds int64
			if err := f.owner.QueryRowContext(ctx, `SELECT window_duration FROM slos WHERE tenant_id=$1 AND id=$2`, f.tenantID, created.ID).Scan(&seconds); err != nil {
				t.Fatal(err)
			}
			if created.WindowDuration != duration || seconds != int64(duration/time.Second) {
				t.Fatalf("duration = %s, stored seconds = %d, want %s", created.WindowDuration, seconds, duration)
			}
		})
	}
	for _, duration := range []time.Duration{0, time.Millisecond, 1500 * time.Millisecond, slo.MaxWindowDuration + time.Second} {
		t.Run("reject "+duration.String(), func(t *testing.T) {
			before := f.state(t)
			spec := f.sloSpec(fmt.Sprintf("invalid-%d", duration))
			spec.WindowDuration = duration
			if _, err := f.slos.Create(ctx, f.tenantID, spec); err == nil {
				t.Fatal("invalid duration was accepted")
			}
			if !reflect.DeepEqual(before, f.state(t)) {
				t.Fatal("rejected duration changed domain or audit state")
			}
		})
	}

	// Reproduce a retained row written before creation enforced whole seconds.
	if _, err := f.owner.ExecContext(ctx, `UPDATE slos SET window_duration=0 WHERE tenant_id=$1 AND id=$2`, f.tenantID, f.slo.ID); err != nil {
		t.Fatal(err)
	}
	paused, err := f.slos.ChangeStatus(ctx, f.tenantID, f.group, f.slo.ID, f.slo.Version, slo.StatusPaused)
	if err != nil {
		t.Fatal(err)
	}
	if paused.Status != slo.StatusPaused || paused.WindowDuration != 0 {
		t.Fatalf("legacy pause changed duration or status: %+v", paused)
	}
	if err := f.slos.Delete(ctx, f.tenantID, f.group, paused.ID, paused.Version); err != nil {
		t.Fatal(err)
	}
	var deleted bool
	if err := f.owner.QueryRowContext(ctx, `SELECT deleted_at IS NOT NULL FROM slos WHERE tenant_id=$1 AND id=$2`, f.tenantID, paused.ID).Scan(&deleted); err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("legacy zero-window SLO was not soft-deleted")
	}
}
