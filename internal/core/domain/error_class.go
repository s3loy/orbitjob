package domain

// ErrorClass categorizes a database error by its impact on adaptive control.
type ErrorClass int

const (
	ClassNone     ErrorClass = iota // no error (success path)
	SkipWorthy                      // transient conflict — skip job, don't count toward breaker
	BackoffWorthy                   // resource contention — count toward path 1 error rate
	FatalWorthy                     // non-recoverable — breaker opens immediately
)

func (c ErrorClass) String() string {
	switch c {
	case ClassNone:
		return "none"
	case SkipWorthy:
		return "skip"
	case BackoffWorthy:
		return "backoff"
	case FatalWorthy:
		return "fatal"
	default:
		return "unknown"
	}
}
