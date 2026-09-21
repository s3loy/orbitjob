package execution

import (
	"errors"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
)

// ErrForeignJob reports a Kubernetes Job that carries control plane labels but
// belongs to a different run. Adopting it would mean driving someone else's
// workload from this run's state, so the observation is refused instead.
var ErrForeignJob = errors.New("kubernetes job belongs to another run")

// ValidateOwnership checks that an observed Job is the one this run created.
// Name alone is not proof: a Job deleted and recreated with the same name has a
// new UID, and a Job from a previous installation can outlive its run row.
func ValidateOwnership(job *batchv1.Job, identity Identity) error {
	if job == nil {
		return fmt.Errorf("job is required")
	}
	switch {
	case job.Labels[LabelRunName] != identity.RunName:
		return fmt.Errorf("%w: run name %q", ErrForeignJob, job.Labels[LabelRunName])
	case identity.RunUID != "" && job.Labels[LabelRunUID] != identity.RunUID:
		return fmt.Errorf("%w: run uid %q", ErrForeignJob, job.Labels[LabelRunUID])
	case job.Labels[LabelAttempt] != itoa(identity.Attempt):
		return fmt.Errorf("%w: attempt %q", ErrForeignJob, job.Labels[LabelAttempt])
	}
	return nil
}

// IdentityFromJob reconstructs the platform identity carried by an observed Job.
// It is how the Job informer maps a Kubernetes event back to a run without
// holding an in-memory index that a restart would lose.
func IdentityFromJob(job *batchv1.Job) (Identity, error) {
	if job == nil {
		return Identity{}, fmt.Errorf("job is required")
	}
	labels := job.Labels
	if labels[LabelRunName] == "" || labels[LabelAttempt] == "" {
		return Identity{}, fmt.Errorf("job %s/%s is not managed by orbitjob", job.Namespace, job.Name)
	}
	attempt, err := atoi(labels[LabelAttempt])
	if err != nil {
		return Identity{}, fmt.Errorf("job %s has invalid attempt label: %w", job.Name, err)
	}
	return Identity{
		RunName:    labels[LabelRunName],
		RunUID:     labels[LabelRunUID],
		RevisionID: labels[LabelRevisionID],
		Attempt:    attempt,
	}, nil
}
