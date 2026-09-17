package revision

import "errors"

var (
	ErrInvalidIdentity = errors.New("invalid revision identity")
	ErrInvalidInput    = errors.New("invalid revision input")
)
