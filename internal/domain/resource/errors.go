package resource

// NotFoundError indicates a requested resource does not exist.
type NotFoundError struct {
	Resource string
	ID       any
}

func (e *NotFoundError) Error() string {
	return e.Resource + " not found"
}
