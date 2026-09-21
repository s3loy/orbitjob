package resource

// NotFoundError indicates a requested resource does not exist.
type NotFoundError struct {
	Resource string
	ID       any
}

func (e *NotFoundError) Error() string {
	return e.Resource + " not found"
}

// ConflictError indicates the requested mutation conflicts with current resource state.
type ConflictError struct {
	Resource string
	ID       any
	Field    string
	Message  string
}

func (e *ConflictError) Error() string {
	if e.Message != "" {
		return e.Message
	}

	return e.Resource + " conflict"
}

// ScopeError indicates the caller's authorization scope cannot be applied to the
// resource it asked for, so the request is refused rather than served with a
// wider view than the caller holds.
//
// This is not the same as "not found". A resource that simply is not in the
// caller's scope is reported as not found, because saying anything else leaks
// whether it exists. This error is for the different case where the resource
// kind has no notion of the scope at all: a key limited to a resource group
// asks for a job definition, and a job definition has no group to be limited
// by. Serving it would silently widen the grant, so it is denied instead.
type ScopeError struct {
	Resource string
	Scope    string
}

func (e *ScopeError) Error() string {
	return e.Resource + " is not scoped to " + e.Scope +
		", so a caller limited to that scope cannot reach it"
}

// RequireUnscoped refuses a caller that carries a scope the named resource
// cannot be limited by. An empty scope passes.
//
// Call this from the normalizer of any query whose resource has no group
// column, before the query runs. The alternative — running it anyway — serves
// the caller more than its grant covers, silently.
func RequireUnscoped(scope, resource string) error {
	if scope == "" {
		return nil
	}

	return &ScopeError{Resource: resource, Scope: scope}
}
