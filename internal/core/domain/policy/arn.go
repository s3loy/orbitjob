// Package policy implements the authorization model: named policy documents,
// resource ARNs, and the evaluation that decides whether a request is allowed.
package policy

import (
	"fmt"
	"strings"
)

// ARN identifies one resource, or a set of them when a segment is "*".
// Format: orbitjob:{tenant}:{group}:{type}/{id}
type ARN struct {
	Tenant string
	Group  string
	Type   string
	ID     string
}

// ParseARN reads the four-segment form. It rejects anything it cannot map onto
// all four fields rather than filling in defaults, because a half-parsed ARN
// would silently widen or narrow a grant.
func ParseARN(s string) (ARN, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 4 || parts[0] != "orbitjob" {
		return ARN{}, fmt.Errorf("malformed arn %q", s)
	}
	tail := strings.SplitN(parts[3], "/", 2)
	if len(tail) != 2 || tail[0] == "" || tail[1] == "" {
		return ARN{}, fmt.Errorf("malformed arn %q", s)
	}
	return ARN{Tenant: parts[1], Group: parts[2], Type: tail[0], ID: tail[1]}, nil
}

func (a ARN) String() string {
	return fmt.Sprintf("orbitjob:%s:%s:%s/%s", a.Tenant, a.Group, a.Type, a.ID)
}

// Matches reports whether the pattern covers the target. A "*" in any segment
// matches that whole segment; an empty Group in the pattern means any group.
func (pattern ARN) Matches(target ARN) bool {
	return segmentMatches(pattern.Tenant, target.Tenant) &&
		groupMatches(pattern.Group, target.Group) &&
		segmentMatches(pattern.Type, target.Type) &&
		segmentMatches(pattern.ID, target.ID)
}

func segmentMatches(pattern, value string) bool {
	return pattern == "*" || pattern == value
}

// groupMatches also accepts the empty segment, so a policy written before
// resource groups existed keeps meaning "every group".
func groupMatches(pattern, value string) bool {
	return pattern == "" || pattern == "*" || pattern == value
}
