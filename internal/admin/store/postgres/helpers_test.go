package postgres

import (
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/lib/pq"

	"orbitjob/internal/core/domain/audit"
	"orbitjob/internal/domain/resource"
)

// testAuditEvent is the record a mutation writes alongside the change. Every
// write path carries one, so the fixtures name it once rather than repeating
// the fields at each call site.
func testAuditEvent() audit.Event {
	return audit.Event{
		TenantID:     "tenant-a",
		ActorID:      "caller-key",
		EventType:    audit.EventCreate,
		ResourceType: audit.ResourcePolicy,
		ResourceID:   "p1",
	}
}

// jsonBytes matches a []byte or string argument against the given JSON, so an
// expected encoding is asserted at the call site rather than inspected later.
type jsonBytes struct{ want string }

func (j jsonBytes) Match(value driver.Value) bool {
	switch v := value.(type) {
	case []byte:
		return string(v) == j.want
	case string:
		return v == j.want
	default:
		return false
	}
}

// uniqueViolation is what PostgreSQL returns for a duplicate key.
func uniqueViolation() error {
	return &pq.Error{Code: "23505", Message: "duplicate key value violates unique constraint"}
}

// foreignKeyViolation is what PostgreSQL returns when a referenced row is
// still in use.
func foreignKeyViolation() error {
	return &pq.Error{Code: "23503", Message: "update or delete violates foreign key constraint"}
}

func assertConflict(t *testing.T, err error, resourceName, field string) {
	t.Helper()
	var ce *resource.ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("expected a conflict error, got %T: %v", err, err)
	}
	if ce.Resource != resourceName {
		t.Fatalf("expected resource %q, got %q", resourceName, ce.Resource)
	}
	if ce.Field != field {
		t.Fatalf("expected field %q, got %q", field, ce.Field)
	}
}

func assertNotFound(t *testing.T, err error, resourceName string) {
	t.Helper()
	var ne *resource.NotFoundError
	if !errors.As(err, &ne) {
		t.Fatalf("expected a not-found error, got %T: %v", err, err)
	}
	if ne.Resource != resourceName {
		t.Fatalf("expected resource %q, got %q", resourceName, ne.Resource)
	}
}
