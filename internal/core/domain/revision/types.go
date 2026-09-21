package revision

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

type Identity struct {
	SourceMode string
	SourceUID  string
	Namespace  string
	Name       string
}

type Revision struct {
	ID             int64
	Identity       Identity
	Generation     int64
	SpecHash       string
	NormalizedSpec string
	CreatedAt      time.Time
	Actor          string
	// ResourceGroupID is the isolation tier the revision's source belongs to,
	// or empty when it belongs to none. A check-sourced revision inherits its
	// check row's group; a ScheduledJob revision has none, because the CR
	// surface refuses group-scoped keys outright.
	ResourceGroupID string
}

// New projects one revision. resourceGroupID is the source's isolation tier;
// pass the check row's group for check-sourced revisions and "" for
// ScheduledJob ones.
func New(identity Identity, generation int64, normalizedSpec, actor, resourceGroupID string, now time.Time) (Revision, error) {
	if identity.SourceMode == "" || identity.SourceUID == "" || identity.Namespace == "" || identity.Name == "" {
		return Revision{}, ErrInvalidIdentity
	}
	if generation <= 0 || strings.TrimSpace(normalizedSpec) == "" {
		return Revision{}, ErrInvalidInput
	}
	if now.IsZero() {
		return Revision{}, ErrInvalidInput
	}
	sum := sha256.Sum256([]byte(normalizedSpec))
	return Revision{Identity: identity, Generation: generation, SpecHash: hex.EncodeToString(sum[:]), NormalizedSpec: normalizedSpec, CreatedAt: now, Actor: actor, ResourceGroupID: strings.TrimSpace(resourceGroupID)}, nil
}

func (r Revision) IsImmutableComparedTo(other Revision) bool {
	return r.Identity == other.Identity && r.Generation == other.Generation && r.SpecHash == other.SpecHash && r.NormalizedSpec == other.NormalizedSpec
}
