package operator

import (
	"context"
	"errors"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"orbitjob/internal/core/domain/jobrun"
)

func (r Runtime) admissionConditions(obj unstructured.Unstructured, blocked bool, message string) []any {
	current, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	out := make([]any, 0, len(current)+1)
	status, reason := "False", "AdmissionResolved"
	if blocked {
		status, reason = "True", "ConcurrencyBlocked"
	}
	next := map[string]any{"type": "ConcurrencyBlocked", "status": status, "reason": reason, "message": message,
		"lastTransitionTime": r.now().Format("2006-01-02T15:04:05Z07:00")}
	for _, item := range current {
		condition, ok := item.(map[string]any)
		if !ok || condition["type"] != "ConcurrencyBlocked" {
			out = append(out, item)
			continue
		}
		if condition["status"] == status && condition["reason"] == reason && condition["message"] == message {
			next = condition
		}
	}
	return append(out, next)
}

func (r Runtime) patchAdmissionBlocked(ctx context.Context, gvr schema.GroupVersionResource, obj unstructured.Unstructured, err error) error {
	var blocked *jobrun.AdmissionBlockedError
	if !errors.As(err, &blocked) {
		return err
	}
	return r.patchStatus(ctx, gvr, obj, map[string]any{"phase": "Pending", "conditions": r.admissionConditions(obj, true, blocked.Reason)})
}
