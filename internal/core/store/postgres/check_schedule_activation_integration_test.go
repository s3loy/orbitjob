//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"orbitjob/internal/core/app/checkschedule"
	"orbitjob/internal/core/app/projection"
	"orbitjob/internal/core/domain/check"
	"orbitjob/internal/core/domain/coordination"
	"orbitjob/internal/platform/scan"
)

type checkActivationFixture struct {
	owner, runtime *sql.DB
	checks         *CheckRepository
	control        *ControlPlaneRepository
	publisher      *checkPausePublisher
	tick           *checkschedule.TickUseCase
	scope          checkschedule.Scope
	check          check.Snapshot
	now            time.Time
}

func newCheckActivationFixture(t *testing.T) *checkActivationFixture {
	t.Helper()
	f := &checkActivationFixture{owner: OpenStoreDB(t), now: time.Now().UTC().Truncate(time.Microsecond)}
	f.scope = checkschedule.Scope{TenantID: scan.GenerateID(), Namespace: "checks"}
	if _, err := f.owner.ExecContext(t.Context(), `INSERT INTO tenants(id,slug,name) VALUES($1::char(26),$1::text,'Check activation')`, f.scope.TenantID); err != nil {
		t.Fatal(err)
	}
	f.runtime = openRoleDB(t, f.owner, "orbitjob_runtime")
	f.checks, f.control = NewCheckRepository(f.runtime), NewControlPlaneRepository(f.runtime)
	interval := 30
	due := f.now.Add(-time.Second)
	var err error
	f.check, err = NewCheckRepository(f.owner).Create(t.Context(), check.CreateSpec{
		Name: "activation", TenantID: f.scope.TenantID, CheckType: check.CheckTypeHTTPHealth,
		CheckConfig:    map[string]any{"url": "https://example.com/health"},
		AssertionRules: []check.AssertionRule{}, Labels: map[string]any{},
		ScheduleType: check.ScheduleTypeInterval, IntervalSec: &interval, Timezone: "UTC", TimeoutSec: 5,
		NextRunAt: &due,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.publisher = &checkPausePublisher{}
	f.tick = checkschedule.NewTickUseCase(f.checks, f.control, f.control, f.publisher)
	f.tick.Now = func() time.Time { return f.now }
	return f
}

func (f *checkActivationFixture) current(t *testing.T) check.Snapshot {
	t.Helper()
	c, err := f.checks.GetByID(t.Context(), f.scope.TenantID, f.check.ID)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (f *checkActivationFixture) requireRuns(t *testing.T, want int) {
	t.Helper()
	var runs, audits int
	if err := f.owner.QueryRowContext(t.Context(), `SELECT count(*) FROM job_run_control_plane WHERE tenant_id=$1`, f.scope.TenantID).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if err := f.owner.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND event_type=$2`, f.scope.TenantID, auditEventRunCreated).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if runs != want || audits != want {
		t.Fatalf("runs=%d creation audits=%d want=%d", runs, audits, want)
	}
}

func TestCheckScheduleFirstActivationSkipsStaleSlotAndExecutesNext(t *testing.T) {
	for _, resume := range []bool{false, true} {
		name := "new-check"
		if resume {
			name = "resumed-version"
		}
		t.Run(name, func(t *testing.T) {
			f := newCheckActivationFixture(t)
			if resume {
				if _, err := (projection.Service{Revisions: f.control}).ApplyCheck(t.Context(), f.check, f.scope.Namespace, f.scope.TenantID, checkschedule.ActorCheckScheduler); err != nil {
					t.Fatal(err)
				}
				for _, action := range []string{check.ActionPause, check.ActionResume} {
					var err error
					f.check, err = f.checks.ChangeStatus(t.Context(), f.scope.TenantID, "", f.check.ID, f.check.Version, action)
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			if created, err := f.tick.RunBatch(t.Context(), f.scope, 1); err != nil || created != 0 {
				t.Fatalf("first tick created=%d err=%v", created, err)
			}
			f.requireRuns(t, 0)
			if len(f.publisher.objects) != 0 {
				t.Fatal("stale slot published a JobRun")
			}
			current := f.current(t)
			if current.NextRunAt == nil || !current.NextRunAt.Equal(f.now.Add(30*time.Second)) {
				t.Fatalf("next cursor=%v", current.NextRunAt)
			}
			var revisionID, generation int64
			var activation time.Time
			if err := f.owner.QueryRowContext(t.Context(), `SELECT id,generation,activated_at FROM job_definition_revisions WHERE tenant_id=$1 AND is_active`, f.scope.TenantID).Scan(&revisionID, &generation, &activation); err != nil {
				t.Fatal(err)
			}
			if generation != int64(f.check.Version) || !activation.After(*f.check.NextRunAt) {
				t.Fatalf("generation=%d activation=%s old cursor=%s", generation, activation, *f.check.NextRunAt)
			}
			f.now = *current.NextRunAt
			if created, err := f.tick.RunBatch(t.Context(), f.scope, 1); err != nil || created != 1 {
				t.Fatalf("eligible tick created=%d err=%v", created, err)
			}
			f.requireRuns(t, 1)
			if len(f.publisher.objects) != 1 || f.publisher.objects[0].Spec.OccurrenceKey != coordination.OccurrenceKey(check.CheckSourceUID(f.check.ID), revisionID, f.now) {
				t.Fatalf("published occurrences=%+v", f.publisher.objects)
			}
			if created, err := f.tick.RunBatch(t.Context(), f.scope, 1); err != nil || created != 0 {
				t.Fatalf("repeated tick created=%d err=%v", created, err)
			}
			f.requireRuns(t, 1)
		})
	}
}

type failingCheckCursor struct {
	*CheckRepository
	fail bool
}

func (r *failingCheckCursor) UpdateNextRunAt(ctx context.Context, tenant string, id int64, version int, previous, next time.Time) (bool, error) {
	if r.fail {
		return false, errors.New("cursor unavailable")
	}
	return r.CheckRepository.UpdateNextRunAt(ctx, tenant, id, version, previous, next)
}

func TestCheckScheduleCursorFailuresPreserveSkipAndAcceptedIdentity(t *testing.T) {
	f := newCheckActivationFixture(t)
	cursor := &failingCheckCursor{CheckRepository: f.checks, fail: true}
	f.tick = checkschedule.NewTickUseCase(cursor, f.control, f.control, f.publisher)
	f.tick.Now = func() time.Time { return f.now }
	if _, err := f.tick.RunBatch(t.Context(), f.scope, 1); err != nil {
		t.Fatal(err)
	}
	f.requireRuns(t, 0)
	if current := f.current(t); !current.NextRunAt.Equal(*f.check.NextRunAt) || len(f.publisher.objects) != 0 {
		t.Fatal("failed stale-slot cursor write changed state")
	}
	cursor.fail = false
	if _, err := f.tick.RunBatch(t.Context(), f.scope, 1); err != nil {
		t.Fatal(err)
	}
	f.now = *f.current(t).NextRunAt
	cursor.fail = true
	if _, err := f.tick.RunBatch(t.Context(), f.scope, 1); err != nil {
		t.Fatal(err)
	}
	f.requireRuns(t, 1)
	if len(f.publisher.objects) != 1 {
		t.Fatalf("published=%d want one accepted occurrence", len(f.publisher.objects))
	}
	original := f.publisher.objects[0]
	cursor.fail = false
	if created, err := f.tick.RunBatch(t.Context(), f.scope, 1); err != nil || created != 0 {
		t.Fatalf("accepted replay created=%d err=%v", created, err)
	}
	f.requireRuns(t, 1)
	for _, object := range f.publisher.objects {
		if object.Name != original.Name || object.Spec.OccurrenceKey != original.Spec.OccurrenceKey {
			t.Fatal("cursor retry changed accepted occurrence identity")
		}
	}
	if current := f.current(t); current.NextRunAt == nil || !current.NextRunAt.After(f.now) {
		t.Fatal("accepted replay did not repair cursor")
	}
}

func TestCheckScheduleCursorCASPreservesConcurrentChanges(t *testing.T) {
	for _, change := range []struct{ name, update string }{
		{"version", "version=version+1"},
		{"cursor", "next_run_at=next_run_at+interval '2 minutes'"},
		{"status", "status='paused'"},
	} {
		t.Run(change.name, func(t *testing.T) {
			f := newCheckActivationFixture(t)
			f.runtime.SetMaxOpenConns(1)
			var waiterPID int
			if err := f.runtime.QueryRowContext(t.Context(), `SELECT pg_backend_pid()`).Scan(&waiterPID); err != nil {
				t.Fatal(err)
			}
			tx, err := f.owner.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback() }()
			if _, err := tx.ExecContext(t.Context(), `UPDATE checks SET `+change.update+` WHERE id=$1`, f.check.ID); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			advanced := make(chan bool, 1)
			go func() {
				updated, err := f.checks.UpdateNextRunAt(t.Context(), f.scope.TenantID, f.check.ID, f.check.Version, *f.check.NextRunAt, f.now.Add(time.Minute))
				advanced <- updated
				done <- err
			}()
			waitControlSessionLock(t, f.owner, waiterPID, done)
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if <-advanced {
				t.Fatal("old snapshot overwrote a committed concurrent change")
			}
			current := f.current(t)
			wantCursor := *f.check.NextRunAt
			if change.name == "cursor" {
				wantCursor = wantCursor.Add(2 * time.Minute)
			}
			if !current.NextRunAt.Equal(wantCursor) {
				t.Fatalf("cursor=%s want %s", current.NextRunAt, wantCursor)
			}
		})
	}
}
