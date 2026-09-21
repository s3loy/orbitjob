// Package resourcegroup models the optional isolation tier inside a tenant.
//
// A group plays the role a Kubernetes namespace plays: resources may belong to
// one, and a key scoped to a group sees only that group's resources. The tier
// is optional -- a resource with no group belongs to none, and a key with no
// group is not narrowed by one.
package resourcegroup

import (
	"fmt"
	"regexp"
	"strings"

	"orbitjob/internal/domain/validation"
)

// slugPattern keeps slugs usable inside an ARN segment without escaping.
// The ARN grammar splits on ":" and "/", so either character would make a group
// unaddressable; restricting the alphabet is simpler than quoting it.
var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// MaxNameLength matches the name column width in the baseline schema.
const MaxNameLength = 128

// Group is one resource group.
type Group struct {
	ID       string
	TenantID string
	Slug     string
	Name     string
}

// New validates a group before it is stored.
//
// The slug is normalized to lower case rather than rejected, because a caller
// typing "CI" means the group named ci, and silently creating a second group
// distinguishable only by case is the outcome worth avoiding.
func New(id, tenantID, slug, name string) (Group, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	name = strings.TrimSpace(name)

	if !slugPattern.MatchString(slug) {
		return Group{}, &validation.Error{
			Field:   "slug",
			Message: "must start with a letter or digit and contain only lower-case letters, digits and dashes",
		}
	}
	if name == "" {
		return Group{}, &validation.Error{Field: "name", Message: "must not be empty"}
	}
	if len(name) > MaxNameLength {
		return Group{}, &validation.Error{
			Field:   "name",
			Message: fmt.Sprintf("must be at most %d characters", MaxNameLength),
		}
	}
	return Group{ID: id, TenantID: tenantID, Slug: slug, Name: name}, nil
}
