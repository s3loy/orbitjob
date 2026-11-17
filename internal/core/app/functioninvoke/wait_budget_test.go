package functioninvoke

import (
	"context"
	"testing"
	"time"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
)

type blockingPublisher struct {
	fakePublisher
	getDeadline time.Time
}

func (p *blockingPublisher) Get(ctx context.Context, _, _ string) (v1alpha1.JobRun, error) {
	p.getDeadline, _ = ctx.Deadline()
	<-ctx.Done()
	return v1alpha1.JobRun{}, ctx.Err()
}
func TestSynchronousWaitBoundsSlowObservation(t *testing.T) {
	pub := &blockingPublisher{}
	uc := &UseCase{publisher: pub}
	deadline := time.Now().Add(30 * time.Millisecond)
	done := make(chan struct{})
	go func() { uc.awaitTerminalPhase(context.Background(), "tasks", "run", deadline); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("observation ignored synchronous wait budget")
	}
	if pub.getDeadline.IsZero() || pub.getDeadline.After(deadline) {
		t.Fatal("Get did not inherit wait deadline")
	}
}

func TestInvokeWaitDeadlineRetainsPublishedPhase(t *testing.T) {
	for _, phase := range []string{"Pending", "Running"} {
		t.Run(phase, func(t *testing.T) {
			pub := &blockingPublisher{fakePublisher: fakePublisher{
				stored: v1alpha1.JobRun{Status: v1alpha1.JobRunStatus{Phase: phase}},
			}}
			uc := New(
				&fakeFunctions{def: activeDefinition(12), found: true},
				&fakeRevisions{rev: Revision{ID: 4, Namespace: "tasks"}, found: true},
				pub,
			)
			in := invokeInput(12)
			in.WaitSeconds = MaxInvokeWaitSeconds
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			deadline, _ := ctx.Deadline()
			type response struct {
				result Result
				err    error
			}
			done := make(chan response, 1)
			go func() {
				got, err := uc.Invoke(ctx, in)
				done <- response{result: got, err: err}
			}()

			select {
			case got := <-done:
				if got.err != nil {
					t.Fatalf("invoke: %v", got.err)
				}
				if got.result.Phase != phase || got.result.Name == "" || got.result.Namespace != "tasks" {
					t.Fatalf("expired wait lost the accepted invocation: %+v", got.result)
				}
			case <-time.After(time.Second):
				t.Fatal("invoke ignored the caller's earlier deadline")
			}
			if pub.getDeadline.IsZero() || !pub.getDeadline.Equal(deadline) {
				t.Fatalf("Get deadline = %v, want caller deadline %v", pub.getDeadline, deadline)
			}
		})
	}
}
