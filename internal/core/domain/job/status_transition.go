package job

const (
	StatusActive = "active"
	StatusPaused = "paused"

	ActionPause  = "pause"
	ActionResume = "resume"
)

// ChangeStatusSpec is the validated write-side input for pause/resume lifecycle mutations.
type ChangeStatusSpec struct {
	ID            int64
	TenantID      string
	Version       int
	CurrentStatus string
	NextStatus    string
	Action        string
}

// Pause validates whether a job can transition from active to paused.
func Pause(status string, version int) (string, error) {
	if version < 1 {
		return "", validationError("version", "must be >= 1")
	}
	if status != StatusActive {
		return "", validationError("status", "only active jobs can be paused")
	}

	return StatusPaused, nil
}

// Resume validates whether a job can transition from paused to active.
func Resume(status string, version int) (string, error) {
	if version < 1 {
		return "", validationError("version", "must be >= 1")
	}
	if status != StatusPaused {
		return "", validationError("status", "only paused jobs can be resumed")
	}

	return StatusActive, nil
}
