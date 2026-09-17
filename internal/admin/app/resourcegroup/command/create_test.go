package command

import (
	"context"
	"errors"
	"testing"

	"orbitjob/internal/core/domain/audit"
	"orbitjob/internal/core/domain/resourcegroup"
	"orbitjob/internal/domain/validation"
)

// stubGroupWriter captures the event it was handed: the repository writes the
// audit row inside the same transaction as the group.
type stubGroupWriter struct {
	got      resourcegroup.Group
	gotEvent audit.Event
	err      error
	calls    int
}

func (s *stubGroupWriter) Create(_ context.Context, g resourcegroup.Group, ev audit.Event) error {
	s.calls++
	s.got = g
	s.gotEvent = ev
	return s.err
}

func TestCreateNormalizesAndStores(t *testing.T) {
	writer := &stubGroupWriter{}

	out, err := NewCreator(writer).Create(context.Background(), CreateInput{
		TenantID: "tenant-a",
		Slug:     "  CI  ",
		Name:     "Continuous Integration",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if writer.got.Slug != "ci" {
		t.Fatalf("expected a normalized slug, got %q", writer.got.Slug)
	}
	if writer.got.TenantID != "tenant-a" {
		t.Fatalf("expected the caller's tenant, got %q", writer.got.TenantID)
	}
	if out.ID == "" || out.ID != writer.got.ID {
		t.Fatalf("the returned id does not match the stored one: %+v vs %+v", out, writer.got)
	}
}

func TestCreateRejectsAnInvalidSlugBeforeWriting(t *testing.T) {
	writer := &stubGroupWriter{}

	_, err := NewCreator(writer).Create(context.Background(), CreateInput{
		TenantID: "tenant-a",
		Slug:     "not a slug",
		Name:     "Name",
	})
	var ve *validation.Error
	if !validation.As(err, &ve) {
		t.Fatalf("expected a validation error, got %v", err)
	}
	if writer.calls != 0 {
		t.Fatal("nothing may be written when validation fails")
	}
}

func TestCreateRecordsAnAuditEvent(t *testing.T) {
	writer := &stubGroupWriter{}

	out, err := NewCreator(writer).Create(context.Background(), CreateInput{
		TenantID: "tenant-a",
		Slug:     "ci",
		Name:     "Continuous Integration",
		ActorID:  "key-1",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	e := writer.gotEvent
	if e.ResourceType != audit.ResourceGroup || e.ResourceID != out.ID || e.ActorID != "key-1" {
		t.Fatalf("unexpected audit event: %+v", e)
	}
	if e.Diff["slug"] != "ci" {
		t.Fatalf("audit diff does not record the slug: %+v", e.Diff)
	}
}

// The group and its audit row are one write, so a failure means no group at all
// rather than a group with no record of who created it.
func TestCreateSurfacesAWriteFailure(t *testing.T) {
	writer := &stubGroupWriter{err: errors.New("transaction aborted")}

	if _, err := NewCreator(writer).Create(context.Background(), CreateInput{
		TenantID: "tenant-a",
		Slug:     "ci",
		Name:     "CI",
	}); err == nil {
		t.Fatal("expected the write failure to propagate")
	}
}

func TestCreatePropagatesAStoreConflict(t *testing.T) {
	writer := &stubGroupWriter{err: errors.New("duplicate key")}
	if _, err := NewCreator(writer).Create(context.Background(), CreateInput{
		TenantID: "tenant-a",
		Slug:     "ci",
		Name:     "CI",
	}); err == nil {
		t.Fatal("expected the store error to propagate")
	}
}

// Every create hands a non-empty event to the repository: there is no path that
// stores a group without recording its author.
func TestCreateAlwaysRecordsAnEvent(t *testing.T) {
	writer := &stubGroupWriter{}
	if _, err := NewCreator(writer).Create(context.Background(), CreateInput{
		TenantID: "tenant-a",
		Slug:     "ci",
		Name:     "CI",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if writer.gotEvent.EventType == "" || writer.gotEvent.ResourceType == "" {
		t.Fatalf("no audit event reached the repository: %+v", writer.gotEvent)
	}
}
