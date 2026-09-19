package projection

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/revision"
)

// The workflow projection is the front half of the workflow surface: the CR
// becomes an immutable revision the walker decodes the task DAG from. The
// tests pin the revision identity (source_mode, CR UID, generation), the
// tenant routing, and the normalized spec's round trip through exactly the
// decode the walker performs.

func validWorkflowJob() v1alpha1.WorkflowJob {
	return v1alpha1.WorkflowJob{
		ObjectMeta: metav1.ObjectMeta{
			UID: "wf-uid-1", Namespace: "finance", Name: "nightly-pipeline",
			Generation: 4, CreationTimestamp: metav1.Time{Time: time.Unix(2000, 0).UTC()},
		},
		Spec: v1alpha1.WorkflowJobSpec{
			Schedule:   "0 3 * * *",
			FailPolicy: v1alpha1.Continue,
			Tasks: []v1alpha1.WorkflowTaskSpec{
				{Name: "extract", JobRef: v1alpha1.WorkflowTaskJobRef{Name: "extract-job"}},
				{
					Name:      "load",
					JobRef:    v1alpha1.WorkflowTaskJobRef{Name: "load-job"},
					DependsOn: []string{"extract"},
				},
			},
		},
	}
}

func TestApplyWorkflowJobProjectsRevisionForTheWalker(t *testing.T) {
	w := &captureWriter{}
	obj := validWorkflowJob()
	id, err := (Service{Revisions: w, Now: func() time.Time { return time.Unix(3000, 0).UTC() }}).
		ApplyWorkflowJob(context.Background(), obj, "tenant-a", "orbitjob-operator")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 42 || w.calls != 1 || w.tenant != "tenant-a" {
		t.Fatalf("id=%d calls=%d tenant=%q", id, w.calls, w.tenant)
	}

	got := w.got
	// The revision identity a workflow run deduplicates against: the walker
	// resolves a run's WorkflowRef.UID onto this source_uid, and a resync of
	// the same generation must re-project idempotently. The mode is pinned as
	// a literal: the value is a storage contract, not a symbol to rename.
	if got.Identity.SourceMode != "workflow" {
		t.Errorf("source_mode = %q, want workflow", got.Identity.SourceMode)
	}
	if got.Identity.SourceUID != "wf-uid-1" || got.Identity.Namespace != "finance" || got.Identity.Name != "nightly-pipeline" {
		t.Errorf("identity = %+v", got.Identity)
	}
	if got.Generation != 4 {
		t.Errorf("generation = %d, want 4", got.Generation)
	}
	if got.Actor != "orbitjob-operator" {
		t.Errorf("actor = %q", got.Actor)
	}
	if got.ResourceGroupID != "" {
		t.Errorf("resource group = %q, want none for a CR-sourced revision", got.ResourceGroupID)
	}
	if got.CreatedAt != time.Unix(3000, 0).UTC() {
		t.Errorf("created_at = %v", got.CreatedAt)
	}

	// The walker decodes the pinned revision into a WorkflowJobSpec and walks
	// the DAG from it, so the stored bytes must round trip into the declared
	// spec through exactly that decode.
	var decoded v1alpha1.WorkflowJobSpec
	if err := json.Unmarshal([]byte(got.NormalizedSpec), &decoded); err != nil {
		t.Fatalf("walker decode of normalized spec: %v", err)
	}
	if decoded.Schedule != obj.Spec.Schedule || decoded.FailPolicy != obj.Spec.FailPolicy {
		t.Errorf("decoded spec head = %+v", decoded)
	}
	if len(decoded.Tasks) != 2 || decoded.Tasks[1].DependsOn[0] != "extract" {
		t.Errorf("decoded task DAG = %+v", decoded.Tasks)
	}
	for _, want := range []string{"extract-job", "load-job"} {
		if !strings.Contains(got.NormalizedSpec, want) {
			t.Errorf("normalized spec missing task ref %q: %s", want, got.NormalizedSpec)
		}
	}
	// The spec hash is derived from the same bytes every projection writes, so
	// a re-projection of one generation carries a stable hash.
	if got.SpecHash == "" {
		t.Fatal("spec hash is empty")
	}
}

func TestApplyWorkflowJobRejectsIncompleteInput(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*v1alpha1.WorkflowJob)
	}{
		{"no schedule", func(o *v1alpha1.WorkflowJob) { o.Spec.Schedule = "" }},
		{"no tasks", func(o *v1alpha1.WorkflowJob) { o.Spec.Tasks = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := validWorkflowJob()
			tt.mut(&obj)
			if _, err := (Service{Revisions: &captureWriter{}}).ApplyWorkflowJob(context.Background(), obj, "tenant-a", "orbitjob-operator"); err == nil {
				t.Fatal("expected an incomplete spec to be refused")
			}
		})
	}

	if _, err := (Service{Revisions: &captureWriter{}}).ApplyWorkflowJob(context.Background(), validWorkflowJob(), "", "orbitjob-operator"); err == nil {
		t.Fatal("expected an empty tenant to be refused")
	}
	if _, err := (Service{Revisions: nil}).ApplyWorkflowJob(context.Background(), validWorkflowJob(), "tenant-a", "orbitjob-operator"); err == nil {
		t.Fatal("expected a missing revision writer to be refused")
	}
}

func TestApplyWorkflowJobSurfacesWriterError(t *testing.T) {
	w := &captureWriter{err: revision.ErrInvalidIdentity}
	if _, err := (Service{Revisions: w}).ApplyWorkflowJob(context.Background(), validWorkflowJob(), "tenant-a", "orbitjob-operator"); err == nil {
		t.Fatal("expected the writer error to surface")
	}
}
