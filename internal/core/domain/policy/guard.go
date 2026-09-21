package policy

import "errors"

// ErrPrivilegeEscalation is returned when a caller tries to grant permissions
// it does not itself hold, or to widen a boundary it is subject to.
var ErrPrivilegeEscalation = errors.New("grant exceeds the caller's own permissions")

// ErrPlatformPolicyImmutable is returned when a caller tries to change a
// platform preset. Presets belong to the installation, so a tenant may bind one
// and nothing more: editing AdministratorAccess would hand every tenant that
// already holds it a different policy than the one that was reviewed.
var ErrPlatformPolicyImmutable = errors.New("platform policies cannot be modified")

// CheckGrantable reports whether every permission in granted is already held by
// caller.
//
// A caller may delegate what it holds and nothing more. This is the rule
// Kubernetes applies to a namespace admin: it can create bindings inside its
// own namespace but cannot grant a permission it lacks. Without it, any key
// able to create keys can mint itself an administrator.
func CheckGrantable(caller, granted []Document) error {
	if Permits(granted, caller) {
		return nil
	}
	return ErrPrivilegeEscalation
}

// PropagateBoundary decides the boundary a newly created key must carry.
//
// A boundary does not apply to keys its holder creates unless the creation path
// forces it -- a point AWS documents at length, because teams routinely assume
// "the creator is bounded, so what it creates is bounded". It is not. So a
// caller subject to a boundary produces keys bounded by at least that same
// boundary: it may narrow the ceiling for the new key, never widen it.
//
// A caller without a boundary is already unrestricted within its own grants, so
// its keys inherit nothing.
func PropagateBoundary(callerBoundary, requested *Document) (*Document, error) {
	if callerBoundary == nil {
		return requested, nil
	}
	if requested == nil {
		return callerBoundary, nil
	}
	// The requested boundary must not exceed the caller's own ceiling.
	if !Permits([]Document{*requested}, []Document{*callerBoundary}) {
		return nil, ErrPrivilegeEscalation
	}
	return requested, nil
}
