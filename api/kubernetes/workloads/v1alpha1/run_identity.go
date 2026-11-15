package v1alpha1

import (
	"context"
	"errors"
	"fmt"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

var ErrRunIdentityConflict = errors.New("custom resource belongs to another occurrence")

type RunIdentity struct {
	SourceUID     string
	OccurrenceKey string
	RevisionID    int64
	Trigger       Trigger
	Workflow      bool
}

func (id RunIdentity) Matches(obj *unstructured.Unstructured) bool {
	if obj == nil {
		return false
	}
	ref := "scheduledJobRef"
	if id.Workflow {
		ref = "workflowRef"
	}
	uid, _, _ := unstructured.NestedString(obj.Object, "spec", ref, "uid")
	key, _, _ := unstructured.NestedString(obj.Object, "spec", "occurrenceKey")
	revision, _, _ := unstructured.NestedInt64(obj.Object, "spec", "definitionRevision")
	trigger, _, _ := unstructured.NestedString(obj.Object, "spec", "trigger")
	return uid == id.SourceUID && key == id.OccurrenceKey && revision == id.RevisionID && trigger == string(id.Trigger)
}

// ResolveRun reads a bounded ordered candidate set. A name collision is never
// adoption: every existing object must match the complete occurrence identity.
func ResolveRun(ctx context.Context, resource dynamic.ResourceInterface, names []string, expected RunIdentity) (*unstructured.Unstructured, bool, error) {
	seen := map[string]bool{}
	for index, name := range names {
		if name == "" || seen[name] || (index > 0 && len(name) > 253) {
			continue
		}
		seen[name] = true
		obj, err := resource.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		if !expected.Matches(obj) {
			if index > 0 {
				continue
			} // A foreign legacy prefix is not this occurrence.
			return nil, false, fmt.Errorf("%w: %s", ErrRunIdentityConflict, name)
		}
		return obj, true, nil
	}
	return nil, false, nil
}
