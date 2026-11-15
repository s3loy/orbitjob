package operator

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"

	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
	"orbitjob/internal/core/domain/workflow"
)

func snapshotWalkerRun(t *testing.T, revs *fakeWorkflowRevisions) workflow.Run {
	t.Helper()
	run := walkerRun()
	run.SnapshotVersion = 1
	run.TaskSnapshots = map[string]workflow.TaskSnapshot{}
	for _, def := range revs.active {
		task := "extract"
		if def.Identity.Name == "load-job" {
			task = "load"
		}
		run.TaskSnapshots[task] = workflow.TaskSnapshot{SourceUID: def.Identity.SourceUID, RevisionID: def.ID, Namespace: def.Identity.Namespace, Name: def.Identity.Name, Image: "registry.example.com/step:v1"}
	}
	return run
}

type snapshotRecoveryRuns struct {
	*recoveryRuns
	accepted bool
	err      error
}

func (s *snapshotRecoveryRuns) SnapshotWorkflowStep(context.Context, string, int64, int64) (bool, error) {
	return s.accepted, s.err
}

func TestOnlyVerifiedSnapshotStepCanDispatchAfterSourceDeletion(t *testing.T) {
	for _, c := range []struct {
		name       string
		accepted   bool
		trigger    v1alpha1.Trigger
		err        error
		wantCreate bool
	}{
		{name: "accepted graph", accepted: true, trigger: v1alpha1.Workflow, wantCreate: true},
		{name: "legacy step", trigger: v1alpha1.Workflow},
		{name: "proof unavailable", trigger: v1alpha1.Workflow, err: errors.New("database unavailable")},
		{name: "manual run", accepted: true, trigger: v1alpha1.Manual},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, s, jobs := recoveryFixture(t)
			s.stored.Phase, s.stored.Attempt = jobrun.Pending, 0
			s.stored.AttemptUID, s.stored.AttemptJobName, s.stored.AttemptPhase = "", "", ""
			jobs.job = nil
			r.Runs = &snapshotRecoveryRuns{recoveryRuns: s, accepted: c.accepted, err: c.err}
			if err := r.Dynamic.Resource(scheduledJobGVR).Namespace("finance").Delete(t.Context(), "nightly-report", metav1.DeleteOptions{}); err != nil {
				t.Fatal(err)
			}
			obj, err := r.Dynamic.Resource(jobRunGVR).Namespace("finance").Get(t.Context(), v1alpha1.RunObjectName("nightly-report", "occ-1"), metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			obj.Object["spec"].(map[string]any)["trigger"] = string(c.trigger)
			if _, err := r.Dynamic.Resource(jobRunGVR).Namespace("finance").Update(t.Context(), obj, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			err = r.ReconcileJobRun(t.Context(), *obj)
			if c.wantCreate {
				if err != nil || len(jobs.created) != 1 || len(s.attempts) != 1 {
					t.Fatalf("accepted step did not dispatch: err=%v jobs=%d attempts=%d", err, len(jobs.created), len(s.attempts))
				}
			} else if err == nil || len(jobs.created) != 0 || len(s.attempts) != 0 {
				t.Fatalf("unverified run dispatched: err=%v jobs=%d attempts=%d", err, len(jobs.created), len(s.attempts))
			}
		})
	}
}

func TestWorkflowSnapshotSurvivesReferenceDeletionAndReplacement(t *testing.T) {
	revs := walkerRevisions(t)
	run := snapshotWalkerRun(t, revs)
	// The root already finished; changing the unstarted dependent's active
	// pointer and recreating its name cannot change this parent's choice.
	runs := &fakeWorkflowRuns{stored: run, steps: []workflow.StepRun{stepFor(t, "extract", jobrun.Succeeded, 1)}}
	replacement := withIdentity(t, workflowRevision(t, 30, jobSpecJSON(t)), "kubernetes", "replacement-load-uid", "finance", "load-job")
	revs.active = []revision.Revision{replacement}
	pub := &fakeWorkflowPublisher{}
	w, _ := newWalker(t, runs, revs, pub)
	created, err := w.Tick(t.Context())
	if err != nil || created != 1 || len(runs.createdStep) != 1 {
		t.Fatalf("created=%d steps=%+v err=%v", created, runs.createdStep, err)
	}
	if step := runs.createdStep[0]; step.SourceUID != "load-job-uid" || step.RevisionID != 11 {
		t.Fatalf("replacement changed accepted task: %+v", step)
	}
	if len(pub.published) != 1 || pub.published[0].Spec.DefinitionRevision != 11 {
		t.Fatalf("wrong pinned publication: %+v", pub.published)
	}
}

func TestWorkflowSnapshotMissingHistoryNeverUsesLatest(t *testing.T) {
	revs := walkerRevisions(t)
	run := snapshotWalkerRun(t, revs)
	delete(revs.byID, 11)
	revs.active = []revision.Revision{withIdentity(t, workflowRevision(t, 30, jobSpecJSON(t)), "kubernetes", "load-job-uid", "finance", "load-job")}
	runs := &fakeWorkflowRuns{stored: run}
	pub := &fakeWorkflowPublisher{}
	w, _ := newWalker(t, runs, revs, pub)
	created, err := w.advanceRun(t.Context(), executionTenant, run, definitionIndex(revs.active))
	if err == nil || created != 0 || len(runs.createdStep) != 0 || len(pub.published) != 0 || len(runs.phases) != 0 {
		t.Fatalf("missing history was substituted: created=%d err=%v steps=%+v", created, err, runs.createdStep)
	}
}

func TestWorkflowSnapshotRequiresCompleteTaskSet(t *testing.T) {
	revs := walkerRevisions(t)
	run := snapshotWalkerRun(t, revs)
	delete(run.TaskSnapshots, "load")
	w, _ := newWalker(t, &fakeWorkflowRuns{stored: run}, revs, &fakeWorkflowPublisher{})
	if _, err := w.snapshotDefinitions(t.Context(), executionTenant, run, workflow.DAGFromSpec(twoTaskSpec()), "finance"); err == nil {
		t.Fatal("partial snapshot accepted")
	}
}
