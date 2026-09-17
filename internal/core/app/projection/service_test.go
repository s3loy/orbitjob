package projection

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/revision"
)

type captureWriter struct {
	tenant string
	got    revision.Revision
	calls  int
	err    error
}

func (w *captureWriter) ApplyRevisionForTenant(_ context.Context, tenantID string, rev revision.Revision) (int64, error) {
	w.calls++
	w.tenant = tenantID
	w.got = rev
	return 42, w.err
}

func validScheduledJob() v1alpha1.ScheduledJob {
	return v1alpha1.ScheduledJob{
		ObjectMeta: metav1.ObjectMeta{
			UID: "uid-1", Namespace: "finance", Name: "report",
			Generation: 3, CreationTimestamp: metav1.Time{Time: time.Unix(1000, 0).UTC()},
		},
		Spec: v1alpha1.ScheduledJobSpec{
			Schedule: "0 2 * * *",
			JobTemplate: v1alpha1.JobTemplateSpec{
				Image: "registry.example.com/report:v1", Command: []string{"/bin/report"},
			},
		},
	}
}

func TestApplyScheduledJobProjectsFullSpec(t *testing.T) {
	w := &captureWriter{}
	id, err := (Service{Revisions: w}).ApplyScheduledJob(context.Background(), validScheduledJob(), "tenant-a", "operator")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 42 || w.calls != 1 || w.tenant != "tenant-a" {
		t.Fatalf("id=%d calls=%d tenant=%q", id, w.calls, w.tenant)
	}
	// The revision must carry everything a run needs to build its Job, not a
	// hand-picked subset of the spec.
	for _, want := range []string{"0 2 * * *", "registry.example.com/report:v1", "/bin/report"} {
		if !strings.Contains(w.got.NormalizedSpec, want) {
			t.Errorf("normalized spec missing %q: %s", want, w.got.NormalizedSpec)
		}
	}
	if w.got.Identity.SourceUID != "uid-1" || w.got.Generation != 3 || w.got.Actor != "operator" {
		t.Fatalf("unexpected revision: %+v", w.got)
	}
}

func TestApplyScheduledJobIsDeterministic(t *testing.T) {
	first := &captureWriter{}
	second := &captureWriter{}
	svc := Service{Revisions: first}
	if _, err := svc.ApplyScheduledJob(context.Background(), validScheduledJob(), "t", "op"); err != nil {
		t.Fatal(err)
	}
	svc2 := Service{Revisions: second}
	if _, err := svc2.ApplyScheduledJob(context.Background(), validScheduledJob(), "t", "op"); err != nil {
		t.Fatal(err)
	}
	if first.got.SpecHash != second.got.SpecHash {
		t.Fatalf("spec hash not stable: %s vs %s", first.got.SpecHash, second.got.SpecHash)
	}
}

func TestApplyScheduledJobRejectsIncompleteInput(t *testing.T) {
	noImage := validScheduledJob()
	noImage.Spec.JobTemplate.Image = ""
	noSchedule := validScheduledJob()
	noSchedule.Spec.Schedule = ""
	noUID := validScheduledJob()
	noUID.UID = ""
	zeroGen := validScheduledJob()
	zeroGen.Generation = 0

	tests := []struct {
		name   string
		obj    v1alpha1.ScheduledJob
		tenant string
		svc    Service
	}{
		{"missing tenant", validScheduledJob(), "", Service{Revisions: &captureWriter{}}},
		{"missing writer", validScheduledJob(), "t", Service{}},
		{"missing uid", noUID, "t", Service{Revisions: &captureWriter{}}},
		{"zero generation", zeroGen, "t", Service{Revisions: &captureWriter{}}},
		{"missing schedule", noSchedule, "t", Service{Revisions: &captureWriter{}}},
		{"missing image", noImage, "t", Service{Revisions: &captureWriter{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.svc.ApplyScheduledJob(context.Background(), tt.obj, tt.tenant, "op"); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestApplyScheduledJobPropagatesWriterError(t *testing.T) {
	w := &captureWriter{err: errors.New("conflict")}
	if _, err := (Service{Revisions: w}).ApplyScheduledJob(context.Background(), validScheduledJob(), "t", "op"); err == nil {
		t.Fatal("expected writer error to propagate")
	}
}

func TestApplyScheduledJobUsesInjectedClock(t *testing.T) {
	// The revision is created now, not when the CR was created: re-projecting an
	// old resource must not backdate the revision it produces.
	fixed := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	w := &captureWriter{}
	svc := Service{Revisions: w, Now: func() time.Time { return fixed }}
	if _, err := svc.ApplyScheduledJob(context.Background(), validScheduledJob(), "t", "op"); err != nil {
		t.Fatal(err)
	}
	if !w.got.CreatedAt.Equal(fixed) {
		t.Fatalf("created at = %s, want %s", w.got.CreatedAt, fixed)
	}

	// With no clock injected the wall clock is used, and it must not be zero.
	fallback := &captureWriter{}
	if _, err := (Service{Revisions: fallback}).ApplyScheduledJob(context.Background(), validScheduledJob(), "t", "op"); err != nil {
		t.Fatal(err)
	}
	if fallback.got.CreatedAt.IsZero() {
		t.Fatal("revision created without a timestamp")
	}
}

func TestNormalizeSpecIsLosslessAndDeterministic(t *testing.T) {
	spec := v1alpha1.ScheduledJobSpec{
		Schedule:          "0 2 * * *",
		ConcurrencyPolicy: v1alpha1.Forbid,
		MisfirePolicy:     v1alpha1.Skip,
		JobTemplate: v1alpha1.JobTemplateSpec{
			Image: "img", Command: []string{"/bin/sh"}, Args: []string{"-c", "true"}, BackoffLimit: 4,
		},
		RetryPolicy: v1alpha1.RetryPolicy{MaxAttempts: 3},
		History:     v1alpha1.HistoryPolicy{SuccessfulRuns: 1, FailedRuns: 2},
	}
	first := normalizeSpec(spec)
	second := normalizeSpec(spec)
	if first != second {
		t.Fatal("normalization is not deterministic")
	}
	// Anything the run needs to build its Job must survive normalization.
	for _, fragment := range []string{"0 2 * * *", "Forbid", "Skip", "img", "/bin/sh", "-c", "true", "4", "3"} {
		if !strings.Contains(first, fragment) {
			t.Errorf("normalized spec dropped %q: %s", fragment, first)
		}
	}
}

func TestApplyScheduledJobAcceptsMinimalValidSpec(t *testing.T) {
	// Only schedule and image are required; every optional field must be
	// omittable without the projection refusing the definition.
	w := &captureWriter{}
	obj := validScheduledJob()
	obj.Spec = v1alpha1.ScheduledJobSpec{
		Schedule:    "0 2 * * *",
		JobTemplate: v1alpha1.JobTemplateSpec{Image: "img"},
	}
	if _, err := (Service{Revisions: w}).ApplyScheduledJob(context.Background(), obj, "t", "op"); err != nil {
		t.Fatal(err)
	}
	if w.got.SpecHash == "" {
		t.Fatal("no spec hash recorded")
	}
}
