package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"orbitjob/internal/core/app/controlplane"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/sli"
	"orbitjob/internal/domain/resource"
)

// EventConfigurationForOutcome serializes configuration activation with the
// first plan for a committed terminal Run. Later derivatives reuse that plan.
func (r *SLIRepository) EventConfigurationForOutcome(ctx context.Context, tenantID string, runID int64) (sli.EventConfiguration, error) {
	var plan sli.EventConfiguration
	err := withAdmissionTx(ctx, r.db, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		var source string
		if err := tx.QueryRowContext(ctx, `SELECT source_uid FROM job_run_control_plane WHERE tenant_id=$1 AND id=$2
 UNION ALL SELECT source_uid FROM sli_event_plans WHERE tenant_id=$1 AND run_id=$2 LIMIT 1`, tenantID, runID).Scan(&source); errors.Is(err, sql.ErrNoRows) {
			return &resource.NotFoundError{Resource: "job_run", ID: runID}
		} else if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `SELECT orbitjob_lock_sli_source($1,$2)`, tenantID, source); err != nil {
			return err
		}
		if err := lockSLIEventRun(ctx, tx, tenantID, runID); err != nil {
			return err
		}
		found, err := loadSLIEventPlan(ctx, tx, tenantID, runID, &plan)
		if err != nil || found {
			return err
		}
		var phase, jobUID string
		var created, observed time.Time
		var semantics int
		var evidence []byte
		var scheduled, started, finished sql.NullTime
		err = tx.QueryRowContext(ctx, `SELECT r.phase,r.created_at,r.outcome_observed_at,r.sli_semantics_version,r.scheduled_for,
 COALESCE(a.kubernetes_job_uid,''),a.termination_evidence,a.started_at,a.completed_at
 FROM job_run_control_plane r LEFT JOIN LATERAL
 (SELECT kubernetes_job_uid,termination_evidence,started_at,completed_at FROM job_run_attempts_control_plane
  WHERE tenant_id=r.tenant_id AND run_id=r.id ORDER BY attempt_number DESC LIMIT 1) a ON true
 WHERE r.tenant_id=$1 AND r.id=$2 AND r.source_uid=$3`, tenantID, runID, source).Scan(&phase, &created, &observed, &semantics, &scheduled, &jobUID, &evidence, &started, &finished)
		if err != nil {
			return err
		}
		if phase != string(jobrun.Succeeded) && phase != string(jobrun.Failed) {
			return fmt.Errorf("SLI plan requires a confirmed execution outcome")
		}
		plan = sli.EventConfiguration{RunID: runID, Semantics: sli.EventTime, EventTime: observed.UTC(), TimeQuality: sli.TimeObserved, Configurations: []sli.Snapshot{}}
		if semantics == 1 {
			plan.Semantics = sli.LegacyProcessing
			plan.TimeQuality = "legacy_processing"
			if finished.Valid {
				plan.EventTime = finished.Time.UTC()
			} else if scheduled.Valid {
				plan.EventTime = scheduled.Time.UTC()
			}
		} else if start, end, ok := controlplane.TaskSpanFromEvidence(evidence, jobUID, created, observed); ok {
			start = start.UTC().Round(time.Microsecond)
			end = end.UTC().Round(time.Microsecond)
			plan.EventTime = end
			plan.TimeQuality = sli.TimeContainerFinished
			plan.ContainerStartedAt = &start
			plan.ContainerFinishedAt = &end
		}
		plan.EventTime = plan.EventTime.UTC().Round(time.Microsecond)
		var rows *sql.Rows
		if semantics == 1 {
			rows, err = tx.QueryContext(ctx, `SELECT v.definition_json,v.configuration_version,v.effective_from FROM sli_configuration_versions v JOIN slis s ON s.tenant_id=v.tenant_id AND s.id=v.sli_id
 WHERE v.tenant_id=$1 AND v.definition_json->>'source_type'='job_run' AND v.definition_json->'source_config'->>'source_uid'=$2
 AND (s.deleted_at IS NULL OR EXISTS(SELECT 1 FROM sli_snapshot_events e WHERE e.tenant_id=$1 AND e.run_id=$3 AND e.sli_id=s.id))
 ORDER BY v.sli_id LIMIT 257`, tenantID, source, runID)
		} else {
			rows, err = tx.QueryContext(ctx, `SELECT definition_json,configuration_version,effective_from FROM sli_configuration_versions
 WHERE tenant_id=$1 AND definition_json->>'source_type'='job_run' AND definition_json->'source_config'->>'source_uid'=$2
 AND effective_from<=$3 AND (effective_until IS NULL OR effective_until>$3) ORDER BY sli_id LIMIT 257`, tenantID, source, plan.EventTime)
		}
		if err != nil {
			return err
		}
		for rows.Next() {
			var definition []byte
			var snapshot sli.Snapshot
			if err = rows.Scan(&definition, &snapshot.ConfigurationVersion, &snapshot.ActiveFrom); err != nil {
				break
			}
			if err = json.Unmarshal(definition, &snapshot); err != nil {
				break
			}
			snapshot.StatisticsSemantics = plan.Semantics
			plan.Configurations = append(plan.Configurations, snapshot)
		}
		if err == nil {
			err = rows.Err()
		}
		_ = rows.Close()
		if err != nil {
			return err
		}
		if len(plan.Configurations) > 256 {
			return fmt.Errorf("SLI source exceeds the 256-configuration plan budget")
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO sli_event_plans(tenant_id,run_id,source_uid,semantics,event_time,time_quality,container_started_at,container_finished_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, tenantID, runID, source, plan.Semantics, plan.EventTime, plan.TimeQuality, plan.ContainerStartedAt, plan.ContainerFinishedAt); err != nil {
			return err
		}
		for _, configuration := range plan.Configurations {
			if _, err = tx.ExecContext(ctx, `INSERT INTO sli_event_plan_members VALUES($1,$2,$3,$4)`, tenantID, runID, configuration.ID, configuration.ConfigurationVersion); err != nil {
				return err
			}
		}
		return nil
	})
	return plan, err
}

func loadSLIEventPlan(ctx context.Context, tx *sql.Tx, tenant string, runID int64, plan *sli.EventConfiguration) (bool, error) {
	var start, end sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT semantics,event_time,time_quality,container_started_at,container_finished_at FROM sli_event_plans WHERE tenant_id=$1 AND run_id=$2`, tenant, runID).Scan(&plan.Semantics, &plan.EventTime, &plan.TimeQuality, &start, &end)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	plan.RunID = runID
	plan.Configurations = []sli.Snapshot{}
	if start.Valid {
		plan.ContainerStartedAt = &start.Time
	}
	if end.Valid {
		plan.ContainerFinishedAt = &end.Time
	}
	rows, err := tx.QueryContext(ctx, `SELECT v.definition_json,m.configuration_version,v.effective_from FROM sli_event_plan_members m JOIN sli_configuration_versions v
 ON v.tenant_id=m.tenant_id AND v.sli_id=m.sli_id AND v.configuration_version=m.configuration_version
 WHERE m.tenant_id=$1 AND m.run_id=$2 ORDER BY m.sli_id`, tenant, runID)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var data []byte
		var configuration sli.Snapshot
		if err = rows.Scan(&data, &configuration.ConfigurationVersion, &configuration.ActiveFrom); err != nil {
			return false, err
		}
		if err = json.Unmarshal(data, &configuration); err != nil {
			return false, err
		}
		configuration.StatisticsSemantics = plan.Semantics
		plan.Configurations = append(plan.Configurations, configuration)
	}
	return true, rows.Err()
}
