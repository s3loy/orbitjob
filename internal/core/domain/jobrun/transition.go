package jobrun

import "time"

func StartAttempt(run JobRun, attempt Attempt, now time.Time) (JobRun, Attempt, error) {
	if attempt.Number <= 0 || attempt.JobRunID != run.ID || run.Phase != Pending && run.Phase != RetryWaiting {
		return run, attempt, ErrInvalidTransition
	}
	if now.IsZero() {
		return run, attempt, ErrInvalidTransition
	}
	run.Phase = CreatingAttempt
	run.Attempt = attempt.Number
	attempt.Phase = CreatingAttempt
	return run, attempt, nil
}

func RequestCancel(run JobRun, now time.Time) (JobRun, error) {
	if now.IsZero() || !CanTransition(run.Phase, CancelRequested) {
		return run, ErrInvalidTransition
	}
	run.Phase = CancelRequested
	return run, nil
}

func ConfirmCanceled(run JobRun, now time.Time) (JobRun, error) {
	if now.IsZero() || run.Phase != CancelRequested {
		return run, ErrInvalidTransition
	}
	run.Phase = Canceled
	return run, nil
}
