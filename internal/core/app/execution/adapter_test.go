package execution

import (
	"context"
	"errors"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type fakeClient struct {
	existing   *batchv1.Job
	getErr     error
	createErr  error
	deleteErr  error
	created    []*batchv1.Job
	deleted    []string
	createCall int
}

func (f *fakeClient) Get(_ context.Context, _, _ string, _ metav1.GetOptions) (*batchv1.Job, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.existing == nil {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "jobs"}, "missing")
	}
	return f.existing, nil
}

func (f *fakeClient) Create(_ context.Context, job *batchv1.Job, _ metav1.CreateOptions) (*batchv1.Job, error) {
	f.createCall++
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.created = append(f.created, job)
	return job, nil
}

func (f *fakeClient) Delete(_ context.Context, _, name string, _ metav1.DeleteOptions) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, name)
	return nil
}

func desiredJob() *batchv1.Job {
	return &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "oj-run-1-1", Namespace: "finance"}}
}

func TestEnsureReturnsExistingJobWithoutCreating(t *testing.T) {
	existing := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "oj-run-1-1", Namespace: "finance", UID: "uid-9"}}
	client := &fakeClient{existing: existing}
	got, err := (Adapter{Client: client}).Ensure(context.Background(), desiredJob())
	if err != nil {
		t.Fatal(err)
	}
	if got.UID != "uid-9" {
		t.Fatalf("uid = %q", got.UID)
	}
	// Adopting the live object is what stops a controller restart from creating
	// a second Job for the same attempt.
	if client.createCall != 0 {
		t.Fatalf("create called %d times", client.createCall)
	}
}

func TestEnsureCreatesWhenAbsent(t *testing.T) {
	client := &fakeClient{}
	got, err := (Adapter{Client: client}).Ensure(context.Background(), desiredJob())
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "oj-run-1-1" || len(client.created) != 1 {
		t.Fatalf("created = %v", client.created)
	}
}

func TestEnsureAdoptsJobFromLostCreateRace(t *testing.T) {
	// Another writer created the Job between our Get and Create. Adopting the
	// winner is correct; failing here would leave the attempt unrecorded.
	winner := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "oj-run-1-1", Namespace: "finance", UID: "uid-winner"}}
	client := &raceClient{winner: winner}
	got, err := (Adapter{Client: client}).Ensure(context.Background(), desiredJob())
	if err != nil {
		t.Fatal(err)
	}
	if got.UID != "uid-winner" {
		t.Fatalf("uid = %q, want the winner's job", got.UID)
	}
}

// raceClient reports NotFound until the first Create, then AlreadyExists.
type raceClient struct {
	winner *batchv1.Job
	getN   int
}

func (r *raceClient) Get(_ context.Context, _, _ string, _ metav1.GetOptions) (*batchv1.Job, error) {
	r.getN++
	if r.getN == 1 {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "jobs"}, "missing")
	}
	return r.winner, nil
}

func (r *raceClient) Create(_ context.Context, _ *batchv1.Job, _ metav1.CreateOptions) (*batchv1.Job, error) {
	return nil, apierrors.NewAlreadyExists(schema.GroupResource{Resource: "jobs"}, "oj-run-1-1")
}

func (r *raceClient) Delete(context.Context, string, string, metav1.DeleteOptions) error { return nil }

func TestEnsureRejectsBadInput(t *testing.T) {
	tests := []struct {
		name    string
		adapter Adapter
		desired *batchv1.Job
	}{
		{"missing client", Adapter{}, desiredJob()},
		{"nil job", Adapter{Client: &fakeClient{}}, nil},
		{"no name", Adapter{Client: &fakeClient{}}, &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Namespace: "finance"}}},
		{"no namespace", Adapter{Client: &fakeClient{}}, &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "x"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.adapter.Ensure(context.Background(), tt.desired); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestEnsurePropagatesApiErrors(t *testing.T) {
	forbidden := apierrors.NewForbidden(schema.GroupResource{Resource: "jobs"}, "x", errors.New("denied"))
	client := &fakeClient{getErr: forbidden}
	// A permission error must not be mistaken for "absent" and retried as a create.
	if _, err := (Adapter{Client: client}).Ensure(context.Background(), desiredJob()); !apierrors.IsForbidden(err) {
		t.Fatalf("error = %v", err)
	}
	if client.createCall != 0 {
		t.Fatal("must not create after a non-NotFound error")
	}
}

func TestGetDistinguishesAbsenceFromFailure(t *testing.T) {
	client := &fakeClient{}
	job, found, err := (Adapter{Client: client}).Get(context.Background(), "finance", "oj-run-1-1")
	if err != nil || found || job != nil {
		t.Fatalf("job=%v found=%v err=%v", job, found, err)
	}

	client.existing = desiredJob()
	job, found, err = (Adapter{Client: client}).Get(context.Background(), "finance", "oj-run-1-1")
	if err != nil || !found || job == nil {
		t.Fatalf("job=%v found=%v err=%v", job, found, err)
	}

	client.getErr = errors.New("api down")
	if _, _, err := (Adapter{Client: client}).Get(context.Background(), "finance", "x"); err == nil {
		t.Fatal("a transport failure must surface as an error, not as absence")
	}
	if _, _, err := (Adapter{}).Get(context.Background(), "finance", "x"); err == nil {
		t.Fatal("missing client must be rejected")
	}
}

func TestDeleteTreatsAbsenceAsSuccess(t *testing.T) {
	client := &fakeClient{}
	if err := (Adapter{Client: client}).Delete(context.Background(), "finance", "oj-run-1-1"); err != nil {
		t.Fatalf("deleting an absent job must converge: %v", err)
	}
	if len(client.deleted) != 1 {
		t.Fatalf("deleted = %v", client.deleted)
	}

	client.deleteErr = apierrors.NewNotFound(schema.GroupResource{Resource: "jobs"}, "x")
	if err := (Adapter{Client: client}).Delete(context.Background(), "finance", "x"); err != nil {
		t.Fatalf("not-found from the API must converge: %v", err)
	}
	client.deleteErr = errors.New("api down")
	if err := (Adapter{Client: client}).Delete(context.Background(), "finance", "x"); err == nil {
		t.Fatal("a transport failure must surface so the cancel keeps retrying")
	}
	if err := (Adapter{}).Delete(context.Background(), "finance", "x"); err == nil {
		t.Fatal("missing client must be rejected")
	}
}
