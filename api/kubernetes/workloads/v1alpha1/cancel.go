package v1alpha1

import (
	"context"
	"encoding/json"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

// CancelRun only mutates the exact server object whose complete occurrence
// identity was checked. A same-UID status update may be retried after a fresh
// identity check; a replacement UID is never adopted within this request.
func CancelRun(ctx context.Context, resource dynamic.ResourceInterface, names []string, expected RunIdentity) (string, error) {
	const maxAttempts = 3
	var originalUID types.UID
	for range maxAttempts {
		object, found, err := ResolveRun(ctx, resource, names, expected)
		if err != nil {
			return "", err
		}
		if !found {
			return "", fmt.Errorf("%w: run resource missing while cancellation is pending", ErrRunIdentityConflict)
		}
		if object.GetUID() == "" || object.GetResourceVersion() == "" {
			return "", fmt.Errorf("%w: run resource lacks server identity", ErrRunIdentityConflict)
		}
		if originalUID != "" && originalUID != object.GetUID() {
			return "", fmt.Errorf("%w: run resource was replaced during cancellation", ErrRunIdentityConflict)
		}
		originalUID = object.GetUID()
		patch, err := json.Marshal([]map[string]any{
			{"op": "test", "path": "/metadata/uid", "value": string(originalUID)},
			{"op": "test", "path": "/metadata/resourceVersion", "value": object.GetResourceVersion()},
			{"op": "add", "path": "/spec/cancelRequested", "value": true},
		})
		if err != nil {
			return "", err
		}
		_, err = resource.Patch(ctx, object.GetName(), types.JSONPatchType, patch, metav1.PatchOptions{})
		if err == nil {
			return object.GetName(), nil
		}
		if !apierrors.IsNotFound(err) && !apierrors.IsConflict(err) && !apierrors.IsInvalid(err) {
			return "", err
		}
		// Retry only after ResolveRun revalidates every immutable field and the
		// original UID. JSON Patch test failures are reported as Invalid by the API.
	}
	return "", fmt.Errorf("%w: run resource kept changing during cancellation", ErrRunIdentityConflict)
}
