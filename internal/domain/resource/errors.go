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
