package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/domain/sourcecontrol"
	"orbitjob/internal/platform/scan"
)

func TestCheckSchedulePauseAdmissionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		mode                             string
		trigger                          jobrun.Trigger
		newRoot, paused, execution, gone bool
		wantReason                       string
	}{
		{name: "new timer check", mode: sourcecontrol.Check, trigger: jobrun.Check, newRoot: true, paused: true, wantReason: "schedule_paused"},
		{name: "unpaused timer check", mode: sourcecontrol.Check, trigger: jobrun.Check, newRoot: true},
		{name: "new scheduled job", mode: sourcecontrol.Kubernetes, trigger: jobrun.Schedule, newRoot: true, paused: true, wantReason: "schedule_paused"},
		{name: "new scheduled workflow", mode: sourcecontrol.Workflow, trigger: jobrun.Schedule, newRoot: true, paused: true, wantReason: "schedule_paused"},
		{name: "manual check source", mode: sourcecontrol.Check, trigger: jobrun.Manual, newRoot: true, paused: true},
		{name: "manual job", mode: sourcecontrol.Kubernetes, trigger: jobrun.Manual, newRoot: true, paused: true},
		{name: "manual workflow", mode: sourcecontrol.Workflow, trigger: jobrun.Manual, newRoot: true, paused: true},
		{name: "workflow step", mode: sourcecontrol.Kubernetes, trigger: jobrun.Workflow, newRoot: true, paused: true},
		{name: "function invocation", mode: sourcecontrol.Function, trigger: jobrun.Function, newRoot: true, paused: true},
		{name: "existing check attempt", mode: sourcecontrol.Check, trigger: jobrun.Check, paused: true},
		{name: "existing scheduled attempt", mode: sourcecontrol.Kubernetes, trigger: jobrun.Schedule, paused: true},
		{name: "execution pause still stops existing check", mode: sourcecontrol.Check, trigger: jobrun.Check, paused: true, execution: true, wantReason: "execution_paused"},
		{name: "execution pause precedes schedule pause", mode: sourcecontrol.Check, trigger: jobrun.Check, newRoot: true, paused: true, execution: true, wantReason: "execution_paused"},
		{name: "deleted source precedes schedule pause", mode: sourcecontrol.Check, trigger: jobrun.Check, newRoot: true, paused: true, gone: true, wantReason: "source_deleted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := reliabilityDB(t)
			tenant, uid := scan.GenerateID(), scan.GenerateID()
			mock.ExpectBegin()
			tx, err := db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			expectReliabilityControlRead(mock, tenant, tc.mode, uid, sourceControlRows(1, tc.paused, tc.execution, tc.gone, time.Now().UTC()))
			mock.ExpectRollback()
			defer func() { _ = tx.Rollback() }()
			err = checkSourceControls(context.Background(), tx, tenant, tc.mode, uid, tc.trigger, tc.newRoot)
			if tc.wantReason == "" {
				if err != nil {
					t.Fatalf("permitted work was blocked: %v", err)
				}
				return
			}
			var blocked *jobrun.AdmissionBlockedError
			if !errors.As(err, &blocked) || blocked.Reason != tc.wantReason {
				t.Fatalf("error=%v, want admission reason %q", err, tc.wantReason)
			}
		})
	}
}
