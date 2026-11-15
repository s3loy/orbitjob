package jobrun

import "errors"

// DispatchStage records the durable boundary before an external creation call.
// A possibly sent request can only be observed; absence never permits resend.
type DispatchStage string

const (
	DispatchPrepared       DispatchStage = "Prepared"
	DispatchMayHaveStarted DispatchStage = "DispatchMayHaveStarted"
	DispatchBound          DispatchStage = "Bound"
)

var ErrDispatchMayHaveStarted = errors.New("execution dispatch may have started; observe the original identity")
