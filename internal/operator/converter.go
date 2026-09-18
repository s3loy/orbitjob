package operator

import (
	"fmt"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
)

func scheduledJobFromUnstructured(obj unstructured.Unstructured) (v1alpha1.ScheduledJob, error) {
	var out v1alpha1.ScheduledJob
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &out); err != nil {
		return out, fmt.Errorf("decode scheduledjob: %w", err)
	}
	return out, nil
}

func workflowJobFromUnstructured(obj unstructured.Unstructured) (v1alpha1.WorkflowJob, error) {
	var out v1alpha1.WorkflowJob
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &out); err != nil {
		return out, fmt.Errorf("decode workflowjob: %w", err)
	}
	return out, nil
}
