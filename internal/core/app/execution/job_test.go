package execution

import (
	"errors"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

func identity() Identity {
	return Identity{RunName: "nightly-report", RunUID: "uid-1", RevisionID: "7", Attempt: 2}
}

func TestBuildJobStampsOwnershipAndExecutionSpec(t *testing.T) {
	job := BuildJob(identity(), "finance", Template{
		Image:        "registry.example.com/report:v1",
		Command:      []string{"/bin/report"},
		Args:         []string{"--date", "2026-01-01"},
		BackoffLimit: 3,
	})

	if job.Name != "oj-nightly-report-2" || job.Namespace != "finance" {
		t.Fatalf("unexpected object meta: %s/%s", job.Namespace, job.Name)
	}
	for key, want := range map[string]string{
		LabelRunName:    "nightly-report",
		LabelRunUID:     "uid-1",
		LabelRevisionID: "7",
		LabelAttempt:    "2",
	} {
		if got := job.Labels[key]; got != want {
			t.Errorf("label %s = %q, want %q", key, got, want)
		}
		if got := job.Spec.Template.Labels[key]; got != want {
			t.Errorf("pod label %s = %q, want %q", key, got, want)
		}
	}
	container := job.Spec.Template.Spec.Containers[0]
	if container.Image != "registry.example.com/report:v1" || container.Args[1] != "2026-01-01" {
		t.Fatalf("unexpected container: %+v", container)
	}
	// The platform owns retry; a Pod must never restart itself inside one attempt.
	if job.Spec.Template.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Fatalf("restart policy = %s", job.Spec.Template.Spec.RestartPolicy)
	}
	if job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 3 {
		t.Fatalf("backoff limit = %v", job.Spec.BackoffLimit)
	}
}

func TestBuildJobCopiesTemplateSlices(t *testing.T) {
	args := []string{"--first"}
	job := BuildJob(identity(), "finance", Template{Image: "img", Args: args})
	args[0] = "--mutated"
	if job.Spec.Template.Spec.Containers[0].Args[0] != "--first" {
		t.Fatal("job shares the caller's args slice")
	}
}

func TestBuildJobNameIsDeterministicAndBounded(t *testing.T) {
	first := JobName(identity())
	if first != JobName(identity()) {
		t.Fatal("job name is not deterministic")
	}
	long := Identity{RunName: strings.Repeat("a", 200), Attempt: 3}
	name := JobName(long)
	if len(name) > maxJobNameLength {
		t.Fatalf("job name length %d exceeds %d: %s", len(name), maxJobNameLength, name)
	}
	if name != JobName(long) {
		t.Fatal("hashed job name is not deterministic")
	}
	if JobName(Identity{RunName: "run", Attempt: 1}) == JobName(Identity{RunName: "run", Attempt: 2}) {
		t.Fatal("attempts must produce distinct job names")
	}
}

func TestPhaseReadsTerminalConditions(t *testing.T) {
	tests := []struct {
		name   string
		status batchv1.JobStatus
		want   string
	}{
		{"no observations yet", batchv1.JobStatus{}, PhasePending},
		{"active pod", batchv1.JobStatus{Active: 1}, PhaseRunning},
		{"failed pod while still retrying", batchv1.JobStatus{Failed: 1, Active: 1}, PhaseRunning},
		{"failed pod awaiting replacement", batchv1.JobStatus{Failed: 1}, PhasePending},
		{"partial parallel success", batchv1.JobStatus{Succeeded: 1, Active: 1}, PhaseRunning},
		{"complete condition", batchv1.JobStatus{Conditions: []batchv1.JobCondition{
			{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}}, PhaseSucceeded},
		{"failed condition", batchv1.JobStatus{Conditions: []batchv1.JobCondition{
			{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}}, PhaseFailed},
		{"complete condition false", batchv1.JobStatus{Conditions: []batchv1.JobCondition{
			{Type: batchv1.JobComplete, Status: corev1.ConditionFalse}}}, PhasePending},
		{"failure target is not terminal", batchv1.JobStatus{Active: 1, Conditions: []batchv1.JobCondition{
			{Type: batchv1.JobFailureTarget, Status: corev1.ConditionTrue}}}, PhaseRunning},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Phase(batchv1.Job{Status: tt.status}); got != tt.want {
				t.Fatalf("phase = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestValidateOwnership(t *testing.T) {
	run := identity()
	tests := []struct {
		name    string
		mutate  func(*batchv1.Job)
		wantErr bool
	}{
		{"matching job", func(*batchv1.Job) {}, false},
		{"different run name", func(j *batchv1.Job) { j.Labels[LabelRunName] = "other" }, true},
		{"recreated run with same name", func(j *batchv1.Job) { j.Labels[LabelRunUID] = "uid-2" }, true},
		{"different attempt", func(j *batchv1.Job) { j.Labels[LabelAttempt] = "3" }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := BuildJob(run, "finance", Template{Image: "img"})
			tt.mutate(job)
			err := ValidateOwnership(job, run)
			if tt.wantErr && !errors.Is(err, ErrForeignJob) {
				t.Fatalf("error = %v, want ErrForeignJob", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
	if err := ValidateOwnership(nil, run); err == nil {
		t.Fatal("nil job must be rejected")
	}
}

func TestIdentityFromJob(t *testing.T) {
	job := BuildJob(identity(), "finance", Template{Image: "img"})
	got, err := IdentityFromJob(job)
	if err != nil {
		t.Fatal(err)
	}
	if got != identity() {
		t.Fatalf("identity = %+v, want %+v", got, identity())
	}

	unmanaged := &batchv1.Job{}
	if _, err := IdentityFromJob(unmanaged); err == nil {
		t.Fatal("unmanaged job must be rejected")
	}
	badAttempt := BuildJob(identity(), "finance", Template{Image: "img"})
	badAttempt.Labels[LabelAttempt] = "not-a-number"
	if _, err := IdentityFromJob(badAttempt); err == nil {
		t.Fatal("non-numeric attempt label must be rejected")
	}
	zeroAttempt := BuildJob(identity(), "finance", Template{Image: "img"})
	zeroAttempt.Labels[LabelAttempt] = "0"
	if _, err := IdentityFromJob(zeroAttempt); err == nil {
		t.Fatal("attempt 0 must be rejected")
	}
}
