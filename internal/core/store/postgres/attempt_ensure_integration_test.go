//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/platform/scan"
)

func TestAttemptEnsureIsSerializedAgainstCancellation(t *testing.T) {
	db, tenant, id := seedIntegrityRun(t, jobrun.Pending, 0)
	repo := NewControlPlaneRepository(db)
	ctx := ctxStore(t)
	reserved := jobrun.Attempt{Number: 1, KubernetesJobName: "reserved-job", Phase: jobrun.CreatingAttempt}
	entered, release := make(chan struct{}), make(chan struct{})
	ensured := make(chan error, 1)
	go func() {
		ensured <- repo.CreateAttemptWithEnsure(ctx, tenant, id, reserved, func(ctx context.Context) (jobrun.Attempt, error) {
			if _, ok := ctx.Deadline(); !ok {
				return jobrun.Attempt{}, errors.New("Ensure context is unbounded")
			}
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return jobrun.Attempt{}, ctx.Err()
			}
			return jobrun.Attempt{Number: 1, KubernetesJobName: "reserved-job", KubernetesJobUID: "uid", Phase: jobrun.Running}, nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("Ensure never started")
	}
	var phase, uid string
	if err := db.QueryRowContext(ctx, `SELECT r.phase,COALESCE(a.kubernetes_job_uid,'') FROM job_run_control_plane r JOIN job_run_attempts_control_plane a ON a.run_id=r.id WHERE r.id=$1`, id).Scan(&phase, &uid); err != nil {
		t.Fatal(err)
	}
	if phase != "CreatingAttempt" || uid != "" {
		t.Fatalf("reservation phase=%s uid=%s", phase, uid)
	}
	cancelResult := make(chan error, 1)
	go func() { _, err := repo.UpdateRunPhase(ctx, tenant, id, jobrun.CancelRequested, 1); cancelResult <- err }()
	select {
	case err := <-cancelResult:
		t.Fatalf("cancel crossed active Ensure lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-ensured; err != nil {
		t.Fatal(err)
	}
	if err := <-cancelResult; err != nil {
		t.Fatal(err)
	}
	stored, found, err := repo.RunByOccurrence(ctx, tenant, tenant, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err != nil || !found || stored.AttemptUID != "uid" || stored.Phase != jobrun.CancelRequested {
		t.Fatalf("stored=%+v found=%v err=%v", stored, found, err)
	}
}

func TestUncertainEnsureRequiresObservationWithoutAnotherDispatch(t *testing.T) {
	db, tenant, id := seedIntegrityRun(t, jobrun.Pending, 0)
	repo := NewControlPlaneRepository(db)
	ctx := ctxStore(t)
	reserved := jobrun.Attempt{Number: 1, KubernetesJobName: "uncertain-job", Phase: jobrun.CreatingAttempt}
	uncertain := errors.New("transport outcome unknown")
	if err := repo.CreateAttemptWithEnsure(ctx, tenant, id, reserved, func(context.Context) (jobrun.Attempt, error) { return jobrun.Attempt{}, uncertain }); !errors.Is(err, uncertain) {
		t.Fatalf("uncertain error=%v", err)
	}
	var phase string
	var count int
	if err := db.QueryRowContext(ctx, `SELECT phase,attempt FROM job_run_control_plane WHERE id=$1`, id).Scan(&phase, &count); err != nil {
		t.Fatal(err)
	}
	if phase != "CreatingAttempt" || count != 1 {
		t.Fatalf("intent lost: %s attempt=%d", phase, count)
	}
	if err := repo.CreateAttemptWithEnsure(ctx, tenant, id, reserved, func(context.Context) (jobrun.Attempt, error) {
		t.Error("possibly sent attempt dispatched again")
		return reserved, nil
	}); !errors.Is(err, jobrun.ErrDispatchMayHaveStarted) {
		t.Fatalf("replay must observe, got %v", err)
	}
	if err := repo.BindAttemptObservation(ctx, tenant, id, 1, reserved.KubernetesJobName, "confirmed-uid", "9"); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateAttemptWithEnsure(ctx, tenant, id, reserved, func(context.Context) (jobrun.Attempt, error) {
		t.Error("confirmed attempt ensured twice")
		return reserved, nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDispatchMarkerIsCommittedBeforeExternalCallAndNeverRegresses(t *testing.T) {
	db, tenant, id := seedIntegrityRun(t, jobrun.Pending, 0)
	repo := NewControlPlaneRepository(db)
	ctx := ctxStore(t)
	intent := jobrun.Attempt{Number: 1, KubernetesJobName: "marker-before-send", OwnerRunUID: scan.GenerateID()}
	wantErr := errors.New("create acknowledgement lost")
	if err := repo.CreateAttemptWithEnsure(ctx, tenant, id, intent, func(context.Context) (jobrun.Attempt, error) {
		// A separate connection must see the marker while the callback's binding
		// transaction remains open. An uncommitted marker would read Prepared.
		var stage string
		if err := db.QueryRowContext(ctx, `SELECT dispatch_stage FROM job_run_attempts_control_plane WHERE run_id=$1`, id).Scan(&stage); err != nil {
			t.Fatal(err)
		}
		if stage != string(jobrun.DispatchMayHaveStarted) {
			t.Fatalf("external call preceded marker commit: %s", stage)
		}
		return jobrun.Attempt{}, wantErr
	}); !errors.Is(err, wantErr) {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE job_run_attempts_control_plane SET dispatch_stage='Prepared' WHERE run_id=$1`, id); err == nil {
		t.Fatal("possibly sent dispatch returned to Prepared")
	}
	e, found, err := repo.CurrentExecution(ctx, tenant, id)
	if err != nil || !found || e.DispatchStage != jobrun.DispatchMayHaveStarted || e.Run.OwnerRunUID != intent.OwnerRunUID {
		t.Fatalf("durable marker lost: %+v found=%t err=%v", e, found, err)
	}
	if err := repo.MarkExecutionUnknown(ctx, tenant, e, jobrun.ReasonJobNotFound); err != nil {
		t.Fatal(err)
	}
	unknown, found, err := repo.CurrentExecution(ctx, tenant, id)
	if err != nil || !found || unknown.Run.Phase != jobrun.ExecutionUnknown || unknown.DispatchStage != jobrun.DispatchMayHaveStarted {
		t.Fatalf("unbound absence did not retain unknown: %+v found=%t err=%v", unknown, found, err)
	}
	lateUID := scan.GenerateID()
	if err := repo.BindAttemptObservation(ctx, tenant, id, 1, intent.KubernetesJobName, lateUID, "10"); err != nil {
		t.Fatal(err)
	}
	bound, found, err := repo.CurrentExecution(ctx, tenant, id)
	if err != nil || !found || bound.DispatchStage != jobrun.DispatchBound || bound.Run.AttemptUID != lateUID || bound.Run.Phase != jobrun.ExecutionUnknown || bound.Run.UnknownSince != nil {
		t.Fatalf("late observation changed execution identity or phase: %+v found=%t err=%v", bound, found, err)
	}
}

func TestOnlyPreparedCancellationProvesNoDispatch(t *testing.T) {
	for _, stage := range []jobrun.DispatchStage{jobrun.DispatchPrepared, jobrun.DispatchMayHaveStarted} {
		t.Run(string(stage), func(t *testing.T) {
			db, tenant, id := seedIntegrityRun(t, jobrun.Pending, 0)
			repo := NewControlPlaneRepository(db)
			ctx := ctxStore(t)
			if err := repo.createAttemptForTenant(ctx, tenant, id, jobrun.Attempt{Number: 1, KubernetesJobName: "prepared-cancel", Phase: jobrun.CreatingAttempt}, jobrun.CreatingAttempt, true); err != nil {
				t.Fatal(err)
			}
			if stage == jobrun.DispatchMayHaveStarted {
				if _, err := db.ExecContext(ctx, `UPDATE job_run_attempts_control_plane SET dispatch_stage=$1 WHERE run_id=$2`, stage, id); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := repo.UpdateRunPhase(ctx, tenant, id, jobrun.CancelRequested, 1); err != nil {
				t.Fatal(err)
			}
			e, found, err := repo.CurrentExecution(ctx, tenant, id)
			if err != nil || !found {
				t.Fatal(err)
			}
			changed, err := repo.CancelPreparedAttempt(ctx, tenant, e)
			if stage == jobrun.DispatchMayHaveStarted {
				if changed || !errors.Is(err, ErrRunChanged) {
					t.Fatalf("possible late dispatch confirmed canceled: changed=%t err=%v", changed, err)
				}
			} else if !changed || err != nil {
				t.Fatalf("Prepared did not cancel: changed=%t err=%v", changed, err)
			}
		})
	}
}

func TestCanceledRunDoesNotInvokeEnsure(t *testing.T) {
	db, tenant, id := seedIntegrityRun(t, jobrun.CancelRequested, 0)
	repo := NewControlPlaneRepository(db)
	err := repo.CreateAttemptWithEnsure(ctxStore(t), tenant, id, jobrun.Attempt{Number: 1, KubernetesJobName: "never-create"}, func(context.Context) (jobrun.Attempt, error) {
		t.Error("canceled run invoked Ensure")
		return jobrun.Attempt{}, nil
	})
	if !errors.Is(err, ErrRunChanged) {
		t.Fatalf("error=%v", err)
	}
}

func TestUncertainAttemptCanLearnIdentityDuringCancellation(t *testing.T) {
	db, tenant, id := seedIntegrityRun(t, jobrun.Pending, 0)
	ctx := ctxStore(t)
	repo := NewControlPlaneRepository(db)
	intent := jobrun.Attempt{Number: 1, KubernetesJobName: "late-job"}
	_ = repo.CreateAttemptWithEnsure(ctx, tenant, id, intent, func(context.Context) (jobrun.Attempt, error) {
		return jobrun.Attempt{}, errors.New("unknown create result")
	})
	if changed, err := repo.UpdateRunPhase(ctx, tenant, id, jobrun.CancelRequested, 1); err != nil || !changed {
		t.Fatalf("request cancel changed=%v err=%v", changed, err)
	}
	if err := repo.BindAttemptObservation(ctx, tenant, id, 1, "late-job", "late-uid", "9"); err != nil {
		t.Fatal(err)
	}
	if err := repo.BindAttemptObservation(ctx, tenant, id, 1, "late-job", "replacement-uid", "10"); !errors.Is(err, ErrAttemptOwnership) {
		t.Fatalf("replaced UID: %v", err)
	}
	stored, found, err := repo.RunByOccurrence(ctx, tenant, tenant, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err != nil || !found || stored.Phase != jobrun.CancelRequested || stored.AttemptUID != "late-uid" {
		t.Fatalf("identity binding changed run phase: %+v found=%v err=%v", stored, found, err)
	}
}
