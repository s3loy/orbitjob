package functioninvoke

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
)

type failedObservationPublisher struct {
	fakePublisher
	getErr error
}

func (p *failedObservationPublisher) Get(ctx context.Context, namespace, name string) (v1alpha1.JobRun, error) {
	run, _ := p.fakePublisher.Get(ctx, namespace, name)
	return run, p.getErr
}

func TestInvokeWaitRetainsPublishedPhaseWithoutNewObservation(t *testing.T) {
	for _, phase := range []string{"Pending", "Running"} {
		for _, created := range []bool{false, true} {
			for _, observation := range []struct {
				name string
				err  error
			}{
				{name: "failed", err: errors.New("Kubernetes read unavailable")},
				{name: "empty"},
			} {
				t.Run(fmt.Sprintf("%s/created=%t/%s", phase, created, observation.name), func(t *testing.T) {
					pub := &failedObservationPublisher{
						fakePublisher: fakePublisher{
							stored:  v1alpha1.JobRun{Status: v1alpha1.JobRunStatus{Phase: phase}},
							created: created,
						},
						getErr: observation.err,
					}
					uc := New(
						&fakeFunctions{def: activeDefinition(12), found: true},
						&fakeRevisions{rev: Revision{ID: 4, Namespace: "tasks"}, found: true},
						pub,
					).WithPollInterval(time.Second)
					in := invokeInput(12)
					in.IdempotencyKey = "retain-published-phase"
					in.WaitSeconds = 1

					got, err := uc.Invoke(t.Context(), in)
					if err != nil {
						t.Fatalf("invoke: %v", err)
					}
					if got.Phase != phase {
						t.Fatalf("phase = %q, want the published phase %q", got.Phase, phase)
					}
					if got.Created != created || len(pub.published) != 1 || got.Name != pub.published[0].Name || got.Namespace != "tasks" {
						t.Fatalf("accepted invocation reference changed: %+v", got)
					}
					if len(pub.getNames) != 1 || pub.getNames[0] != got.Name || pub.getNamespaces[0] != got.Namespace {
						t.Fatalf("poll addresses = %v/%v, want one read of %s/%s", pub.getNamespaces, pub.getNames, got.Namespace, got.Name)
					}
				})
			}
		}
	}
}
