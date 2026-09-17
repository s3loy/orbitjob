package projection

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/revision"
)

// RevisionWriter persists an immutable definition revision for one tenant.
// Tenant is a required argument rather than a field so a caller cannot construct
// a writer that silently projects every namespace into one tenant's row set.
type RevisionWriter interface {
	ApplyRevisionForTenant(ctx context.Context, tenantID string, rev revision.Revision) (int64, error)
}

type Service struct {
	Revisions RevisionWriter
	// Now supplies the revision creation time. It defaults to time.Now; tests
	// inject a fixed clock to assert revision identity is stable.
	Now func() time.Time
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// ApplyScheduledJob normalizes a ScheduledJob spec and projects it as an
// immutable revision. It returns the revision id the scheduler must pin.
//
// The normalized form is the full spec, not a hand-picked subset: a run executes
// from the stored revision alone, so anything omitted here would be missing at
// execution time. Marshal is deterministic for a fixed struct definition, which
// is what makes the spec hash meaningful.
func (s Service) ApplyScheduledJob(ctx context.Context, obj v1alpha1.ScheduledJob, tenantID, actor string) (int64, error) {
	if tenantID == "" {
		return 0, fmt.Errorf("tenant is required")
	}
	if s.Revisions == nil {
		return 0, fmt.Errorf("revision writer is required")
	}
	// Only the fields this projection is responsible for. Identity and
	// generation are validated by revision.New, so they are not re-checked
	// here where the two could drift apart.
	if obj.Spec.Schedule == "" || obj.Spec.JobTemplate.Image == "" {
		return 0, fmt.Errorf("schedule and job template image are required")
	}

	rev, err := revision.New(
		revision.Identity{
			SourceMode: "kubernetes",
			SourceUID:  string(obj.UID),
			Namespace:  obj.Namespace,
			Name:       obj.Name,
		},
		obj.Generation,
		normalizeSpec(obj.Spec),
		actor,
		// A ScheduledJob revision has no resource group: the CR surface refuses
		// group-scoped keys, so no CR belongs to a tier.
		"",
		// A revision is created now, not when the CR was created: re-projecting
		// an old resource must not backdate the revision it produces.
		s.now(),
	)
	if err != nil {
		return 0, err
	}
	return s.Revisions.ApplyRevisionForTenant(ctx, tenantID, rev)
}

// normalizeSpec renders the spec deterministically. Marshalling a struct of
// strings, integers and slices cannot fail, so this returns no error rather than
// forcing every caller to handle an impossible branch.
func normalizeSpec(spec v1alpha1.ScheduledJobSpec) string {
	raw, _ := json.Marshal(spec)
	return string(raw)
}
