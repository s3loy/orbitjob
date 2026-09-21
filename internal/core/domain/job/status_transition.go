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
