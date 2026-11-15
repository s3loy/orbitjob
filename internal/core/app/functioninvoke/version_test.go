package functioninvoke

import (
	"context"
	"errors"
	"testing"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/domain/resource"
)

func TestReviewedVersionNeverPublishesAnOlderRevision(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		definition, revision int
	}{
		{"projection pending", 2, 1}, {"stale review", 3, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			def := activeDefinition(7)
			def.Version = tc.definition
			pub := &fakePublisher{created: true}
			uc := assembledUseCase(&fakeFunctions{def: def, found: true},
				&fakeRevisions{rev: Revision{ID: 501, DefinitionVersion: tc.revision}, found: true}, pub)
			in := invokeInput(7)
			in.ExpectedVersion = 2
			_, err := uc.Invoke(context.Background(), in)
			var conflict *resource.ConflictError
			if !errors.As(err, &conflict) || len(pub.published) != 0 {
				t.Fatalf("error = %v, published = %d", err, len(pub.published))
			}
		})
	}
}

func TestInvokeVerifiesPublisherResultInsteadOfTrustingTheRequestedPin(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mutate   func(*v1alpha1.JobRun)
		conflict bool
	}{
		{"complete replay", func(run *v1alpha1.JobRun) { run.Spec.CancelRequested = true }, false},
		{"foreign revision", func(run *v1alpha1.JobRun) { run.Spec.DefinitionRevision++ }, true},
		{"foreign occurrence", func(run *v1alpha1.JobRun) { run.Spec.OccurrenceKey = "different-occurrence" }, true},
		{"foreign source", func(run *v1alpha1.JobRun) { run.Spec.ScheduledJobRef.UID = "different-source" }, true},
		{"foreign namespace", func(run *v1alpha1.JobRun) { run.Namespace = "different-namespace" }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			def := activeDefinition(7)
			def.Version = 2
			rev := Revision{ID: 501, DefinitionVersion: 2, Namespace: "tasks"}
			in := invokeInput(7)
			in.ExpectedVersion, in.IdempotencyKey = 2, "receipt"
			key, err := deriveOccurrenceKey(in, function.SourceUID(7))
			if err != nil {
				t.Fatal(err)
			}
			stored := jobRun(def, rev, key, in.ActorID)
			tc.mutate(&stored)
			pub := &fakePublisher{stored: stored, created: false}
			uc := assembledUseCase(&fakeFunctions{def: def, found: true}, &fakeRevisions{rev: rev, found: true}, pub)
			result, err := uc.Invoke(context.Background(), in)
			if tc.conflict {
				var conflict *resource.ConflictError
				if !errors.As(err, &conflict) {
					t.Fatalf("foreign publisher result accepted: result=%+v error=%v", result, err)
				}
			} else if err != nil || result.Created || result.RevisionID != 501 || result.OccurrenceKey != key {
				t.Fatalf("matching replay = %+v, error=%v", result, err)
			}
		})
	}
}

func TestReviewedVersionPublishesTheReadyRevision(t *testing.T) {
	def := activeDefinition(7)
	def.Version = 2
	pub := &fakePublisher{created: true}
	uc := assembledUseCase(&fakeFunctions{def: def, found: true},
		&fakeRevisions{rev: Revision{ID: 501, DefinitionVersion: 2}, found: true}, pub)
	in := invokeInput(7)
	in.ExpectedVersion = 2
	if _, err := uc.Invoke(context.Background(), in); err != nil || len(pub.published) != 1 {
		t.Fatalf("error = %v, published = %d", err, len(pub.published))
	}
}
