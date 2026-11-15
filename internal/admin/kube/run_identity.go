package kube

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/domain/resource"
)

func resolveRun(ctx context.Context, client dynamic.ResourceInterface, names []string, identity v1alpha1.RunIdentity) (*unstructured.Unstructured, bool, error) {
	object, found, err := v1alpha1.ResolveRun(ctx, client, names, identity)
	if errors.Is(err, v1alpha1.ErrRunIdentityConflict) {
		return nil, false, &resource.ConflictError{Resource: "run resource", Field: "occurrence_key", Message: "custom resource has a conflicting immutable identity"}
	}
	return object, found, err
}

func publishRun(ctx context.Context, client dynamic.ResourceInterface, object *unstructured.Unstructured, names []string, identity v1alpha1.RunIdentity) (*unstructured.Unstructured, bool, error) {
	existing, found, err := resolveRun(ctx, client, names, identity)
	if err != nil {
		return nil, false, fmt.Errorf("resolve existing run: %w", err)
	}
	if found {
		return existing, false, nil
	}
	created, err := client.Create(ctx, object, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, found, err = resolveRun(ctx, client, names, identity)
		if err != nil {
			return nil, false, err
		}
		if !found {
			return nil, false, &resource.ConflictError{Resource: "run resource", Message: "conflicting run could not be resolved after create"}
		}
		return existing, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return created, true, nil
}

func cancelRun(ctx context.Context, client dynamic.ResourceInterface, definitionName string, identity v1alpha1.RunIdentity) (string, error) {
	name, err := v1alpha1.CancelRun(ctx, client, v1alpha1.RunNameCandidates(definitionName, identity.OccurrenceKey), identity)
	if errors.Is(err, v1alpha1.ErrRunIdentityConflict) {
		return "", &resource.ConflictError{Resource: "run resource", Message: "custom resource changed or is missing; retry after reconciliation"}
	}
	return name, err
}
