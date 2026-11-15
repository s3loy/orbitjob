package v1alpha1

import "orbitjob/internal/domain/executionpolicy"

// NormalizeExecutionPolicy pins resolved v1 defaults and leaves legacy omission
// unchanged. Every authoring and projection boundary uses the same validation.
func (s *ScheduledJobSpec) NormalizeExecutionPolicy() error {
	resources, err := executionpolicy.Normalize(s.ExecutionPolicyVersion, s.RetrySafe, s.JobTemplate.BackoffLimit, s.RetryPolicy.MaxAttempts, s.JobTemplate.Resources)
	if err != nil {
		return err
	}
	s.JobTemplate.Resources = resources
	if s.ExecutionPolicyVersion == executionpolicy.Version1 && s.RetryPolicy.MaxAttempts == 0 {
		s.RetryPolicy.MaxAttempts = 1
	}
	return nil
}
