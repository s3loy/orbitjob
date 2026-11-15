package command

import (
	"context"
	"errors"
	"testing"

	"orbitjob/internal/domain/resource"
)

func TestTriggerRejectsNonScheduledJobEvenIfReaderReturnsIt(t *testing.T) {
	for _, kind := range []string{"function", "workflow", "check", ""} {
		t.Run(kind, func(t *testing.T) {
			item := t3SuspendableDefinition()
			item.SourceMode = kind
			reader := &t3SpyDefinitionReader{item: item}
			pub := &t3SpyRunPublisher{}
			_, err := NewTriggerJobUseCase(reader, pub).Trigger(context.Background(), TriggerInput{JobID: item.ID, TenantID: t3Tenant, ActorID: "caller"})
			var missing *resource.NotFoundError
			if !errors.As(err, &missing) || pub.calls != 0 {
				t.Fatalf("trigger crossed kind boundary: %v calls=%d", err, pub.calls)
			}
		})
	}
}
