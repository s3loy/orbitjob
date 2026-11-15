package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"orbitjob/internal/core/domain/sli"
	"orbitjob/internal/platform/scan"
)

func expectReliabilityPlanLocks(mock sqlmock.Sqlmock, tenant string, runID int64) {
	expectReliabilityTx(mock, tenant)
	mock.ExpectQuery("SELECT source_uid FROM job_run_control_plane").WithArgs(tenant, runID).WillReturnRows(sqlmock.NewRows([]string{"source_uid"}).AddRow("nightly"))
	expectReliabilitySLISourceLock(mock, tenant, "nightly").WillReturnResult(sqlmock.NewResult(0, 1))
	expectReliabilitySLIRunLock(mock, tenant, runID).WillReturnResult(sqlmock.NewResult(0, 1))
}

func reliabilityPlanRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"semantics", "event_time", "time_quality", "container_started_at", "container_finished_at"})
}

func reliabilityDefinition(t *testing.T, tenant string, id int64, active time.Time) []byte {
	t.Helper()
	definition, err := json.Marshal(sli.Snapshot{ID: id, TenantID: tenant, Name: "frozen", ConfigurationVersion: 3, ActiveFrom: active, SourceType: "job_run", SourceConfig: map[string]any{"source_uid": "nightly"}})
	if err != nil {
		t.Fatal(err)
	}
	return definition
}

func TestSLIPlanReliability_ReplayUsesFrozenConfigurationAndTiming(t *testing.T) {
	db, mock := reliabilityDB(t)
	tenant := scan.GenerateID()
	const runID int64 = 91
	start := time.Date(2026, 11, 15, 1, 0, 0, 0, time.UTC)
	end := start.Add(time.Minute)
	expectReliabilityPlanLocks(mock, tenant, runID)
	mock.ExpectQuery("SELECT semantics,event_time,time_quality").WithArgs(tenant, runID).
		WillReturnRows(reliabilityPlanRows().AddRow(sli.EventTime, end, sli.TimeContainerFinished, start, end))
	mock.ExpectQuery("SELECT v.definition_json,m.configuration_version,v.effective_from").WithArgs(tenant, runID).
		WillReturnRows(sqlmock.NewRows([]string{"definition_json", "configuration_version", "effective_from"}).AddRow(reliabilityDefinition(t, tenant, 7, start), 3, start))
	mock.ExpectCommit()
	plan, err := NewSLIRepository(db).EventConfigurationForOutcome(context.Background(), tenant, runID)
	if err != nil || plan.RunID != runID || plan.Semantics != sli.EventTime || plan.TimeQuality != sli.TimeContainerFinished || !plan.EventTime.Equal(end) || plan.ContainerStartedAt == nil || !plan.ContainerStartedAt.Equal(start) || plan.ContainerFinishedAt == nil || !plan.ContainerFinishedAt.Equal(end) {
		t.Fatalf("stored timing was not replayed: plan=%+v err=%v", plan, err)
	}
	if len(plan.Configurations) != 1 || plan.Configurations[0].ID != 7 || plan.Configurations[0].TenantID != tenant || plan.Configurations[0].ConfigurationVersion != 3 || plan.Configurations[0].StatisticsSemantics != sli.EventTime || !plan.Configurations[0].ActiveFrom.Equal(start) {
		t.Fatalf("frozen membership was not replayed: %+v", plan.Configurations)
	}
}

func TestSLIPlanReliability_NewPlanKeepsEventAndLegacyTimeBoundaries(t *testing.T) {
	for _, kind := range []string{"event time", "legacy completed", "legacy scheduled", "member write failure"} {
		t.Run(kind, func(t *testing.T) {
			db, mock := reliabilityDB(t)
			tenant := scan.GenerateID()
			const runID int64 = 91
			created := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
			observed := created.Add(2 * time.Hour)
			scheduled := created.Add(time.Minute)
			finished := created.Add(time.Hour)
			semantics, quality, event := sli.EventTime, sli.TimeObserved, observed
			version := 2
			var finish any
			if strings.HasPrefix(kind, "legacy") {
				version, semantics, quality, event = 1, sli.LegacyProcessing, "legacy_processing", scheduled
				if kind == "legacy completed" {
					finish, event = finished, finished
				}
			}
			expectReliabilityPlanLocks(mock, tenant, runID)
			mock.ExpectQuery("SELECT semantics,event_time,time_quality").WithArgs(tenant, runID).WillReturnRows(reliabilityPlanRows())
			mock.ExpectQuery("SELECT r.phase,r.created_at,r.outcome_observed_at").WithArgs(tenant, runID, "nightly").
				WillReturnRows(sqlmock.NewRows([]string{"phase", "created_at", "observed", "semantics", "scheduled_for", "job_uid", "evidence", "started_at", "completed_at"}).AddRow("Succeeded", created, observed, version, scheduled, "", nil, nil, finish))
			rows := sqlmock.NewRows([]string{"definition_json", "configuration_version", "effective_from"}).AddRow(reliabilityDefinition(t, tenant, 7, created), 3, created)
			if version == 1 {
				mock.ExpectQuery("SELECT v.definition_json,v.configuration_version,v.effective_from").WithArgs(tenant, "nightly", runID).WillReturnRows(rows)
			} else {
				mock.ExpectQuery("SELECT definition_json,configuration_version,effective_from").WithArgs(tenant, "nightly", event).WillReturnRows(rows)
			}
			mock.ExpectExec("INSERT INTO sli_event_plans").WithArgs(tenant, runID, "nightly", semantics, event, quality, nil, nil).WillReturnResult(sqlmock.NewResult(0, 1))
			member := mock.ExpectExec("INSERT INTO sli_event_plan_members").WithArgs(tenant, runID, int64(7), 3)
			failure := errors.New("plan member unavailable")
			if kind == "member write failure" {
				member.WillReturnError(failure)
				mock.ExpectRollback()
			} else {
				member.WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			}
			plan, err := NewSLIRepository(db).EventConfigurationForOutcome(context.Background(), tenant, runID)
			if kind == "member write failure" {
				if !errors.Is(err, failure) {
					t.Fatalf("member failure did not abort plan: %v", err)
				}
				return
			}
			if err != nil || !plan.EventTime.Equal(event) || plan.Semantics != semantics || plan.TimeQuality != quality || len(plan.Configurations) != 1 || plan.Configurations[0].StatisticsSemantics != semantics {
				t.Fatalf("event boundary lost: plan=%+v err=%v", plan, err)
			}
		})
	}
}

func TestSLIPlanReliability_RejectNonterminalAndOversizedPlans(t *testing.T) {
	for _, overflow := range []bool{false, true} {
		t.Run(map[bool]string{false: "unconfirmed outcome", true: "configuration budget"}[overflow], func(t *testing.T) {
			db, mock := reliabilityDB(t)
			tenant := scan.GenerateID()
			const runID int64 = 91
			now := time.Date(2026, 11, 15, 1, 0, 0, 0, time.UTC)
			phase, message := "Running", "confirmed execution outcome"
			if overflow {
				phase, message = "Failed", "256-configuration plan budget"
			}
			expectReliabilityPlanLocks(mock, tenant, runID)
			mock.ExpectQuery("SELECT semantics,event_time,time_quality").WithArgs(tenant, runID).WillReturnRows(reliabilityPlanRows())
			mock.ExpectQuery("SELECT r.phase,r.created_at,r.outcome_observed_at").WithArgs(tenant, runID, "nightly").
				WillReturnRows(sqlmock.NewRows([]string{"phase", "created_at", "observed", "semantics", "scheduled_for", "job_uid", "evidence", "started_at", "completed_at"}).AddRow(phase, now.Add(-time.Hour), now, 2, nil, "", nil, nil, nil))
			if overflow {
				rows := sqlmock.NewRows([]string{"definition_json", "configuration_version", "effective_from"})
				for id := int64(1); id <= 257; id++ {
					rows.AddRow(reliabilityDefinition(t, tenant, id, now), 3, now)
				}
				mock.ExpectQuery("SELECT definition_json,configuration_version,effective_from").WithArgs(tenant, "nightly", now).WillReturnRows(rows)
			}
			mock.ExpectRollback()
			_, err := NewSLIRepository(db).EventConfigurationForOutcome(context.Background(), tenant, runID)
			if err == nil || !strings.Contains(err.Error(), message) {
				t.Fatalf("unsafe plan was not rejected: %v", err)
			}
		})
	}
}
