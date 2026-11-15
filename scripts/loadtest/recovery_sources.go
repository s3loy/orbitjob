package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"reflect"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	workloadsv1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
)

type recoverySource struct {
	Metadata metav1.ObjectMeta `json:"metadata"`
	Status   struct {
		ActiveRevision int64 `json:"activeRevision"`
	} `json:"status"`
}

func readRecoveryObject(ctx context.Context, kind, namespace, name string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	args := []string{"get", kind}
	if name != "" {
		args = append(args, name)
	}
	args = append(args, "-n", namespace, "-o", "json")
	data, err := exec.CommandContext(ctx, "kubectl", args...).Output()
	if err != nil {
		return fmt.Errorf("read %s %s/%s: %w", kind, namespace, name, err)
	}
	if err = json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode %s: %w", kind, err)
	}
	return nil
}

type RecoveryTarget struct {
	UID                string   `json:"uid"`
	Generation         int64    `json:"generation"`
	ObservedGeneration int64    `json:"observed_generation"`
	Desired            int32    `json:"desired"`
	Ready              int32    `json:"ready"`
	Updated            int32    `json:"updated"`
	PodUIDs            []string `json:"pod_uids"`
}

func readRecoveryTarget(ctx context.Context, fault FaultSpec) (RecoveryTarget, error) {
	var deployment appsv1.Deployment
	var result RecoveryTarget
	if err := readRecoveryObject(ctx, "deployment", FaultNamespace(fault.Name), "orbitjob-"+fault.Name, &deployment); err != nil {
		return result, err
	}
	if deployment.Spec.Replicas == nil || deployment.Spec.Selector == nil {
		return result, fmt.Errorf("fault target lacks replicas/selector")
	}
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil || selector.Empty() {
		return result, fmt.Errorf("fault target has no exact selector")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, "kubectl", "get", "pods", "-n", FaultNamespace(fault.Name), "-l", selector.String(), "-o", "json").Output()
	if err != nil {
		return result, err
	}
	var pods corev1.PodList
	if err = json.Unmarshal(data, &pods); err != nil {
		return result, err
	}
	result = RecoveryTarget{UID: string(deployment.UID), Generation: deployment.Generation, ObservedGeneration: deployment.Status.ObservedGeneration, Desired: *deployment.Spec.Replicas, Ready: deployment.Status.ReadyReplicas, Updated: deployment.Status.UpdatedReplicas}
	for _, pod := range pods.Items {
		if selector.Matches(labels.Set(pod.Labels)) {
			result.PodUIDs = append(result.PodUIDs, string(pod.UID))
		}
	}
	if result.UID == "" {
		return result, fmt.Errorf("fault target UID missing")
	}
	return result, nil
}

func targetReplaced(before, after RecoveryTarget) bool {
	if before.UID == "" || before.UID != after.UID || len(before.PodUIDs) == 0 || after.Generation <= before.Generation || after.ObservedGeneration < after.Generation || after.Desired <= 0 || after.Ready != after.Desired || after.Updated != after.Desired || len(after.PodUIDs) != int(after.Desired) {
		return false
	}
	for _, old := range before.PodUIDs {
		for _, current := range after.PodUIDs {
			if current == "" || old == current {
				return false
			}
		}
	}
	return true
}

type RecoveryAttempt struct {
	Number      int        `json:"number"`
	Phase       string     `json:"phase"`
	JobName     string     `json:"job_name"`
	JobUID      string     `json:"job_uid"`
	OwnerUID    string     `json:"owner_uid"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type RecoveryRun struct {
	ID                 int64             `json:"id"`
	Tenant             string            `json:"tenant"`
	Revision           int64             `json:"revision"`
	SourceUID          string            `json:"source_uid"`
	Occurrence         string            `json:"occurrence"`
	Phase              string            `json:"phase"`
	Trigger            string            `json:"trigger"`
	CreatedAt          time.Time         `json:"created_at"`
	TerminalObservedAt *time.Time        `json:"terminal_observed_at,omitempty"`
	Attempts           []RecoveryAttempt `json:"attempts"`
	CRUID              string            `json:"cr_uid"`
	OwnedJobUID        string            `json:"owned_job_uid"`
	JobSucceeded       bool              `json:"job_succeeded"`
}

func collectRecoveryRun(ctx context.Context, db *sql.DB, c RecoveryCanary, b PreparedCanary, receipt TriggerResponse) (RecoveryRun, error) {
	var r RecoveryRun
	if receipt.OccurrenceKey == "" || receipt.Namespace != b.Namespace || receipt.Name != jobRunCRName(b.CaseID, receipt.OccurrenceKey) || receipt.Trigger != "Manual" {
		return r, fmt.Errorf("canary trigger receipt lacks exact manifest identity")
	}
	var source recoverySource
	if err := readRecoveryObject(ctx, "scheduledjob", b.Namespace, b.CaseID, &source); err != nil {
		return r, err
	}
	if source.Metadata.UID == "" || string(source.Metadata.UID) != b.SourceUID || source.Metadata.Namespace != b.Namespace || source.Status.ActiveRevision != b.RevisionID {
		return r, fmt.Errorf("canary source identity changed")
	}
	err := withTenantTx(ctx, db, b.Tenant, func(tx *sql.Tx) error {
		var total int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM job_run_control_plane WHERE tenant_id=$1 AND (revision_id=$2 OR source_uid=$3)`, b.Tenant, b.RevisionID, b.SourceUID).Scan(&total); err != nil {
			return err
		}
		if total > 1 {
			return fmt.Errorf("canary source has extra occurrences outside the accepted receipt")
		}
		err := tx.QueryRowContext(ctx, `SELECT id,tenant_id,revision_id,source_uid,occurrence_key,phase,trigger,created_at,outcome_observed_at
		 FROM job_run_control_plane WHERE tenant_id=$1 AND revision_id=$2 AND occurrence_key=$3`, b.Tenant, b.RevisionID, receipt.OccurrenceKey).Scan(&r.ID, &r.Tenant, &r.Revision, &r.SourceUID, &r.Occurrence, &r.Phase, &r.Trigger, &r.CreatedAt, &r.TerminalObservedAt)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT attempt_number,phase,kubernetes_job_name,COALESCE(kubernetes_job_uid,''),COALESCE(owner_run_uid,''),completed_at
		 FROM job_run_attempts_control_plane WHERE tenant_id=$1 AND run_id=$2 ORDER BY attempt_number`, b.Tenant, r.ID)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var a RecoveryAttempt
			if err := rows.Scan(&a.Number, &a.Phase, &a.JobName, &a.JobUID, &a.OwnerUID, &a.CompletedAt); err != nil {
				return err
			}
			r.Attempts = append(r.Attempts, a)
		}
		return rows.Err()
	})
	if err != nil {
		return r, err
	}
	if r.ID == 0 {
		return r, nil
	}
	if r.Tenant != b.Tenant || r.Revision != b.RevisionID || r.SourceUID != b.SourceUID || r.Trigger != "Manual" {
		return r, fmt.Errorf("ledger canary identity differs from manifest")
	}
	var cr workloadsv1alpha1.JobRun
	if err = readRecoveryObject(ctx, "jobrun", b.Namespace, receipt.Name, &cr); err != nil {
		return r, err
	}
	if cr.Namespace != b.Namespace || cr.Name != receipt.Name || cr.UID == "" || cr.Spec.OccurrenceKey != receipt.OccurrenceKey || cr.Spec.DefinitionRevision != b.RevisionID || cr.Spec.ScheduledJobRef.UID != b.SourceUID || string(cr.Spec.Trigger) != "Manual" {
		return r, fmt.Errorf("JobRun canary ownership differs from ledger")
	}
	r.CRUID = string(cr.UID)
	var runList struct {
		Items []workloadsv1alpha1.JobRun `json:"items"`
	}
	if err = readRecoveryObject(ctx, "jobruns", b.Namespace, "", &runList); err != nil {
		return r, err
	}
	matchingRuns := 0
	for _, candidate := range runList.Items {
		if candidate.Spec.ScheduledJobRef.UID == b.SourceUID || candidate.Spec.DefinitionRevision == b.RevisionID {
			matchingRuns++
			if candidate.UID != cr.UID || candidate.Name != cr.Name || candidate.Namespace != b.Namespace {
				return r, fmt.Errorf("canary has an extra or orphaned JobRun outside its receipt")
			}
		}
	}
	if matchingRuns != 1 {
		return r, fmt.Errorf("complete canary JobRun inventory is missing")
	}
	if len(r.Attempts) == 0 {
		return r, nil
	}
	a := r.Attempts[len(r.Attempts)-1]
	if a.JobUID == "" || a.OwnerUID == "" {
		return r, nil
	}
	var job batchv1.Job
	if err = readRecoveryObject(ctx, "job", b.Namespace, a.JobName, &job); err != nil {
		return r, err
	}
	owned := false
	for _, owner := range job.OwnerReferences {
		if owner.Kind == "JobRun" && owner.APIVersion == scheduledJobAPIVersion && owner.Name == cr.Name && string(owner.UID) == a.OwnerUID && a.OwnerUID == r.CRUID && owner.Controller != nil && *owner.Controller {
			owned = true
		}
	}
	if string(job.UID) != a.JobUID || job.Namespace != b.Namespace || !owned || job.Labels["orbitjob.io/revision-id"] != strconv.FormatInt(b.RevisionID, 10) {
		return r, fmt.Errorf("canary Kubernetes Job lacks original immutable ownership")
	}
	containers := job.Spec.Template.Spec.Containers
	if len(containers) != 1 || containers[0].Image != c.Definition.ScheduledJob.Image || !reflect.DeepEqual(containers[0].Command, c.Definition.ScheduledJob.Command) || !reflect.DeepEqual(containers[0].Args, c.Definition.ScheduledJob.Args) {
		return r, fmt.Errorf("owned canary Job payload differs from generated nonce marker")
	}
	r.OwnedJobUID = string(job.UID)
	var jobList batchv1.JobList
	if err = readRecoveryObject(ctx, "jobs", b.Namespace, "", &jobList); err != nil {
		return r, err
	}
	matchingJobs := 0
	for _, candidate := range jobList.Items {
		relevant := candidate.Labels["orbitjob.io/revision-id"] == strconv.FormatInt(b.RevisionID, 10)
		for _, owner := range candidate.OwnerReferences {
			if string(owner.UID) == a.OwnerUID && owner.Kind == "JobRun" {
				relevant = true
			}
		}
		if relevant {
			matchingJobs++
			if candidate.UID != job.UID || candidate.Name != job.Name || candidate.Namespace != b.Namespace {
				return r, fmt.Errorf("canary has an extra physical Job outside its original attempt")
			}
		}
	}
	if matchingJobs != 1 {
		return r, fmt.Errorf("complete canary Job inventory is missing")
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
			r.JobSucceeded = true
		}
	}
	return r, nil
}

type RecoveryBudget struct {
	SLOID       int64     `json:"slo_id"`
	SLIID       int64     `json:"sli_id"`
	SourceUID   string    `json:"source_uid"`
	Version     int       `json:"version"`
	EvaluatedAt time.Time `json:"evaluated_at"`
	EventAt     time.Time `json:"event_at"`
	Good        int64     `json:"good"`
	Total       int64     `json:"total"`
	Consumed    float64   `json:"consumed"`
	Remaining   float64   `json:"remaining"`
	Status      string    `json:"status"`
}

func collectRecoveryBudget(ctx context.Context, db *sql.DB, b PreparedCanary, runID int64) (RecoveryBudget, error) {
	var out RecoveryBudget
	err := withTenantTx(ctx, db, b.Tenant, func(tx *sql.Tx) error {
		// Re-read the declared SLO/SLI configuration and its exact canary event;
		// a generic advancing budget timestamp cannot substitute for these facts.
		err := tx.QueryRowContext(ctx, `SELECT s.id,i.id,i.source_config->>'source_uid'
		 FROM slos s JOIN slis i ON i.id=s.sli_id AND i.tenant_id=s.tenant_id
		 WHERE s.tenant_id=$1 AND s.id=$2 AND i.id=$3 AND s.deleted_at IS NULL AND i.deleted_at IS NULL
		 AND s.status='active' AND s.target=0.99 AND s.window_type='rolling' AND s.window_duration=86400
		 AND i.source_type='job_run' AND i.sli_type='availability' AND i.aggregation='ratio'
		 AND i.good_event_criteria='{"status":"success"}'::jsonb`, b.Tenant, b.SLOID, b.SLIID).Scan(&out.SLOID, &out.SLIID, &out.SourceUID)
		if err != nil {
			return err
		}
		if out.SourceUID != b.SourceUID {
			return fmt.Errorf("scheduler canary monitoring source differs")
		}
		if runID == 0 {
			return nil
		}
		err = tx.QueryRowContext(ctx, `SELECT event_time FROM sli_snapshot_events WHERE tenant_id=$1 AND sli_id=$2 AND run_id=$3 AND is_good=true AND evaluation_state='evaluated'`, b.Tenant, b.SLIID, runID).Scan(&out.EventAt)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(sum(good_events_count),0),COALESCE(sum(total_events_count),0) FROM sli_snapshots WHERE tenant_id=$1 AND sli_id=$2`, b.Tenant, b.SLIID).Scan(&out.Good, &out.Total)
		if err != nil {
			return err
		}
		var evaluated sql.NullTime
		err = tx.QueryRowContext(ctx, `SELECT version,evaluated_at,budget_consumed,budget_remaining,status FROM budgets
		 WHERE tenant_id=$1 AND slo_id=$2 AND last_event_at >= $3 ORDER BY window_start DESC LIMIT 1`, b.Tenant, b.SLOID, out.EventAt).Scan(&out.Version, &evaluated, &out.Consumed, &out.Remaining, &out.Status)
		if err == sql.ErrNoRows {
			return nil
		}
		if evaluated.Valid {
			out.EvaluatedAt = evaluated.Time
		}
		return err
	})
	return out, err
}

func budgetRecovered(b RecoveryBudget, after time.Time) bool {
	return b.SLOID > 0 && b.SLIID > 0 && b.SourceUID != "" && b.Version > 0 && !b.EventAt.IsZero() && !b.EvaluatedAt.Before(b.EventAt) && !b.EvaluatedAt.Before(after) && b.Total == 1 && b.Good == 1 && math.Abs(b.Consumed) < 1e-8 && math.Abs(b.Remaining-0.01) < 1e-8 && b.Status == "healthy"
}
