package execution

import (
	"context"
	"errors"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
)

func TestBuildJobClampsNegativeBackoffAndAppliesDeadline(t *testing.T) {
	job := BuildJob(Identity{RunName: "run", Attempt: 1}, "finance", Template{
		Image: "img", BackoffLimit: -1, ActiveDeadlineSeconds: 900,
	})
	if job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 0 {
		t.Fatalf("backoff = %v, want 0", job.Spec.BackoffLimit)
	}
	if job.Spec.ActiveDeadlineSeconds == nil || *job.Spec.ActiveDeadlineSeconds != 900 {
		t.Fatalf("deadline = %v", job.Spec.ActiveDeadlineSeconds)
	}

	// No deadline requested means none set: Kubernetes treats an absent field as
	// unbounded, which is the intended default.
	noDeadline := BuildJob(Identity{RunName: "run", Attempt: 1}, "finance", Template{Image: "img"})
	if noDeadline.Spec.ActiveDeadlineSeconds != nil {
		t.Fatal("deadline must be omitted when not requested")
	}
}

func TestSanitizeBoundsAndTrims(t *testing.T) {
	tests := []struct {
		name  string
		input string
		limit int
		want  string
	}{
		{"within limit", "report", 10, "report"},
		{"trimmed to limit", "nightly-report", 7, "nightly"},
		{"trailing dash removed after trim", "abc-def", 4, "abc"},
		{"zero limit yields nothing", "report", 0, ""},
		{"negative limit yields nothing", "report", -1, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitize(tt.input, tt.limit); got != tt.want {
				t.Fatalf("sanitize(%q, %d) = %q, want %q", tt.input, tt.limit, got, tt.want)
			}
		})
	}
}

func TestIdentityFromJobRejectsNil(t *testing.T) {
	if _, err := IdentityFromJob(nil); err == nil {
		t.Fatal("nil job must be rejected")
	}
}

func TestEnsureSurfacesErrorWhenAdoptingAfterRace(t *testing.T) {
	// The create raced, but the follow-up read also fails: the error must
	// surface rather than being swallowed as "adopted".
	client := &failingAdoptClient{}
	if _, err := (Adapter{Client: client}).Ensure(context.Background(), desiredJob()); err == nil {
		t.Fatal("expected the follow-up read error to surface")
	}
}

func TestEnsureSurfacesNonRaceCreateFailure(t *testing.T) {
	// A create that fails for any other reason must surface; only AlreadyExists
	// is recoverable by adopting.
	client := &createFailureClient{err: apierrors.NewForbidden(
		schema.GroupResource{Resource: "jobs"}, "x", errors.New("denied"))}
	if _, err := (Adapter{Client: client}).Ensure(context.Background(), desiredJob()); !apierrors.IsForbidden(err) {
		t.Fatalf("error = %v", err)
	}
}

type createFailureClient struct{ err error }

func (f *createFailureClient) Get(context.Context, string, string, metav1.GetOptions) (*batchv1.Job, error) {
	return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "jobs"}, "x")
}

func (f *createFailureClient) Create(context.Context, *batchv1.Job, metav1.CreateOptions) (*batchv1.Job, error) {
	return nil, f.err
}

func (f *createFailureClient) Delete(context.Context, string, string, metav1.DeleteOptions) error {
	return nil
}

type failingAdoptClient struct{ n int }

func (f *failingAdoptClient) Get(context.Context, string, string, metav1.GetOptions) (*batchv1.Job, error) {
	f.n++
	if f.n == 1 {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "jobs"}, "x")
	}
	return nil, errors.New("api down")
}

func (f *failingAdoptClient) Create(context.Context, *batchv1.Job, metav1.CreateOptions) (*batchv1.Job, error) {
	return nil, apierrors.NewAlreadyExists(schema.GroupResource{Resource: "jobs"}, "x")
}

func (f *failingAdoptClient) Delete(context.Context, string, string, metav1.DeleteOptions) error {
	return nil
}

func TestKubernetesJobClientRoundTrip(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	client := KubernetesJobClient{Client: clientset.BatchV1()}
	ctx := context.Background()
	desired := BuildJob(Identity{RunName: "run-1", RunUID: "uid-1", Attempt: 1}, "finance", Template{Image: "img"})

	created, err := client.Create(ctx, desired, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != desired.Name || created.Namespace != "finance" {
		t.Fatalf("created = %s/%s", created.Namespace, created.Name)
	}

	fetched, err := client.Get(ctx, "finance", desired.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if fetched.Name != desired.Name {
		t.Fatalf("fetched %q", fetched.Name)
	}

	if err := client.Delete(ctx, "finance", desired.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get(ctx, "finance", desired.Name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("job still present: %v", err)
	}
}

func TestKubernetesJobClientRejectsJobWithoutNamespace(t *testing.T) {
	client := KubernetesJobClient{Client: fake.NewSimpleClientset().BatchV1()}
	// Creating without a namespace would write to the process's own namespace,
	// which is never what a run wants.
	if _, err := client.Create(context.Background(), &batchv1.Job{}, metav1.CreateOptions{}); err == nil {
		t.Fatal("expected a namespace error")
	}
}

func TestKubernetesJobClientPropagatesApiErrors(t *testing.T) {
	client := KubernetesJobClient{Client: fake.NewSimpleClientset().BatchV1()}
	if _, err := client.Get(context.Background(), "missing-ns", "nope", metav1.GetOptions{}); err == nil {
		t.Fatal("expected a not-found error from the typed client")
	}
}

func TestKubernetesJobClientThroughAdapter(t *testing.T) {
	// The typed wrapper and the adapter must compose: this is the path the
	// operator actually runs.
	adapter := Adapter{Client: KubernetesJobClient{Client: fake.NewSimpleClientset().BatchV1()}}
	ctx := context.Background()
	desired := BuildJob(Identity{RunName: "run-1", Attempt: 1}, "finance", Template{Image: "img"})

	first, err := adapter.Ensure(ctx, desired)
	if err != nil {
		t.Fatal(err)
	}
	second, err := adapter.Ensure(ctx, desired)
	if err != nil {
		t.Fatal(err)
	}
	if second.UID != first.UID {
		t.Fatalf("second ensure returned a different job: %q vs %q", second.UID, first.UID)
	}
	if !strings.HasPrefix(first.Name, "oj-run-1") {
		t.Fatalf("job name = %q", first.Name)
	}
}
