package operator

import (
	"context"
	"fmt"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"orbitjob/internal/core/domain/revision"
)

type sourceLifecycle interface {
	DeactivateSource(context.Context, string, string, string) error
}

func (r Runtime) ReconcileDeleted(ctx context.Context, resource, namespace, name string) error {
	_, ok := r.Revisions.(sourceLifecycle)
	if !ok {
		return nil
	}
	mode := "kubernetes"
	if resource == "workflowjobs" {
		mode = "workflow"
	} else if resource != "scheduledjobs" {
		return nil
	}
	obj := unstructured.Unstructured{}
	obj.SetNamespace(namespace)
	tenant, err := r.Tenants.Resolve(obj)
	if err != nil {
		return err
	}
	candidates, err := r.retentionSources(ctx, tenant, mode)
	if err != nil {
		return err
	}
	for _, rev := range candidates {
		if rev.Identity.Namespace == namespace && rev.Identity.Name == name {
			if _, err := r.SourceExists(ctx, rev); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r Runtime) current(ctx context.Context, gvr schema.GroupVersionResource, obj unstructured.Unstructured) (unstructured.Unstructured, bool, error) {
	if r.Dynamic == nil {
		return obj, true, nil
	}
	live, err := r.Dynamic.Resource(gvr).Namespace(obj.GetNamespace()).Get(ctx, obj.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return obj, false, nil
	}
	if err != nil {
		return obj, false, err
	}
	if gvr != scheduledJobGVR && gvr != workflowJobGVR && obj.GetUID() != "" && live.GetUID() != obj.GetUID() {
		return obj, false, nil
	}
	if live.GetDeletionTimestamp() != nil {
		return *live, false, nil
	}
	return *live, true, nil
}

// SourceExists fences newly created work against current source identity. Old
// immutable revisions remain readable so already-started attempts retain history.
func (r Runtime) SourceExists(ctx context.Context, rev revision.Revision) (bool, error) {
	gvr := scheduledJobGVR
	if rev.Identity.SourceMode == "workflow" {
		gvr = workflowJobGVR
	} else if rev.Identity.SourceMode != "kubernetes" {
		return true, nil
	}
	if r.Dynamic == nil {
		return false, fmt.Errorf("source liveness client required")
	}
	obj, err := r.Dynamic.Resource(gvr).Namespace(rev.Identity.Namespace).Get(ctx, rev.Identity.Name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return false, err
	}
	alive := err == nil && string(obj.GetUID()) == rev.Identity.SourceUID && obj.GetDeletionTimestamp() == nil
	if !alive {
		if lifecycle, ok := r.Revisions.(sourceLifecycle); ok {
			tenant, e := r.Tenants.Resolve(unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"namespace": rev.Identity.Namespace}}})
			if e != nil {
				return false, e
			}
			if e = lifecycle.DeactivateSource(ctx, tenant, rev.Identity.SourceMode, rev.Identity.SourceUID); e != nil {
				return false, e
			}
		}
	}
	return alive, nil
}

// SyncSourceLiveness repairs missed deletions only after a complete successful
// list. A partial/failed list must never deactivate live definitions.
func (r Runtime) SyncSourceLiveness(ctx context.Context) error {
	_, ok := r.Revisions.(sourceLifecycle)
	if !ok {
		return nil
	}
	resolver, ok := r.Tenants.(interface {
		TenantIDs() []string
		Namespaces() []string
	})
	if !ok {
		return fmt.Errorf("tenant inventory required")
	}
	namespaces := resolver.Namespaces()
	if len(namespaces) == 0 {
		namespaces = []string{""}
	}
	for _, source := range []struct {
		gvr  schema.GroupVersionResource
		mode string
	}{{scheduledJobGVR, "kubernetes"}, {workflowJobGVR, "workflow"}} {
		candidates := map[string][]revision.Revision{}
		for _, tenant := range resolver.TenantIDs() {
			known, err := r.retentionSources(ctx, tenant, source.mode)
			if err != nil {
				return err
			}
			candidates[tenant] = known
		}
		live := map[string][]string{}
		for _, ns := range namespaces {
			continuation := ""
			for {
				list, err := r.Dynamic.Resource(source.gvr).Namespace(ns).List(ctx, metav1.ListOptions{Limit: 500, Continue: continuation})
				if err != nil {
					return err
				}
				for _, obj := range list.Items {
					if obj.GetDeletionTimestamp() != nil {
						continue
					}
					tenant, err := r.Tenants.Resolve(obj)
					if err != nil {
						return err
					}
					live[tenant] = append(live[tenant], string(obj.GetUID()))
				}
				continuation = list.GetContinue()
				if continuation == "" {
					break
				}
			}
		}
		for tenant, known := range candidates {
			present := map[string]bool{}
			for _, uid := range live[tenant] {
				present[uid] = true
			}
			for _, rev := range known {
				if !present[rev.Identity.SourceUID] {
					if _, err := r.SourceExists(ctx, rev); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func (r Runtime) retentionSources(ctx context.Context, tenant, mode string) ([]revision.Revision, error) {
	if mode == "workflow" {
		if reader, ok := r.Revisions.(interface {
			RetentionWorkflowRevisions(context.Context, string) ([]revision.Revision, error)
		}); ok {
			return reader.RetentionWorkflowRevisions(ctx, tenant)
		}
	} else {
		if reader, ok := r.Revisions.(interface {
			RetentionRevisions(context.Context, string) ([]revision.Revision, error)
		}); ok {
			return reader.RetentionRevisions(ctx, tenant)
		}
	}
	return nil, fmt.Errorf("source inventory revision reader is required")
}
