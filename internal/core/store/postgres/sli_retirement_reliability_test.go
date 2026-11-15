package postgres

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/core/app/slimaintenance"
	"orbitjob/internal/core/domain/slo"
	"orbitjob/internal/platform/scan"
)

func expectReliabilitySLIRunLock(mock sqlmock.Sqlmock, tenant string, runID int64) *sqlmock.ExpectedExec {
	return mock.ExpectExec(regexp.QuoteMeta(`SELECT pg_advisory_xact_lock(hashtextextended($1,0))`)).WithArgs("sli-event-run:" + tenant + ":" + strconv.FormatInt(runID, 10))
}

func expectReliabilitySLISourceLock(mock sqlmock.Sqlmock, tenant, source string) *sqlmock.ExpectedExec {
	return mock.ExpectExec(regexp.QuoteMeta(`SELECT orbitjob_lock_sli_source($1,$2)`)).WithArgs(tenant, source)
}

func expectRetirementPlanPredicate(mock sqlmock.Sqlmock, tenant string, before time.Time, c sliRetirementCandidate) *sqlmock.ExpectedQuery {
	return mock.ExpectQuery("WITH boundary AS").WithArgs(tenant, c.runID, before, int64(slo.MaxWindowDuration/time.Second), c.source)
}

func planRetirementRows(hasRun, young, incomplete, missing bool) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"has_run", "young", "incomplete", "recent_missing"}).AddRow(hasRun, young, incomplete, missing)
}

func expectRetirementPlanLocks(mock sqlmock.Sqlmock, tenant string, c sliRetirementCandidate) {
	expectReliabilityTx(mock, tenant)
	expectReliabilitySLISourceLock(mock, tenant, c.source).WillReturnResult(sqlmock.NewResult(0, 1))
	expectReliabilitySLIRunLock(mock, tenant, c.runID).WillReturnResult(sqlmock.NewResult(0, 1))
}

func TestSLIRetirementReliability_ProtectedPlansNeverDelete(t *testing.T) {
	for _, tc := range []struct {
		name, reason                       string
		hasRun, young, incomplete, missing bool
	}{
		{name: "run still retained", reason: "run_present", hasRun: true, young: true, incomplete: true, missing: true},
		{name: "unacknowledged members", reason: "incomplete_plan", incomplete: true},
		{name: "replay horizon", reason: "replay_horizon", young: true},
		{name: "queryable missing evidence", reason: "queryable_missing_evidence", missing: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := reliabilityDB(t)
			tenant, before := scan.GenerateID(), time.Now().UTC()
			candidate := sliRetirementCandidate{runID: 91, source: "nightly"}
			expectRetirementPlanLocks(mock, tenant, candidate)
			expectRetirementPlanPredicate(mock, tenant, before, candidate).WillReturnRows(planRetirementRows(tc.hasRun, tc.young, tc.incomplete, tc.missing))
			mock.ExpectCommit()
			removed, reason, err := NewSLIRepository(db).retireSLIPlan(context.Background(), tenant, before, candidate)
			if err != nil || removed != -1 || reason != tc.reason {
				t.Fatalf("removed=%d reason=%q err=%v", removed, reason, err)
			}
		})
	}
}

func TestSLIRetirementReliability_PlanDeletionIsAtomic(t *testing.T) {
	for _, tc := range []string{"commit", "event delete fails", "plan delete fails", "plan vanished", "event count unavailable", "commit fails"} {
		t.Run(tc, func(t *testing.T) {
			db, mock := reliabilityDB(t)
			tenant, before := scan.GenerateID(), time.Now().UTC()
			candidate := sliRetirementCandidate{runID: 91, source: "nightly"}
			failure := errors.New("storage unavailable")
			expectRetirementPlanLocks(mock, tenant, candidate)
			expectRetirementPlanPredicate(mock, tenant, before, candidate).WillReturnRows(planRetirementRows(false, false, false, false))
			events := mock.ExpectExec("DELETE FROM sli_snapshot_events WHERE tenant_id=\\$1 AND run_id=\\$2$").WithArgs(tenant, candidate.runID)
			switch tc {
			case "event delete fails":
				events.WillReturnError(failure)
			case "event count unavailable":
				events.WillReturnResult(sqlmock.NewErrorResult(failure))
			default:
				events.WillReturnResult(sqlmock.NewResult(0, 3))
				plans := mock.ExpectExec("DELETE FROM sli_event_plans").WithArgs(tenant, candidate.runID)
				switch tc {
				case "plan delete fails":
					plans.WillReturnError(failure)
				case "plan vanished":
					plans.WillReturnResult(sqlmock.NewResult(0, 0))
				default:
					plans.WillReturnResult(sqlmock.NewResult(0, 1))
				}
			}
			switch tc {
			case "commit":
				mock.ExpectCommit()
			case "commit fails":
				mock.ExpectCommit().WillReturnError(failure)
			default:
				mock.ExpectRollback()
			}
			removed, reason, err := NewSLIRepository(db).retireSLIPlan(context.Background(), tenant, before, candidate)
			if tc == "commit" {
				if err != nil || removed != 3 || reason != "" {
					t.Fatalf("successful retirement: removed=%d reason=%q err=%v", removed, reason, err)
				}
			} else if err == nil || removed != -1 || reason != "" {
				t.Fatalf("failed transaction reported deletion: removed=%d reason=%q err=%v", removed, reason, err)
			} else if tc == "plan vanished" {
				if !strings.Contains(err.Error(), "changed during retirement") {
					t.Fatalf("plan deletion lost its concurrency check: %v", err)
				}
			} else if !errors.Is(err, failure) {
				t.Fatalf("original storage failure lost: %v", err)
			}
		})
	}
}

func TestSLIRetirementReliability_LegacyProtectionAndDeletion(t *testing.T) {
	for _, tc := range []struct {
		name, reason                                string
		hasRun, hasPlan, compatible, young, missing bool
		deleteCount                                 int64
	}{
		{name: "planned event", reason: "planned_event", hasPlan: true, compatible: true},
		{name: "run still retained", reason: "run_present", hasRun: true, compatible: true},
		{name: "unsupported version", reason: "unsupported_legacy_event"},
		{name: "replay horizon", reason: "replay_horizon", compatible: true, young: true},
		{name: "missing evidence", reason: "queryable_missing_evidence", compatible: true, missing: true},
		{name: "old compatible event", compatible: true, deleteCount: 1},
		{name: "concurrent delete", compatible: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := reliabilityDB(t)
			tenant, before := scan.GenerateID(), time.Now().UTC()
			candidate := sliRetirementCandidate{runID: 91, sliID: 7}
			expectReliabilityTx(mock, tenant)
			expectReliabilitySLIRunLock(mock, tenant, candidate.runID).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery("WITH boundary AS").WithArgs(tenant, candidate.runID, candidate.sliID, before, int64(slo.MaxWindowDuration/time.Second)).
				WillReturnRows(sqlmock.NewRows([]string{"has_run", "has_plan", "compatible", "young", "missing"}).AddRow(tc.hasRun, tc.hasPlan, tc.compatible, tc.young, tc.missing))
			if tc.reason == "" {
				mock.ExpectExec("DELETE FROM sli_snapshot_events").WithArgs(tenant, candidate.runID, candidate.sliID).WillReturnResult(sqlmock.NewResult(0, tc.deleteCount))
			}
			mock.ExpectCommit()
			deleted, reason, err := NewSLIRepository(db).retireLegacySLIEvent(context.Background(), tenant, before, candidate)
			if err != nil || reason != tc.reason || deleted != (tc.deleteCount == 1) {
				t.Fatalf("deleted=%v reason=%q err=%v", deleted, reason, err)
			}
		})
	}
}

func expectRetirementCandidates(mock sqlmock.Sqlmock, tenant string, cursor slimaintenance.Cursor, limit int, plans bool) *sqlmock.ExpectedQuery {
	if plans {
		return mock.ExpectQuery("SELECT run_id,source_uid FROM sli_event_plans").WithArgs(tenant, cursor.PlanRunID, limit)
	}
	return mock.ExpectQuery("SELECT run_id,sli_id FROM sli_snapshot_events").WithArgs(tenant, cursor.EventRunID, cursor.EventSLIID, limit)
}

func TestSLIRetirementReliability_PageWrapsBothCursorsWithBoundedQueries(t *testing.T) {
	for _, invalidLimit := range []int{0, slimaintenance.DefaultBatchSize + 1} {
		t.Run(strconv.Itoa(invalidLimit), func(t *testing.T) {
			db, mock := reliabilityDB(t)
			tenant := scan.GenerateID()
			cursor := slimaintenance.Cursor{PlanRunID: 500, EventRunID: 600, EventSLIID: 700}
			expectReliabilityTx(mock, tenant)
			for _, plans := range []bool{true, false} {
				columns := []string{"run_id", "sli_id"}
				if plans {
					columns[1] = "source_uid"
				}
				expectRetirementCandidates(mock, tenant, cursor, slimaintenance.DefaultBatchSize, plans).WillReturnRows(sqlmock.NewRows(columns))
				expectRetirementCandidates(mock, tenant, slimaintenance.Cursor{}, slimaintenance.DefaultBatchSize, plans).WillReturnRows(sqlmock.NewRows(columns))
			}
			mock.ExpectCommit()
			page, err := NewSLIRepository(db).PruneRetiredEventsPage(context.Background(), tenant, time.Now(), cursor, invalidLimit)
			if err != nil || page.Next != (slimaintenance.Cursor{}) || page.Counts.ScannedPlans != 0 || page.Counts.ScannedEvents != 0 || page.Counts.Retained == nil {
				t.Fatalf("empty wrapped page=%+v err=%v", page, err)
			}
		})
	}
}

func TestSLIRetirementReliability_PageMakesProgressAfterItemFailure(t *testing.T) {
	db, mock := reliabilityDB(t)
	tenant, before := scan.GenerateID(), time.Now().UTC()
	failed := sliRetirementCandidate{runID: 11, source: "first"}
	success := sliRetirementCandidate{runID: 12, source: "second"}
	storageErr := errors.New("source lock unavailable")
	expectReliabilityTx(mock, tenant)
	expectRetirementCandidates(mock, tenant, slimaintenance.Cursor{}, 2, true).WillReturnRows(sqlmock.NewRows([]string{"run_id", "source_uid"}).AddRow(failed.runID, failed.source).AddRow(success.runID, success.source))
	expectRetirementCandidates(mock, tenant, slimaintenance.Cursor{}, 2, false).WillReturnRows(sqlmock.NewRows([]string{"run_id", "sli_id"}).AddRow(30, 7))
	mock.ExpectCommit()
	expectReliabilityTx(mock, tenant)
	expectReliabilitySLISourceLock(mock, tenant, failed.source).WillReturnError(storageErr)
	mock.ExpectRollback()
	expectRetirementPlanLocks(mock, tenant, success)
	expectRetirementPlanPredicate(mock, tenant, before, success).WillReturnRows(planRetirementRows(false, false, false, false))
	mock.ExpectExec("DELETE FROM sli_snapshot_events").WithArgs(tenant, success.runID).WillReturnResult(sqlmock.NewResult(0, 4))
	mock.ExpectExec("DELETE FROM sli_event_plans").WithArgs(tenant, success.runID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	expectReliabilityTx(mock, tenant)
	expectReliabilitySLIRunLock(mock, tenant, 30).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("WITH boundary AS").WithArgs(tenant, int64(30), int64(7), before, int64(slo.MaxWindowDuration/time.Second)).WillReturnRows(sqlmock.NewRows([]string{"run", "plan", "compatible", "young", "missing"}).AddRow(false, false, true, false, false))
	mock.ExpectExec("DELETE FROM sli_snapshot_events").WithArgs(tenant, int64(30), int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	page, err := NewSLIRepository(db).PruneRetiredEventsPage(context.Background(), tenant, before, slimaintenance.Cursor{}, 2)
	if !errors.Is(err, storageErr) || !strings.Contains(err.Error(), "retire SLI plan 11") {
		t.Fatalf("per-item error lost: %v", err)
	}
	if page.Next != (slimaintenance.Cursor{PlanRunID: 12, EventRunID: 30, EventSLIID: 7}) || page.Counts.ScannedPlans != 2 || page.Counts.ScannedEvents != 1 || page.Counts.RemovedPlans != 1 || page.Counts.RemovedEvents != 5 || page.Counts.RemovedLegacyEvents != 1 {
		t.Fatalf("partial progress was lost or double counted: %+v", page)
	}
}

func TestSLIRetirementReliability_ScanErrorAbortsBeforeAnyDeletion(t *testing.T) {
	db, mock := reliabilityDB(t)
	tenant := scan.GenerateID()
	storageErr := errors.New("candidate stream failed")
	expectReliabilityTx(mock, tenant)
	expectRetirementCandidates(mock, tenant, slimaintenance.Cursor{}, 2, true).WillReturnRows(sqlmock.NewRows([]string{"run_id", "source_uid"}).AddRow(11, "nightly").RowError(0, storageErr))
	mock.ExpectRollback()
	page, err := NewSLIRepository(db).PruneRetiredEventsPage(context.Background(), tenant, time.Now(), slimaintenance.Cursor{}, 2)
	if !errors.Is(err, storageErr) || !strings.Contains(err.Error(), "scan SLI retirement page") || page.Counts.RemovedEvents != 0 || page.Counts.RemovedPlans != 0 {
		t.Fatalf("scan failure was not atomic: page=%+v err=%v", page, err)
	}
}
