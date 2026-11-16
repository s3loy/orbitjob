package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"orbitjob/internal/core/app/execution"
	"orbitjob/internal/core/domain/function"
	corepostgres "orbitjob/internal/core/store/postgres"
)

var jobRuns = schema.GroupVersionResource{Group: "workloads.orbitjob.io", Version: "v1alpha1", Resource: "jobruns"}

type ownedRun struct {
	Name     string    `json:"name"`
	UID      types.UID `json:"uid"`
	Revision int64     `json:"revision_id"`
}

func ownedRevisions(ctx context.Context, db *sql.DB, m *manifest) (map[string]map[int64]bool, error) {
	owned := map[string]map[int64]bool{}
	for _, row := range m.Functions {
		if row.ID == 0 {
			return nil, errors.New("seed is incomplete; rerun seed to recover every committed owned ID before cleanup")
		}
		uid := function.SourceUID(row.ID)
		owned[uid] = map[int64]bool{}
		err := withTenant(ctx, db, row.Tenant, func(tx *sql.Tx) error {
			rows, err := tx.QueryContext(ctx, `SELECT id FROM job_definition_revisions
				WHERE tenant_id=$1 AND source_mode='function' AND source_uid=$2`, row.Tenant, uid)
			if err != nil {
				return err
			}
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var id int64
				if err := rows.Scan(&id); err != nil {
					return err
				}
				owned[uid][id] = true
			}
			return rows.Err()
		})
		if err != nil {
			return nil, err
		}
	}
	return owned, nil
}

func matchesRun(obj unstructured.Unstructured, owned map[string]map[int64]bool) (bool, error) {
	uid, _, _ := unstructured.NestedString(obj.Object, "spec", "scheduledJobRef", "uid")
	revisions, ours := owned[uid]
	if !ours {
		return false, nil
	}
	id, _, err := unstructured.NestedInt64(obj.Object, "spec", "definitionRevision")
	if err != nil || !revisions[id] {
		return false, errors.New("JobRun names an owned Function but an unverified revision; refusing cleanup")
	}
	if obj.GetNamespace() != fixtureNamespace || obj.GetUID() == "" {
		return false, errors.New("owned JobRun has an invalid namespace or UID")
	}
	return true, nil
}

func rememberRun(m *manifest, obj unstructured.Unstructured) error {
	id, _, _ := unstructured.NestedInt64(obj.Object, "spec", "definitionRevision")
	for _, run := range m.Runs {
		if run.UID == obj.GetUID() {
			if run.Name != obj.GetName() || run.Revision != id {
				return errors.New("recorded JobRun ownership changed")
			}
			return nil
		}
	}
	m.Runs = append(m.Runs, ownedRun{Name: obj.GetName(), UID: obj.GetUID(), Revision: id})
	return nil
}

func cleanup(ctx context.Context, env *environment, o options, m *manifest) error {
	owned, err := ownedRevisions(ctx, env.db, m)
	if err != nil {
		return err
	}
	if err := verifyRememberedRuns(m, owned); err != nil {
		return err
	}
	repo := corepostgres.NewFunctionRepository(env.db)
	// Retire configuration first. History and revisions remain for audit.
	for _, row := range m.Functions {
		_, live, err := repo.GetForTenant(ctx, row.Tenant, row.ID)
		if err != nil {
			return err
		}
		if live {
			if err := repo.DeleteForTenant(ctx, row.Tenant, row.ID); err != nil {
				return err
			}
		}
	}
	waitCtx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()
	for {
		list, err := env.dynamic.Resource(jobRuns).Namespace(o.namespace).List(waitCtx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		terminal := true
		for _, obj := range list.Items {
			ours, err := matchesRun(obj, owned)
			if err != nil {
				return err
			}
			if !ours {
				continue
			}
			if err := rememberRun(m, obj); err != nil {
				return err
			}
			if err := writeManifest(o.manifest, m); err != nil {
				return err
			}
			phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
			if phase == "Succeeded" || phase == "Failed" || phase == "Canceled" {
				continue
			}
			terminal = false
			requested, _, _ := unstructured.NestedBool(obj.Object, "spec", "cancelRequested")
			if requested {
				continue
			}
			patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"uid": obj.GetUID(), "resourceVersion": obj.GetResourceVersion()}, "spec": map[string]any{"cancelRequested": true}})
			_, err = env.dynamic.Resource(jobRuns).Namespace(o.namespace).Patch(waitCtx, obj.GetName(), types.MergePatchType, patch, metav1.PatchOptions{})
			if err != nil && !apierrors.IsNotFound(err) && !apierrors.IsConflict(err) {
				return err
			}
		}
		if terminal {
			break
		}
		select {
		case <-waitCtx.Done():
			return errors.New("owned cancellation is not terminal; resources and evidence are retained, inspect CancelUnknown before retrying cleanup")
		case <-time.After(time.Second):
		}
	}
	jobs, err := env.kube.BatchV1().Jobs(o.namespace).List(waitCtx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, job := range jobs.Items {
		ours, err := matchesExecution(job.Labels, m)
		if err != nil {
			return err
		}
		if !ours {
			if hasOwnedRevision(job.Labels, owned) {
				return errors.New("an owned revision has an execution with no recorded JobRun UID; inspect it before cleanup")
			}
			continue
		}
		uid := job.UID
		policy := metav1.DeletePropagationForeground
		if err := env.kube.BatchV1().Jobs(o.namespace).Delete(waitCtx, job.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}, PropagationPolicy: &policy}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	for _, run := range m.Runs {
		uid := run.UID
		if err := env.dynamic.Resource(jobRuns).Namespace(o.namespace).Delete(waitCtx, run.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	for {
		jobs, err := env.kube.BatchV1().Jobs(o.namespace).List(waitCtx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		pods, err := env.kube.CoreV1().Pods(o.namespace).List(waitCtx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		remaining := false
		for _, job := range jobs.Items {
			ours, err := matchesExecution(job.Labels, m)
			if err != nil {
				return err
			}
			if !ours && hasOwnedRevision(job.Labels, owned) {
				return errors.New("an owned revision has an unverified Job; cleanup is incomplete")
			}
			remaining = remaining || ours
		}
		for _, pod := range pods.Items {
			ours, err := matchesExecution(pod.Labels, m)
			if err != nil {
				return err
			}
			if !ours && hasOwnedRevision(pod.Labels, owned) {
				return errors.New("an owned revision has an unverified Pod; cleanup is incomplete")
			}
			remaining = remaining || ours
		}
		for _, run := range m.Runs {
			obj, err := env.dynamic.Resource(jobRuns).Namespace(o.namespace).Get(waitCtx, run.Name, metav1.GetOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				return err
			}
			remaining = remaining || (err == nil && obj.GetUID() == run.UID)
		}
		if !remaining {
			break
		}
		select {
		case <-waitCtx.Done():
			return errors.New("owned Jobs, Pods or JobRuns remain; cleanup is incomplete")
		case <-time.After(time.Second):
		}
	}
	m.Cleaned = true
	if err := writeManifest(o.manifest, m); err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, "Owned definitions retired and observed execution resources removed. Database history, revisions and audit evidence were retained.")
	return err
}

func hasOwnedRevision(labels map[string]string, owned map[string]map[int64]bool) bool {
	id, _ := strconv.ParseInt(labels[execution.LabelRevisionID], 10, 64)
	for _, revisions := range owned {
		if revisions[id] {
			return true
		}
	}
	return false
}

func verifyRememberedRuns(m *manifest, owned map[string]map[int64]bool) error {
	seen := map[types.UID]bool{}
	for _, run := range m.Runs {
		valid := false
		for source, revisions := range owned {
			if revisions[run.Revision] && strings.HasPrefix(run.Name, source+"-") {
				valid = true
			}
		}
		if !valid || run.UID == "" || seen[run.UID] {
			return errors.New("recorded JobRun is outside the manifest's owned revisions")
		}
		seen[run.UID] = true
	}
	return nil
}

func matchesExecution(labels map[string]string, m *manifest) (bool, error) {
	revision, _ := strconv.ParseInt(labels[execution.LabelRevisionID], 10, 64)
	for _, run := range m.Runs {
		if string(run.UID) == labels[execution.LabelRunUID] {
			if run.Name != labels[execution.LabelRunName] || revision != run.Revision {
				return false, errors.New("execution labels disagree with recorded JobRun ownership")
			}
			return true, nil
		}
	}
	return false, nil
}
