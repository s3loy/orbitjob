//go:build integration

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	workloadsv1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/sloevaluate"
	"orbitjob/internal/core/domain/sli"
	corepostgres "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/postgrestest"
	"orbitjob/internal/platform/scan"
)

func TestRecoveryCollectorsOnMigratedPostgresAndRealJournal(t *testing.T) {
	db := postgrestest.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	c, b, x, _, _ := recoveryCase(t, "scheduler")
	c.Nonce = recoveryDigest(c.Nonce)
	cfg := Config{RunID: "run", Duration: 2 * time.Minute, Tenants: []string{b.Tenant}, Faults: FaultsConfig{Plan: []FaultPhase{{Name: "scheduler", Offset: time.Minute}}}}
	c.Definition = recoveryDefinition(cfg, c.Fault, c.Stage, c.Nonce, c.Definition.ScheduledJob.Image)
	b.CaseID = c.Definition.CaseID
	x.CaseID = b.CaseID
	x.Trigger.OccurrenceKey = c.Nonce
	x.Trigger.Name = jobRunCRName(b.CaseID, c.Nonce)
	fixture := startJournalFixture(t, false)
	initial, err := readJournalSnapshot(ctx, fixture, cfg.RunID)
	if err != nil {
		t.Fatal(err)
	}
	start := initial.Cursor()
	windowStart := time.Now()
	before := RecoveryTarget{UID: scan.GenerateID(), Generation: 1, PodUIDs: []string{scan.GenerateID()}}
	after := RecoveryTarget{UID: before.UID, Generation: 2, ObservedGeneration: 2, Desired: 1, Ready: 1, Updated: 1, PodUIDs: []string{scan.GenerateID()}}
	m := RecoveryManifest{Version: 1, RunID: cfg.RunID, ConfigSHA256: configSHA256(cfg), Canaries: []RecoveryCanary{c}}
	prober := RecoveryProber{DB: db, Manifest: m, FixtureURL: fixture}
	if _, err := prober.checkpoint(ctx, 0, "fault", &before, nil, nil); err != nil {
		t.Fatal(err)
	}
	injectedAt := time.Now()
	if _, err := prober.checkpoint(ctx, 0, "restored", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := prober.checkpoint(ctx, 0, "ready", &after, nil, nil); err != nil {
		t.Fatal(err)
	}
	submission, err := prober.checkpoint(ctx, 0, "submit", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	x.SubmittedAfter = submission.DatabaseTime
	if _, err := db.ExecContext(ctx, `INSERT INTO tenants(id,slug,name,metadata)VALUES($1,'load-recovery','recovery',$2)`, b.Tenant, string(mustJSON(map[string]string{loadManagedByLabelKey: loadManagedByLabelValue}))); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO job_definition_revisions(tenant_id,source_mode,source_uid,source_namespace,source_name,generation,spec_hash,normalized_spec,is_active,actor)
	 VALUES($1,'kubernetes',$2,$3,$4,1,repeat('a',64),'{}',true,'operator') RETURNING id`, b.Tenant, b.SourceUID, b.Namespace, b.CaseID).Scan(&b.RevisionID); err != nil {
		t.Fatal(err)
	}
	var runID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO job_run_control_plane(tenant_id,source_uid,revision_id,occurrence_key,trigger,actor,phase,attempt,max_attempts)VALUES($1,$2,$3,$4,'Manual','loadtest','Succeeded',1,1) RETURNING id`, b.Tenant, b.SourceUID, b.RevisionID, c.Nonce).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	jobUID, ownerUID := scan.GenerateID(), scan.GenerateID()
	response, err := http.Post(fixture+"/ledger", "application/json", strings.NewReader(string(mustJSON(map[string]string{"case_id": b.CaseID, "operation": "recovery", "idempotency_key": c.Nonce}))))
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if _, err := db.ExecContext(ctx, `INSERT INTO job_run_attempts_control_plane(tenant_id,run_id,attempt_number,phase,kubernetes_job_name,kubernetes_job_uid,owner_run_uid,completed_at)VALUES($1,$2,1,'Succeeded','recovery-attempt',$3,$4,clock_timestamp())`, b.Tenant, runID, jobUID, ownerUID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO slis(tenant_id,name,sli_type,source_type,source_config,aggregation,good_event_criteria)VALUES($1,'recovery','availability','job_run',$2,'ratio','{"status":"success"}')RETURNING id`, b.Tenant, string(mustJSON(map[string]string{"source_uid": b.SourceUID}))).Scan(&b.SLIID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO slos(tenant_id,name,sli_id,target,window_type,window_duration)VALUES($1,'recovery',$2,0.99,'rolling',86400)RETURNING id`, b.Tenant, b.SLIID).Scan(&b.SLOID); err != nil {
		t.Fatal(err)
	}
	var eventAt time.Time
	if err := db.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&eventAt); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO sli_event_plans(tenant_id,run_id,source_uid,semantics,event_time,time_quality)VALUES($1,$2,$3,'event_time_v2',$4,'observed')`, b.Tenant, runID, b.SourceUID, eventAt); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO sli_event_plan_members(tenant_id,run_id,sli_id,configuration_version)VALUES($1,$2,$3,1)`, b.Tenant, runID, b.SLIID); err != nil {
		t.Fatal(err)
	}
	good := true
	snapshots := corepostgres.NewSLISnapshotRepository(db)
	if created, err := snapshots.RecordEvaluation(ctx, b.Tenant, sli.Evaluation{RunID: runID, SLIID: b.SLIID, ConfigurationVersion: 1, Semantics: sli.EventTime, EventTime: eventAt, TimeQuality: sli.TimeObserved, WindowStart: eventAt.UTC().Truncate(5 * time.Minute), Good: &good}); err != nil || !created {
		t.Fatal("record actual canary SLI", created, err)
	}
	evaluator := sloevaluate.NewEvaluateUseCase(corepostgres.NewSLORepository(db), snapshots, corepostgres.NewBudgetRepository(db), corepostgres.NewBudgetAlertRepository(db))
	if err := evaluator.EvaluateSLO(ctx, b.Tenant, b.SLOID); err != nil {
		t.Fatal(err)
	}
	budget, err := collectRecoveryBudget(ctx, db, b, runID)
	if err != nil || !budgetRecovered(budget, x.SubmittedAfter) {
		t.Fatalf("actual scheduler result not recovered: %+v %v", budget, err)
	}
	x.Budget = &budget
	controller := true
	source := recoverySource{Metadata: metav1.ObjectMeta{Name: b.CaseID, Namespace: b.Namespace, UID: types.UID(b.SourceUID)}}
	source.Status.ActiveRevision = b.RevisionID
	cr := workloadsv1alpha1.JobRun{ObjectMeta: metav1.ObjectMeta{Name: x.Trigger.Name, Namespace: b.Namespace, UID: types.UID(ownerUID)}}
	cr.Spec.OccurrenceKey = c.Nonce
	cr.Spec.DefinitionRevision = b.RevisionID
	cr.Spec.ScheduledJobRef.UID = b.SourceUID
	cr.Spec.Trigger = "Manual"
	job := batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "recovery-attempt", Namespace: b.Namespace, UID: types.UID(jobUID), Labels: map[string]string{"orbitjob.io/revision-id": jsonNumber(b.RevisionID)}, OwnerReferences: []metav1.OwnerReference{{APIVersion: scheduledJobAPIVersion, Kind: "JobRun", Name: cr.Name, UID: cr.UID, Controller: &controller}}}, Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "task", Image: c.Definition.ScheduledJob.Image, Command: c.Definition.ScheduledJob.Command, Args: c.Definition.ScheduledJob.Args}}}}}, Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}}}
	bin := t.TempDir()
	replicas := int32(1)
	deployment := appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "orbitjob-scheduler", Namespace: WorkloadNamespace(), UID: types.UID(after.UID), Generation: after.Generation}, Spec: appsv1.DeploymentSpec{Replicas: &replicas, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "target"}}}, Status: appsv1.DeploymentStatus{ObservedGeneration: 2, ReadyReplicas: 1, UpdatedReplicas: 1}}
	pods := corev1.PodList{Items: []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "target", Namespace: WorkloadNamespace(), UID: types.UID(after.PodUIDs[0]), Labels: map[string]string{"app": "target"}}}}}
	runList := struct {
		Items []workloadsv1alpha1.JobRun `json:"items"`
	}{Items: []workloadsv1alpha1.JobRun{cr}}
	jobList := batchv1.JobList{Items: []batchv1.Job{job}}
	for name, object := range map[string]any{"scheduledjob": source, "jobrun": cr, "jobruns": runList, "job": job, "jobs": jobList, "deployment": deployment, "pods": pods} {
		if err := os.WriteFile(filepath.Join(bin, name+".json"), mustJSON(object), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte("#!/bin/sh\ncase \"$2\" in scheduledjob|jobrun|jobruns|job|jobs|deployment|pods) exec /bin/cat \"$RECOVERY_KUBE_DIR/$2.json\";; *) exit 2;; esac\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("RECOVERY_KUBE_DIR", bin)
	current, err := collectRecoveryRun(ctx, db, c, b, x.Trigger)
	if err != nil || !canaryRunRecovered(current, b, x.Trigger) {
		t.Fatalf("real ledger collector: %+v %v", current, err)
	}
	x.Observed = current
	if _, err := prober.checkpoint(ctx, 0, "observed", nil, &x.Observed, x.Budget); err != nil {
		t.Fatal(err)
	}
	journal, err := readJournalSnapshot(ctx, fixture, cfg.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if status, message := evaluateRecoveryCanary(c, b, x, current, &budget, journal, start); status != VerdictPass {
		t.Fatal(status, message)
	}
	// Exercise the actual artifact-loading and read-only verifier path too.
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "generated"), 0700); err != nil {
		t.Fatal(err)
	}
	p := PreparedRecovery{ManifestSHA256: recoveryDigest(m), Canaries: []PreparedCanary{b}}
	if err := writeStableJSON(filepath.Join(dir, "generated", "recovery.json"), m); err != nil {
		t.Fatal(err)
	}
	if err := writeStableJSON(filepath.Join(dir, "recovery-prepared.json"), p); err != nil {
		t.Fatal(err)
	}
	receipt := &FaultRecoveryReceipt{ManifestSHA256: recoveryDigest(m), Before: before, After: after, JournalStart: start, JournalEnd: journal.Cursor(), Canaries: []CanaryReceipt{x}, BusinessElapsedSeconds: 1}
	stats := RunStats{RunID: cfg.RunID, StartedAt: windowStart, FinishedAt: time.Now(), JournalStart: &start, FaultPlan: activeFaultPlan(cfg), Faults: []FaultRecord{{Name: "scheduler", ScheduledOffset: time.Minute, InjectedAt: injectedAt.Format(time.RFC3339Nano), InjectionSucceeded: true, Ready: true, Recovery: receipt}}}
	var searchPath string
	if err := db.QueryRowContext(ctx, "SHOW search_path").Scan(&searchPath); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(postgrestest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", searchPath)
	u.RawQuery = query.Encode()
	checks := collectFaultRecoveryChecks(ctx, dir, cfg, stats, u.String(), journal)
	if check := checks[faultCheckID(c.Fault)]; check.Status != VerdictPass {
		t.Fatalf("independent final verification: %+v", check)
	}
	for _, negative := range []string{"wrong tenant", "wrong source", "wrong Job UID", "wrong payload", "stale budget", "source error", "interrupted", "self-report only", "extra occurrence", "extra JobRun", "extra physical Job", "forged submission time", "fault after success"} {
		t.Run(negative, func(t *testing.T) {
			binding := b
			candidate := job
			changedStats := stats
			copyReceipt := *receipt
			copyReceipt.Canaries = append([]CanaryReceipt(nil), receipt.Canaries...)
			changedStats.Faults = append([]FaultRecord(nil), stats.Faults...)
			changedStats.Faults[0].Recovery = &copyReceipt
			switch negative {
			case "extra occurrence":
				var extraID int64
				if err := db.QueryRowContext(ctx, `INSERT INTO job_run_control_plane(tenant_id,source_uid,revision_id,occurrence_key,trigger,actor,phase)VALUES($1,$2,$3,$4,'Manual','loadtest','Succeeded')RETURNING id`, b.Tenant, b.SourceUID, b.RevisionID, recoveryDigest("extra occurrence")).Scan(&extraID); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM job_run_control_plane WHERE id=$1`, extraID) })
			case "extra JobRun":
				extra := cr
				extra.Name = "extra-jobrun"
				extra.UID = types.UID(scan.GenerateID())
				extra.Spec.OccurrenceKey = recoveryDigest("extra CR")
				list := runList
				list.Items = append(append([]workloadsv1alpha1.JobRun(nil), runList.Items...), extra)
				if err := os.WriteFile(filepath.Join(bin, "jobruns.json"), mustJSON(list), 0600); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.WriteFile(filepath.Join(bin, "jobruns.json"), mustJSON(runList), 0600) })
			case "extra physical Job":
				extra := job
				extra.Name = "duplicate-job"
				extra.UID = types.UID(scan.GenerateID())
				list := jobList
				list.Items = append(append([]batchv1.Job(nil), jobList.Items...), extra)
				if err := os.WriteFile(filepath.Join(bin, "jobs.json"), mustJSON(list), 0600); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.WriteFile(filepath.Join(bin, "jobs.json"), mustJSON(jobList), 0600) })
			case "forged submission time":
				copyReceipt.Canaries[0].SubmittedAfter = x.SubmittedAfter.Add(time.Second)
			case "fault after success":
				changedStats.Faults[0].InjectedAt = stats.FinishedAt.Add(time.Second).Format(time.RFC3339Nano)
			case "wrong tenant":
				binding.Tenant = scan.GenerateID()
			case "wrong source":
				binding.SourceUID = scan.GenerateID()
			case "wrong Job UID":
				candidate.UID = types.UID(scan.GenerateID())
			case "wrong payload":
				candidate.Spec.Template.Spec.Containers = append([]corev1.Container(nil), job.Spec.Template.Spec.Containers...)
				candidate.Spec.Template.Spec.Containers[0].Args = []string{"foreign-marker"}
			case "stale budget":
				if _, err := db.ExecContext(ctx, `UPDATE budgets SET evaluated_at=$1 WHERE slo_id=$2`, x.SubmittedAfter.Add(-time.Hour), b.SLOID); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					_, _ = db.ExecContext(ctx, `UPDATE budgets SET evaluated_at=$1 WHERE slo_id=$2`, budget.EvaluatedAt, b.SLOID)
				})
			case "source error":
				if err := os.WriteFile(filepath.Join(bin, "job.json"), []byte("bad-json"), 0600); err != nil {
					t.Fatal(err)
				}
			case "interrupted":
				copyReceipt := *receipt
				copyReceipt.Interrupted = true
				changedStats.Faults = append([]FaultRecord(nil), stats.Faults...)
				changedStats.Faults[0].Recovery = &copyReceipt
			case "self-report only":
				changedStats.Faults = append([]FaultRecord(nil), stats.Faults...)
				changedStats.Faults[0].Recovery = nil
				changedStats.Faults[0].BusinessRecovered = true
				changedStats.Faults[0].BusinessEvidence = "healthy"
			}
			if negative == "wrong Job UID" || negative == "wrong payload" {
				if err := os.WriteFile(filepath.Join(bin, "job.json"), mustJSON(candidate), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if negative == "wrong tenant" || negative == "wrong source" || negative == "wrong Job UID" || negative == "wrong payload" || negative == "source error" {
				observed, err := collectRecoveryRun(ctx, db, c, binding, x.Trigger)
				if err == nil && canaryRunRecovered(observed, binding, x.Trigger) {
					t.Fatal("foreign/error source recovered")
				}
			} else {
				check := collectFaultRecoveryChecks(ctx, dir, cfg, changedStats, u.String(), journal)[faultCheckID(c.Fault)]
				if check.Status == VerdictPass {
					t.Fatal("incomplete recovery passed", negative)
				}
			}
			if err := os.WriteFile(filepath.Join(bin, "job.json"), mustJSON(job), 0600); err != nil {
				t.Fatal(err)
			}
		})
	}
	// New SLI configuration/event-plan facts must not make owned load cleanup
	// fail, and the unrelated tenant must retain its independent history.
	other := scan.GenerateID()
	if _, err := db.ExecContext(ctx, `INSERT INTO tenants(id,slug,name)VALUES($1,'unrelated','unrelated')`, other); err != nil {
		t.Fatal(err)
	}
	for _, step := range resetSteps() {
		if _, err := db.ExecContext(ctx, step.cmd[len(step.cmd)-1]); err != nil {
			t.Fatalf("owned canary cleanup %s: %v", step.name, err)
		}
	}
	var owned, foreign int
	if err := db.QueryRowContext(ctx, `SELECT count(*)FILTER(WHERE id=$1),count(*)FILTER(WHERE id=$2)FROM tenants`, b.Tenant, other).Scan(&owned, &foreign); err != nil || owned != 0 || foreign != 1 {
		t.Fatal("cleanup ownership", owned, foreign, err)
	}
}

func jsonNumber(n int64) string { data, _ := json.Marshal(n); return string(data) }
