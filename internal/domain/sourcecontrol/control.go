// Package sourcecontrol defines explicit source controls independently of
// legacy definition suspend fields and source identity deletion.
package sourcecontrol

import "time"

const (
	Kubernetes = "kubernetes"
	Workflow   = "workflow"
	Function   = "function"
	Check      = "check"
)

func ValidMode(mode string) bool {
	return mode == Kubernetes || mode == Workflow || mode == Function || mode == Check
}

type State struct {
	Mode            string
	SourceUID       string
	Version         int64
	SchedulePaused  bool
	ExecutionPaused bool
	Tombstoned      bool
	Actor           string
	UpdatedAt       time.Time
}
