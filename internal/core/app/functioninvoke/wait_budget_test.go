package functioninvoke

import (
	"context"
	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"testing"
	"time"
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
