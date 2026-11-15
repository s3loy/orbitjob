package operator

import (
	"context"
	"fmt"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"orbitjob/internal/core/app/controlplane"
	"orbitjob/internal/core/app/execution"
	"strconv"
)

type ArchivedJobSource interface {
	ArchivedJobs(context.Context, string) ([]controlplane.ArchivedJob, error)
	MarkArchivedJobCleaned(context.Context, string, controlplane.ArchivedJob) error
}

func (r Runtime) CleanupArchivedJobs(ctx context.Context, tenants []string) (int, error) {
	if r.ArchivedJobs == nil {
		return 0, nil
	}
	removed := 0
	for _, tenant := range tenants {
		jobs, e := r.ArchivedJobs.ArchivedJobs(ctx, tenant)
		if e != nil {
			return removed, e
		}
		for _, ref := range jobs {
			job, found, e := r.Jobs.Get(ctx, ref.Namespace, ref.JobName)
			if e != nil {
				return removed, e
			}
			if !found {
				if e = r.ArchivedJobs.MarkArchivedJobCleaned(ctx, tenant, ref); e != nil {
					return removed, e
				}
				continue
			}
			identity := execution.Identity{RunName: ref.RunName, RunUID: ref.RunUID, RevisionID: strconv.FormatInt(ref.RevisionID, 10), Attempt: ref.Attempt}
			owner := metav1.GetControllerOf(job)
			if string(job.UID) != ref.JobUID || execution.ValidateOwnership(job, identity) != nil || owner == nil || owner.Kind != "JobRun" || owner.APIVersion != "workloads.orbitjob.io/v1alpha1" || owner.Name != ref.RunName || string(owner.UID) != ref.RunUID {
				return removed, fmt.Errorf("refuse archived resource cleanup: immutable ownership differs")
			}
			if execution.RetainsTerminationEvidence(job) {
				store, ok := r.Runs.(retainedEvidenceStore)
				if !ok {
					return removed, fmt.Errorf("retained Pod evidence store required")
				}
				a, exists, err := store.RecordedExecution(ctx, tenant, ref.Namespace, ref.JobName, ref.JobUID)
				if err != nil {
					return removed, err
				}
				if !exists {
					return removed, fmt.Errorf("retained archived Job has no recorded attempt")
				}
				settled, err := r.settlePodEvidence(ctx, tenant, a, job)
				if err != nil {
					return removed, err
				}
				if !settled {
					continue
				}
			}
			terminal := false
			for _, condition := range job.Status.Conditions {
				if condition.Status == corev1.ConditionTrue && (condition.Type == batchv1.JobComplete || condition.Type == batchv1.JobFailed) {
					terminal = true
				}
			}
			if !terminal {
				continue
			}
			if e = r.Jobs.Delete(ctx, ref.Namespace, ref.JobName, ref.JobUID); e != nil {
				return removed, e
			}
			if e = r.ArchivedJobs.MarkArchivedJobCleaned(ctx, tenant, ref); e != nil {
				return removed, e
			}
			removed++
		}
	}
	return removed, nil
}
