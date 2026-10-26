package execution

import (
	"errors"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIdentityFromHashedRunLabel(t *testing.T) {
	longName := "function-1-" + strings.Repeat("a", 64)
	for _, tc := range []struct {
		name, fullName string
		mismatch       bool
	}{
		{name: "legacy short name", fullName: "task-key"},
		{name: "complete annotated name", fullName: longName},
		{name: "annotation and label disagree", fullName: longName, mismatch: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{LabelRunName: RunLabel(tc.fullName), LabelRunUID: "run-uid", LabelRevisionID: "9", LabelAttempt: "2"}}}
			if len(tc.fullName) > 63 {
				job.Annotations = map[string]string{AnnotationRunName: tc.fullName}
			}
			if tc.mismatch {
				job.Annotations[AnnotationRunName] = "other-run"
			}
			got, err := IdentityFromJob(job)
			if tc.mismatch {
				if !errors.Is(err, ErrForeignJob) {
					t.Fatalf("mismatched identity accepted: %+v %v", got, err)
				}
				return
			}
			if err != nil || got.RunName != tc.fullName || got.RunUID != "run-uid" || got.RevisionID != "9" || got.Attempt != 2 {
				t.Fatalf("identity not restored: %+v %v", got, err)
			}
			if err := ValidateOwnership(job, got); err != nil {
				t.Fatal(err)
			}
		})
	}
}
