//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"orbitjob/internal/core/domain/jobrun"
	store "orbitjob/internal/core/store/postgres"
)

func TestAttemptReservationWaitsForRunBeforeLockingExistingAttempt(t *testing.T) {
	f, _, e := seedObservedExecution(t)
	// A parent lock would serialize both operations first and hide the inverse
	// run/attempt lock order that ordinary standalone runs can encounter.
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_run_control_plane SET workflow_run_id=NULL WHERE id=$1`, e.Run.ID); err != nil {
		t.Fatal(err)
	}
	holderDB, holderPID := admissionConnection(t, f)
	reservationDB, reservationPID := admissionConnection(t, f)
	if holderPID == reservationPID {
		t.Fatal("concurrent operations reused one database session")
	}
	// This case isolates row-lock order. Admission tests separately challenge
	// Repeatable Read defaults; a lock wait under that snapshot may be 40001.
	if _, err := reservationDB.ExecContext(t.Context(), `SET default_transaction_isolation='read committed'`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	tx, err := holderDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = store.WithTenant(ctx, tx, f.tenant); err != nil {
		t.Fatal(err)
	}
	var locked int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM job_run_control_plane WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, f.tenant, e.Run.ID).Scan(&locked); err != nil {
		t.Fatal(err)
	}
	reserved := make(chan error, 1)
	go func() {
		reserved <- store.NewControlPlaneRepository(reservationDB).CreateAttemptForTenant(ctx, f.tenant, e.Run.ID, jobrun.Attempt{
			Number: e.Run.Attempt, KubernetesJobName: e.Run.AttemptJobName, KubernetesJobUID: e.Run.AttemptUID, Phase: jobrun.Running,
		}, jobrun.Running)
	}()
	for {
		select {
		case err := <-reserved:
			t.Fatalf("reservation crossed held run lock: %v", err)
		default:
		}
		var blocked bool
		if err := f.owner.QueryRowContext(ctx, `SELECT $1=ANY(pg_blocking_pids($2))`, holderPID, reservationPID).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	// Match recovery's run -> attempt order. The reservation must still be
	// waiting on run; if it held attempt already these two writes would deadlock.
	if _, err = tx.ExecContext(ctx, `UPDATE job_run_attempts_control_plane SET phase='ExecutionUnknown' WHERE tenant_id=$1 AND run_id=$2`, f.tenant, e.Run.ID); err != nil {
		t.Fatalf("run holder could not acquire attempt without deadlock: %v", err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE job_run_control_plane SET phase='ExecutionUnknown' WHERE tenant_id=$1 AND id=$2`, f.tenant, e.Run.ID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-reserved:
		if !errors.Is(err, store.ErrRunChanged) {
			t.Fatalf("reservation ignored the committed isolation state: %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	got := requireExecution(t, f, store.NewControlPlaneRepository(f.application), e.Run.ID)
	if got.Run.Phase != jobrun.ExecutionUnknown || got.Run.AttemptPhase != jobrun.ExecutionUnknown || got.Run.Attempt != e.Run.Attempt {
		t.Fatalf("reservation overwrote recovery: %+v", got.Run)
	}
}
