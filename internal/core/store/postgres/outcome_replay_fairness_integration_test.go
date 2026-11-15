//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"testing"

	"orbitjob/internal/core/domain/jobrun"
	store "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/operator"
)

type poisonProjection struct {
	healthyID    int64
	healthyCalls int
	failedCalls  int
}

func (p *poisonProjection) RecordTerminalPhaseOnce(_ context.Context, _ string, runID int64, _ jobrun.Phase) error {
	if runID == p.healthyID {
		p.healthyCalls++
		return nil
	}
	p.failedCalls++
	return fmt.Errorf("projection remains invalid for run %d", runID)
}

func TestOutcomeReplayRotatesPastPoisonBatchWithoutAcknowledgingIt(t *testing.T) {
	f := seedWorkflowFence(t)
	if _, err := f.owner.ExecContext(t.Context(), `INSERT INTO job_run_control_plane(tenant_id,source_uid,revision_id,occurrence_key,trigger,actor,phase,max_attempts) SELECT $1::char(26),$2::text,$3,lpad(to_hex(n),64,'0'),'Manual','review','Succeeded',1 FROM generate_series(1,101) n`, f.tenant, f.source, f.revision); err != nil {
		t.Fatal(err)
	}
	projection := &poisonProjection{}
	if err := f.owner.QueryRowContext(t.Context(), `SELECT max(id) FROM job_run_control_plane WHERE tenant_id=$1`, f.tenant).Scan(&projection.healthyID); err != nil {
		t.Fatal(err)
	}
	ledger := store.NewControlPlaneRepository(f.application)
	rt := operator.Runtime{Runs: ledger, Outcomes: projection}
	cursor := &operator.OutcomeReplayCursor{}
	for range 3 {
		_, _ = rt.ReplayOutcomes(t.Context(), []string{f.tenant}, 100, cursor)
	}
	if projection.healthyCalls != 1 || projection.failedCalls != 200 {
		t.Fatalf("cursor failed to advance and wrap: healthy=%d failed=%d", projection.healthyCalls, projection.failedCalls)
	}
	var pending, acknowledged int
	if err := f.owner.QueryRowContext(t.Context(), `SELECT count(*) FILTER (WHERE outcome_recorded_at IS NULL), count(*) FILTER (WHERE outcome_recorded_at IS NOT NULL) FROM job_run_control_plane WHERE tenant_id=$1`, f.tenant).Scan(&pending, &acknowledged); err != nil {
		t.Fatal(err)
	}
	if pending != 100 || acknowledged != 1 {
		t.Fatalf("failed facts were acknowledged: pending=%d acknowledged=%d", pending, acknowledged)
	}
	// Scan progress is disposable. A new process retries the durable failures;
	// it neither replays the acknowledged healthy row nor loses the bad facts.
	_, _ = rt.ReplayOutcomes(t.Context(), []string{f.tenant}, 100, &operator.OutcomeReplayCursor{})
	if projection.failedCalls != 300 || projection.healthyCalls != 1 {
		t.Fatalf("restart changed acknowledgement semantics: %+v", projection)
	}
}
