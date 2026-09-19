package command

import (
	"context"
	"errors"
	"strings"
	"testing"

	"orbitjob/internal/core/domain/audit"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/domain/validation"
)

// stubPolicyWriter records the event it was handed, because the repository
// writes the audit row inside the same transaction as the policy.
type stubPolicyWriter struct {
	got       policy.Record
	gotRaw    []byte
	gotEvent  audit.Event
	err       error
	callCount int
}

func (s *stubPolicyWriter) Create(_ context.Context, rec policy.Record, raw []byte, ev audit.Event) error {
	s.callCount++
	s.got = rec
	s.gotRaw = raw
	s.gotEvent = ev
	return s.err
}

func validDocument(t *testing.T) []byte {
	t.Helper()
	return []byte(`{"version":"1","statement":[{"effect":"Allow","action":["job:Trigger"],"resource":["orbitjob:self:*:job/*"]}]}`)
}

func TestCreateStoresTheValidatedBytes(t *testing.T) {
	writer := &stubPolicyWriter{}
	raw := validDocument(t)

	got, err := NewCreator(writer).Create(context.Background(), CreateInput{
		TenantID: "tenant-a",
		Name:     "JobOperator",
		Document: raw,
		ActorID:  "key-1",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got.ID == "" {
		t.Fatal("expected an id")
	}
	// The stored bytes must be the submitted bytes. Decoding and re-encoding
	// would store a document nobody validated.
	if string(writer.gotRaw) != string(raw) {
		t.Fatalf("stored bytes differ from the validated ones:\n got %s\nwant %s", writer.gotRaw, raw)
	}
	if writer.got.TenantID != "tenant-a" || writer.got.Name != "JobOperator" {
		t.Fatalf("unexpected record: %+v", writer.got)
	}
}

func TestCreateRejectsAnUnknownAction(t *testing.T) {
	writer := &stubPolicyWriter{}
	raw := []byte(`{"version":"1","statement":[{"effect":"Allow","action":["job:Creat"],"resource":["orbitjob:self:*:job/*"]}]}`)

	_, err := NewCreator(writer).Create(context.Background(), CreateInput{
		TenantID: "tenant-a",
		Name:     "Typo",
		Document: raw,
	})
	if err == nil {
		t.Fatal("a policy naming an action nobody implements was accepted")
	}
	var ve *validation.Error
	if !validation.As(err, &ve) || ve.Field != "document" {
		t.Fatalf("expected a document validation error, got %v", err)
	}
	if writer.callCount != 0 {
		t.Fatal("nothing may be written when validation fails")
	}
}

func TestCreateRejectsTheWildcardAction(t *testing.T) {
	writer := &stubPolicyWriter{}
	raw := []byte(`{"version":"1","statement":[{"effect":"Allow","action":["*"],"resource":["orbitjob:self:*:*/*"]}]}`)

	if _, err := NewCreator(writer).Create(context.Background(), CreateInput{
		TenantID: "tenant-a",
		Name:     "Everything",
		Document: raw,
	}); err == nil {
		t.Fatal("a tenant-authored policy was allowed to grant every action")
	}
	if writer.callCount != 0 {
		t.Fatal("nothing may be written when validation fails")
	}
}

func TestCreateRejectsAMalformedDocument(t *testing.T) {
	writer := &stubPolicyWriter{}
	raw := []byte(`{"version":"1","statement":[]}`)

	if _, err := NewCreator(writer).Create(context.Background(), CreateInput{
		TenantID: "tenant-a",
		Name:     "Empty",
		Document: raw,
	}); err == nil {
		t.Fatal("a policy with no statements was accepted")
	}
}

// The audit trail is what makes a grant reviewable afterwards, so it has to
// name the acting key and carry the document that was granted.
func TestCreateRecordsAnAuditEvent(t *testing.T) {
	writer := &stubPolicyWriter{}

	out, err := NewCreator(writer).Create(context.Background(), CreateInput{
		TenantID: "tenant-a",
		Name:     "JobOperator",
		Document: validDocument(t),
		ActorID:  "key-1",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	e := writer.gotEvent
	if e.ActorID != "key-1" || e.TenantID != "tenant-a" {
		t.Fatalf("audit event names the wrong actor: %+v", e)
	}
	if e.EventType != audit.EventCreate || e.ResourceType != audit.ResourcePolicy || e.ResourceID != out.ID {
		t.Fatalf("unexpected audit event: %+v", e)
	}
	if e.Diff["name"] != "JobOperator" {
		t.Fatalf("audit diff does not record the policy name: %+v", e.Diff)
	}
}

// The policy and its audit row are one write, so a failed write means no
// policy at all rather than a policy with no record of who created it.
func TestCreateSurfacesAWriteFailure(t *testing.T) {
	writer := &stubPolicyWriter{err: errors.New("transaction aborted")}

	_, err := NewCreator(writer).Create(context.Background(), CreateInput{
		TenantID: "tenant-a",
		Name:     "JobOperator",
		Document: validDocument(t),
	})
	if err == nil || !strings.Contains(err.Error(), "transaction aborted") {
		t.Fatalf("expected the write failure to propagate, got %v", err)
	}
}

func TestCreatePropagatesAStoreConflict(t *testing.T) {
	writer := &stubPolicyWriter{err: errors.New("duplicate key")}

	_, err := NewCreator(writer).Create(context.Background(), CreateInput{
		TenantID: "tenant-a",
		Name:     "JobOperator",
		Document: validDocument(t),
	})
	if err == nil {
		t.Fatal("expected the store error to propagate")
	}
}

// Every create hands a non-empty event to the repository: there is no path
// that stores a policy without recording its author.
func TestCreateAlwaysRecordsAnEvent(t *testing.T) {
	writer := &stubPolicyWriter{}
	if _, err := NewCreator(writer).Create(context.Background(), CreateInput{
		TenantID: "tenant-a",
		Name:     "JobOperator",
		Document: validDocument(t),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if writer.gotEvent.EventType == "" || writer.gotEvent.ResourceType == "" {
		t.Fatalf("no audit event reached the repository: %+v", writer.gotEvent)
	}
}
