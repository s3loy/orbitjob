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

	"orbitjob/internal/core/domain/sli"
	"orbitjob/internal/platform/scan"
)

func reliabilityEvaluation() sli.Evaluation {
	good := true
	event := time.Date(2026, 11, 15, 12, 3, 11, 0, time.UTC)
	return sli.Evaluation{
		RunID: 91, SLIID: 7, ConfigurationVersion: 3, Semantics: sli.EventTime,
		EventTime: event, WindowStart: event.Truncate(defaultSnapshotWindow),
		TimeQuality: sli.TimeObserved, Good: &good,
	}
}

func expectEvaluationTx(mock sqlmock.Sqlmock, tenant string, e sli.Evaluation) {
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`SELECT set_config('app.tenant_id',$1,true)`)).WithArgs(tenant).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT source_uid FROM sli_event_plans").WithArgs(tenant, e.RunID).WillReturnRows(sqlmock.NewRows([]string{"source_uid"}).AddRow("nightly"))
	expectReliabilitySLISourceLock(mock, tenant, "nightly").WillReturnResult(sqlmock.NewResult(0, 1))
	expectReliabilitySLIRunLock(mock, tenant, e.RunID).WillReturnResult(sqlmock.NewResult(0, 1))
}

func expectEvaluationInsert(mock sqlmock.Sqlmock, tenant string, e sli.Evaluation) *sqlmock.ExpectedQuery {
	state := "evaluated"
	var reason any
	if e.Good == nil {
		state, reason = "unavailable", e.MissingReason
	}
	return mock.ExpectQuery(`INSERT INTO sli_snapshot_events.*m.configuration_version=\$4.*AND p.event_time=\$9 AND p.semantics=\$10 AND p.time_quality=\$11.*ON CONFLICT.*DO NOTHING RETURNING run_id`).
		WithArgs(tenant, e.RunID, e.SLIID, e.ConfigurationVersion, e.WindowStart, e.Good, state, reason, e.EventTime, e.Semantics, e.TimeQuality)
}

func expectEvaluationCounter(mock sqlmock.Sqlmock, tenant string, e sli.Evaluation) *sqlmock.ExpectedExec {
	good := int64(0)
	if *e.Good {
		good = 1
	}
	return mock.ExpectExec("INSERT INTO sli_snapshots").WithArgs(tenant, e.SLIID, e.WindowStart, e.WindowStart.Add(defaultSnapshotWindow), good, e.Semantics, e.EventTime)
}

func expectEvaluationFreshness(mock sqlmock.Sqlmock, tenant string, e sli.Evaluation) *sqlmock.ExpectedExec {
	var sample any
	if e.Good != nil {
		sample = e.EventTime
	}
	return mock.ExpectExec("INSERT INTO sli_observation_state").WithArgs(tenant, e.SLIID, e.Semantics, e.EventTime, sample)
}

func TestSLIEvaluationReliability_InvalidFactsDoNotOpenTransaction(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*sli.Evaluation)
	}{
		{name: "missing run", mutate: func(e *sli.Evaluation) { e.RunID = 0 }},
		{name: "missing sli", mutate: func(e *sli.Evaluation) { e.SLIID = 0 }},
		{name: "missing configuration version", mutate: func(e *sli.Evaluation) { e.ConfigurationVersion = 0 }},
		{name: "missing event time", mutate: func(e *sli.Evaluation) { e.EventTime = time.Time{} }},
		{name: "wrong window", mutate: func(e *sli.Evaluation) { e.WindowStart = e.WindowStart.Add(defaultSnapshotWindow) }},
		{name: "unexplained missing fact", mutate: func(e *sli.Evaluation) { e.Good = nil }},
		{name: "oversize missing reason", mutate: func(e *sli.Evaluation) { e.Good, e.MissingReason = nil, strings.Repeat("x", 129) }},
		{name: "sample and missing reason", mutate: func(e *sli.Evaluation) { e.MissingReason = "missing_timing" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := reliabilityDB(t)
			e := reliabilityEvaluation()
			tc.mutate(&e)
			inserted, err := NewSLISnapshotRepository(db).RecordEvaluation(context.Background(), scan.GenerateID(), e)
			if inserted || err == nil {
				t.Fatalf("invalid fact was accepted: inserted=%v err=%v", inserted, err)
			}
		})
	}
}

func TestSLIEvaluationReliability_MissingEvidenceNeverAddsBadSample(t *testing.T) {
	for _, kind := range []string{"good sample", "bad sample", "missing evidence"} {
		t.Run(kind, func(t *testing.T) {
			db, mock := reliabilityDB(t)
			tenant, e := scan.GenerateID(), reliabilityEvaluation()
			switch kind {
			case "bad sample":
				*e.Good = false
			case "missing evidence":
				e.Good, e.MissingReason = nil, "missing_container_span"
			}
			expectEvaluationTx(mock, tenant, e)
			expectEvaluationInsert(mock, tenant, e).WillReturnRows(sqlmock.NewRows([]string{"run_id"}).AddRow(e.RunID))
			if e.Good != nil {
				expectEvaluationCounter(mock, tenant, e).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			expectEvaluationFreshness(mock, tenant, e).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()
			inserted, err := NewSLISnapshotRepository(db).RecordEvaluation(context.Background(), tenant, e)
			if err != nil || !inserted {
				t.Fatalf("valid %s: inserted=%v err=%v", kind, inserted, err)
			}
		})
	}
}

func TestSLIEvaluationReliability_DuplicateDoesNotIncrementCounters(t *testing.T) {
	for _, exists := range []bool{true, false} {
		t.Run(map[bool]string{true: "existing fact", false: "unauthorized identity"}[exists], func(t *testing.T) {
			db, mock := reliabilityDB(t)
			tenant, e := scan.GenerateID(), reliabilityEvaluation()
			expectEvaluationTx(mock, tenant, e)
			expectEvaluationInsert(mock, tenant, e).WillReturnRows(sqlmock.NewRows([]string{"run_id"}))
			mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM sli_snapshot_events`).WithArgs(tenant, e.RunID, e.SLIID).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(exists))
			if exists {
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			inserted, err := NewSLISnapshotRepository(db).RecordEvaluation(context.Background(), tenant, e)
			if inserted {
				t.Fatal("duplicate or unauthorized evaluation was counted as a new fact")
			}
			if exists && err != nil {
				t.Fatalf("duplicate should be idempotent: %v", err)
			}
			if !exists && (err == nil || !strings.Contains(err.Error(), "plan does not authorize")) {
				t.Fatalf("unauthorized plan identity was not rejected: %v", err)
			}
		})
	}
}

func TestSLIEvaluationReliability_DerivativeFailureRollsBackFact(t *testing.T) {
	for _, stage := range []string{"event insert", "counter", "freshness"} {
		t.Run(stage, func(t *testing.T) {
			db, mock := reliabilityDB(t)
			tenant, e := scan.GenerateID(), reliabilityEvaluation()
			failure := errors.New("derivative storage failed")
			expectEvaluationTx(mock, tenant, e)
			insert := expectEvaluationInsert(mock, tenant, e)
			if stage == "event insert" {
				insert.WillReturnError(failure)
			} else {
				insert.WillReturnRows(sqlmock.NewRows([]string{"run_id"}).AddRow(e.RunID))
				counter := expectEvaluationCounter(mock, tenant, e)
				if stage == "counter" {
					counter.WillReturnError(failure)
				} else {
					counter.WillReturnResult(sqlmock.NewResult(0, 1))
					expectEvaluationFreshness(mock, tenant, e).WillReturnError(failure)
				}
			}
			mock.ExpectRollback()
			inserted, err := NewSLISnapshotRepository(db).RecordEvaluation(context.Background(), tenant, e)
			if inserted || !errors.Is(err, failure) {
				t.Fatalf("failed %s did not abort the fact transaction: inserted=%v err=%v", stage, inserted, err)
			}
		})
	}
}

func TestSLIEvaluationReliability_MissingPlanRollsBack(t *testing.T) {
	db, mock := reliabilityDB(t)
	tenant, e := scan.GenerateID(), reliabilityEvaluation()
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`SELECT set_config('app.tenant_id',$1,true)`)).WithArgs(tenant).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT source_uid FROM sli_event_plans").WithArgs(tenant, e.RunID).WillReturnRows(sqlmock.NewRows([]string{"source_uid"}))
	mock.ExpectRollback()
	inserted, err := NewSLISnapshotRepository(db).RecordEvaluation(context.Background(), tenant, e)
	if inserted || !errors.Is(err, sql.ErrNoRows) || !strings.Contains(err.Error(), "read SLI event plan") {
		t.Fatalf("missing plan did not abort evaluation: inserted=%v err=%v", inserted, err)
	}
}
