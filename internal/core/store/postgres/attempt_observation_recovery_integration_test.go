//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"orbitjob/internal/core/domain/jobrun"
)

func TestBoundAttemptAcceptsFirstPhaseAtBoundResourceVersion(t *testing.T) {
	for _, binding := range []string{"watch", "ensure"} {
		for _, phase := range []jobrun.Phase{jobrun.Running, jobrun.Succeeded, jobrun.Failed} {
			t.Run(fmt.Sprintf("%s/%s", binding, phase), func(t *testing.T) {
				db, tenant, id := seedIntegrityRun(t, jobrun.Pending, 0)
				ctx := ctxStore(t)
				repo := NewControlPlaneRepository(db)
				intent := jobrun.Attempt{Number: 1, KubernetesJobName: tenant}
				err := repo.CreateAttemptWithEnsure(ctx, tenant, id, intent, func(context.Context) (jobrun.Attempt, error) {
					if binding == "watch" {
						return jobrun.Attempt{}, errors.New("create response lost")
					}
					return jobrun.Attempt{Number: 1, KubernetesJobName: tenant, KubernetesJobUID: tenant, ObservedResourceVersion: "17", Phase: jobrun.CreatingAttempt}, nil
				})
				if binding == "watch" {
					if err == nil {
						t.Fatal("uncertain creation unexpectedly succeeded")
					}
					if err = repo.BindAttemptObservation(ctx, tenant, id, 1, tenant, tenant, "17"); err != nil {
						t.Fatal(err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if runID, number, err := repo.UpdateAttemptPhase(ctx, tenant, tenant, string(phase), tenant, "17"); err != nil || runID != id || number != 1 {
					t.Fatalf("first bound observation run=%d attempt=%d err=%v", runID, number, err)
				}
				var storedPhase, rv string
				var completed bool
				if err := db.QueryRowContext(ctx, `SELECT phase,observed_resource_version,completed_at IS NOT NULL FROM job_run_attempts_control_plane WHERE run_id=$1`, id).Scan(&storedPhase, &rv, &completed); err != nil {
					t.Fatal(err)
				}
				if storedPhase != string(phase) || rv != "17" || completed != jobrun.Terminal(phase) {
					t.Fatalf("persisted phase=%s rv=%s completed=%t", storedPhase, rv, completed)
				}
			})
		}
	}
}

func TestSameResourceVersionExceptionsPreserveObservationGuards(t *testing.T) {
	for _, tc := range []struct {
		prior, next jobrun.Phase
		rv          string
	}{
		{jobrun.Running, jobrun.CreatingAttempt, "17"},
		{jobrun.Running, jobrun.Succeeded, "17"},
		{jobrun.Succeeded, jobrun.Running, "17"},
		{jobrun.Succeeded, jobrun.Failed, "18"},
		{jobrun.Canceled, jobrun.Running, "18"},
		{jobrun.RetryWaiting, jobrun.Running, "18"},
		{jobrun.CreatingAttempt, jobrun.Succeeded, "16"},
		{jobrun.ExecutionUnknown, jobrun.CancelRequested, "17"},
	} {
		t.Run(fmt.Sprintf("%s/%s/%s", tc.prior, tc.next, tc.rv), func(t *testing.T) {
			db, tenant, id := seedIntegrityRun(t, jobrun.Running, 1)
			ctx := ctxStore(t)
			repo := NewControlPlaneRepository(db)
			if _, err := db.ExecContext(ctx, `INSERT INTO job_run_attempts_control_plane(tenant_id,run_id,attempt_number,phase,kubernetes_job_name,kubernetes_job_uid,observed_resource_version) VALUES($1::char(26),$2,1,$3,$1::text,$1::text,'17')`, tenant, id, tc.prior); err != nil {
				t.Fatal(err)
			}
			if _, _, err := repo.UpdateAttemptPhase(ctx, tenant, tenant, string(tc.next), tenant, tc.rv); !errors.Is(err, ErrStaleObservation) {
				t.Fatalf("illegal observation returned %v", err)
			}
			var storedPhase, rv string
			if err := db.QueryRowContext(ctx, `SELECT phase,observed_resource_version FROM job_run_attempts_control_plane WHERE run_id=$1`, id).Scan(&storedPhase, &rv); err != nil {
				t.Fatal(err)
			}
			if storedPhase != string(tc.prior) || rv != "17" {
				t.Fatalf("refused observation persisted phase=%s rv=%s", storedPhase, rv)
			}
		})
	}
}
