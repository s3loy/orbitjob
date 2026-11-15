package postgres

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/sourcecontrol"
	"orbitjob/internal/platform/scan"
)

func reliabilityDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
		_ = db.Close()
	})
	return db, mock
}

func expectReliabilityTx(mock sqlmock.Sqlmock, tenant string) {
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`SELECT set_config($1, $2, true)`)).WithArgs(tenantSetting, tenant).WillReturnResult(sqlmock.NewResult(0, 0))
}

func sourceControlRows(version int64, schedule, execution, tombstone bool, at time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"version", "schedule_paused", "execution_paused", "tombstoned", "actor", "updated_at"}).
		AddRow(version, schedule, execution, tombstone, "operator", at)
}

func expectReliabilityControlRead(mock sqlmock.Sqlmock, tenant, mode, uid string, rows *sqlmock.Rows) {
	mock.ExpectQuery(regexp.QuoteMeta(readSourceControlSQL)).WithArgs(tenant, mode, uid).WillReturnRows(rows)
}

func TestSourceControlReliability_RejectInvalidMutationBeforeTransaction(t *testing.T) {
	for _, tc := range []struct {
		name, mode, uid, actor string
		version                int64
	}{
		{name: "unsupported mode", mode: "arbitrary", uid: "source", actor: "operator"},
		{name: "missing source", mode: sourcecontrol.Kubernetes, actor: "operator"},
		{name: "oversize source", mode: sourcecontrol.Kubernetes, uid: strings.Repeat("s", 129), actor: "operator"},
		{name: "missing actor", mode: sourcecontrol.Kubernetes, uid: "source", actor: " "},
		{name: "negative version", mode: sourcecontrol.Kubernetes, uid: "source", actor: "operator", version: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := reliabilityDB(t)
			repo := NewSourceControlRepository(db)
			if _, err := repo.UpdateForTenant(context.Background(), scan.GenerateID(), tc.mode, tc.uid, tc.version, true, false, tc.actor); err == nil {
				t.Fatal("invalid source control mutation was accepted")
			}
		})
	}
}

func TestSourceControlReliability_GetScopedState(t *testing.T) {
	for _, found := range []bool{false, true} {
		t.Run(map[bool]string{false: "unset", true: "explicit"}[found], func(t *testing.T) {
			db, mock := reliabilityDB(t)
			tenant, uid := scan.GenerateID(), "nightly"
			now := time.Now().UTC().Truncate(time.Microsecond)
			rows := sqlmock.NewRows([]string{"version", "schedule_paused", "execution_paused", "tombstoned", "actor", "updated_at"})
			if found {
				rows = sourceControlRows(7, true, false, true, now)
			}
			expectReliabilityTx(mock, tenant)
			expectReliabilityControlRead(mock, tenant, sourcecontrol.Kubernetes, uid, rows)
			mock.ExpectCommit()
			state, gotFound, err := NewSourceControlRepository(db).GetForTenant(context.Background(), tenant, sourcecontrol.Kubernetes, uid)
			if err != nil || gotFound != found || state.Mode != sourcecontrol.Kubernetes || state.SourceUID != uid {
				t.Fatalf("state=%+v found=%v err=%v", state, gotFound, err)
			}
			if found && (state.Version != 7 || !state.SchedulePaused || state.ExecutionPaused || !state.Tombstoned || !state.UpdatedAt.Equal(now)) {
				t.Fatalf("persisted control state was lost: %+v", state)
			}
		})
	}
}

func TestSourceControlReliability_StaleVersionAndVerificationAbort(t *testing.T) {
	for _, rejectVerification := range []bool{false, true} {
		t.Run(map[bool]string{false: "stale version", true: "parent verification"}[rejectVerification], func(t *testing.T) {
			db, mock := reliabilityDB(t)
			tenant, uid := scan.GenerateID(), "nightly"
			denied := errors.New("parent permission revoked")
			expectReliabilityTx(mock, tenant)
			mock.ExpectExec(regexp.QuoteMeta(lockRevisionSQL)).WithArgs(sourceLockKey(tenant, sourcecontrol.Kubernetes, uid)).WillReturnResult(sqlmock.NewResult(0, 1))
			var verify func(context.Context, *sql.Tx) error
			if rejectVerification {
				verify = func(ctx context.Context, tx *sql.Tx) error {
					_, err := tx.ExecContext(ctx, "SELECT verify_authorized_parent()")
					return err
				}
				mock.ExpectExec(regexp.QuoteMeta("SELECT verify_authorized_parent()")).WillReturnError(denied)
			} else {
				expectReliabilityControlRead(mock, tenant, sourcecontrol.Kubernetes, uid, sourceControlRows(3, false, false, false, time.Now()))
			}
			mock.ExpectRollback()
			_, err := NewSourceControlRepository(db).UpdateForTenantWithVerification(context.Background(), tenant, sourcecontrol.Kubernetes, uid, 2, true, true, "operator", verify)
			if rejectVerification {
				if !errors.Is(err, denied) {
					t.Fatalf("verification error lost: %v", err)
				}
			} else {
				var conflict *resource.ConflictError
				if !errors.As(err, &conflict) || conflict.Resource != "source_control" || conflict.ID != uid {
					t.Fatalf("expected scoped version conflict, got %v", err)
				}
			}
		})
	}
}

func TestSourceControlReliability_UnchangedStateDoesNotWriteOrAudit(t *testing.T) {
	db, mock := reliabilityDB(t)
	tenant, uid := scan.GenerateID(), "nightly"
	now := time.Now().UTC().Truncate(time.Microsecond)
	expectReliabilityTx(mock, tenant)
	mock.ExpectExec(regexp.QuoteMeta(lockRevisionSQL)).WithArgs(sourceLockKey(tenant, sourcecontrol.Kubernetes, uid)).WillReturnResult(sqlmock.NewResult(0, 1))
	expectReliabilityControlRead(mock, tenant, sourcecontrol.Kubernetes, uid, sourceControlRows(8, true, true, true, now))
	mock.ExpectCommit()
	state, err := NewSourceControlRepository(db).UpdateForTenant(context.Background(), tenant, sourcecontrol.Kubernetes, uid, 8, true, true, "operator")
	if err != nil || state.Version != 8 || !state.Tombstoned || !state.UpdatedAt.Equal(now) {
		t.Fatalf("idempotent update changed state: %+v, %v", state, err)
	}
}

func TestSourceControlReliability_MutationAndAuditAreAtomic(t *testing.T) {
	for _, auditFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "audit rollback"}[auditFailure], func(t *testing.T) {
			db, mock := reliabilityDB(t)
			tenant, uid := scan.GenerateID(), "nightly"
			now := time.Now().UTC()
			auditErr := errors.New("audit storage unavailable")
			expectReliabilityTx(mock, tenant)
			mock.ExpectExec(regexp.QuoteMeta(lockRevisionSQL)).WithArgs(sourceLockKey(tenant, sourcecontrol.Kubernetes, uid)).WillReturnResult(sqlmock.NewResult(0, 1))
			expectReliabilityControlRead(mock, tenant, sourcecontrol.Kubernetes, uid, sqlmock.NewRows([]string{"version", "schedule_paused", "execution_paused", "tombstoned", "actor", "updated_at"}))
			mock.ExpectExec("INSERT INTO execution_source_controls").WithArgs(tenant, sourcecontrol.Kubernetes, uid, true, false, "operator").WillReturnResult(sqlmock.NewResult(0, 1))
			expectReliabilityControlRead(mock, tenant, sourcecontrol.Kubernetes, uid, sourceControlRows(1, true, false, false, now))
			audit := mock.ExpectExec("INSERT INTO audit_events").WithArgs(tenant, "operator", "source.controls_changed", "source_control", sourcecontrol.Kubernetes+":"+uid, `{"execution_paused":false,"schedule_paused":true,"tombstoned":false,"version":1}`)
			if auditFailure {
				audit.WillReturnError(auditErr)
				mock.ExpectRollback()
			} else {
				audit.WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			}
			state, err := NewSourceControlRepository(db).UpdateForTenant(context.Background(), tenant, sourcecontrol.Kubernetes, uid, 0, true, false, "operator")
			if auditFailure {
				if !errors.Is(err, auditErr) {
					t.Fatalf("audit failure did not abort mutation: %v", err)
				}
			} else if err != nil || state.Version != 1 || !state.SchedulePaused || state.ExecutionPaused {
				t.Fatalf("state=%+v err=%v", state, err)
			}
		})
	}
}
