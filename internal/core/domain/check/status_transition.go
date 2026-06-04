package check

// ChangeStatusSpec is the validated write-side input for pause/resume lifecycle mutations.
type ChangeStatusSpec struct {
	ID            int64
	TenantID      string
	Version       int
	CurrentStatus string
	NextStatus    string
	Action        string
}

// Pause validates whether a check can transition from active to paused.
func Pause(status string, version int) (string, error) {
	if version < 1 {
		return "", validationError("version", "must be >= 1")
	}
	if status != StatusActive {
		return "", validationError("status", "only active checks can be paused")
	}

	return StatusPaused, nil
}

// Resume validates whether a check can transition from paused to active.
func Resume(status string, version int) (string, error) {
	if version < 1 {
		return "", validationError("version", "must be >= 1")
	}
	if status != StatusPaused {
		return "", validationError("status", "only paused checks can be resumed")
	}

	return StatusActive, nil
}
