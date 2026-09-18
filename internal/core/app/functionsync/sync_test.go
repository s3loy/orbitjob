package functionsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/core/domain/revision"
)

// The sync loop is the front half of the function surface: it turns active
// definition rows into the revisions an invocation pins. The tests pin the
// tenant routing, the one-revision-per-row identity (source_uid function-<id>,
// generation = version), the skip-one-bad-row semantics, and the decode the
// run pipeline performs on the stored bytes.

type fakeFunctions struct {
	defs []function.Definition
	err  error
}

func (f *fakeFunctions) ActiveForTenant(_ context.Context, _ string) ([]function.Definition, error) {
	return f.defs, f.err
}

// captureRevisions records every projection and can fail the nth call, so the
// skip semantics are exercised against a store that refuses one row.
type captureRevisions struct {
	tenants []string
	revs    []revision.Revision
	errs    []error // consumed per call; a nil entry applies
}

func (c *captureRevisions) ApplyRevisionForTenant(_ context.Context, tenantID string, rev revision.Revision) (int64, error) {
	var err error
	if len(c.errs) > 0 {
		err, c.errs = c.errs[0], c.errs[1:]
	}
	if err != nil {
		return 0, err
	}
	c.tenants = append(c.tenants, tenantID)
	c.revs = append(c.revs, rev)
	return int64(len(c.revs)), nil
}

func activeFunction(id int64, version int) function.Definition {
	return function.Definition{
		ID:             id,
		Version:        version,
		Status:         function.StatusActive,
		Name:           fmt.Sprintf("shrink-%d", id),
		Image:          "registry.example.com/shrink@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Command:        []string{"/bin/shrink"},
		Args:           []string{"--fast"},
		TimeoutSeconds: 30,
		RetryLimit:     2,
	}
}

func newSync(functions FunctionSource, revisions RevisionWriter) *Sync {
	return &Sync{
		Functions: functions,
		Revisions: revisions,
		Now:       func() time.Time { return time.Unix(4000, 0).UTC() },
	}
}

func TestSyncProjectsOneRevisionPerActiveFunction(t *testing.T) {
	functions := &fakeFunctions{defs: []function.Definition{activeFunction(7, 1), activeFunction(9, 3)}}
	revisions := &captureRevisions{}
	synced, err := newSync(functions, revisions).RunTenant(context.Background(), Scope{TenantID: "tenant-a", Namespace: "finance"})
	if err != nil {
		t.Fatal(err)
	}
	if synced != 2 || len(revisions.revs) != 2 {
		t.Fatalf("synced=%d revisions=%d, want 2/2", synced, len(revisions.revs))
	}
	for _, tenant := range revisions.tenants {
		if tenant != "tenant-a" {
			t.Fatalf("revision written for tenant %q", tenant)
		}
	}

	first := revisions.revs[0]
	// Pinned as a literal: the mode is a storage contract shared with the
	// ledger's source_uid prefix, not a symbol to rename.
	if first.Identity.SourceMode != "function" {
		t.Errorf("source_mode = %q, want function", first.Identity.SourceMode)
	}
	if first.Identity.SourceUID != "function-7" || first.Identity.Name != "function-7" {
		t.Errorf("identity = %+v", first.Identity)
	}
	if first.Identity.Namespace != "finance" {
		t.Errorf("namespace = %q, want the scope's namespace", first.Identity.Namespace)
	}
	// The version is the generation: saving the definition bumps it, and the
	// next tick materializes a new revision from it.
	if first.Generation != 1 || revisions.revs[1].Generation != 3 {
		t.Errorf("generations = %d/%d, want 1/3", first.Generation, revisions.revs[1].Generation)
	}
	if first.Actor != ActorFunctionSync {
		t.Errorf("actor = %q", first.Actor)
	}
	if first.CreatedAt != time.Unix(4000, 0).UTC() {
		t.Errorf("created_at = %v", first.CreatedAt)
	}
}

func TestSyncSpecRoundTripsThroughTheRunPipelineDecode(t *testing.T) {
	def := activeFunction(7, 1)
	// A group-carrying definition stamps the tier the definition was created
	// under, the same inheritance a check-sourced revision does.
	def.ResourceGroupID = "grp-tier-1"
	revisions := &captureRevisions{}
	if _, err := newSync(&fakeFunctions{defs: []function.Definition{def}}, revisions).
		RunTenant(context.Background(), Scope{TenantID: "tenant-a", Namespace: "finance"}); err != nil {
		t.Fatal(err)
	}
	rev := revisions.revs[0]
	if rev.ResourceGroupID != "grp-tier-1" {
		t.Errorf("resource group = %q", rev.ResourceGroupID)
	}

	// ReconcileJobRun decodes every pinned revision — function invocations
	// included — as a ScheduledJobSpec and builds the Job from it, so the
	// stored bytes must decode into the rendered spec.
	var declared v1alpha1.ScheduledJobSpec
	if err := json.Unmarshal([]byte(rev.NormalizedSpec), &declared); err != nil {
		t.Fatalf("run pipeline decode of stored spec: %v", err)
	}
	if declared.TimeoutSeconds != 30 {
		t.Errorf("timeout = %d", declared.TimeoutSeconds)
	}
	// RetryLimit 2 means three attempts, the checks counting convention.
	if declared.RetryPolicy.MaxAttempts != 3 {
		t.Errorf("max attempts = %d, want 3", declared.RetryPolicy.MaxAttempts)
	}
	if declared.JobTemplate.Image != def.Image || declared.JobTemplate.Command[0] != "/bin/shrink" {
		t.Errorf("job template = %+v", declared.JobTemplate)
	}
	if declared.Schedule != "" {
		t.Errorf("schedule = %q; a function is invoke-only and must never be schedulable", declared.Schedule)
	}
}

func TestSyncSkipsAFailingDefinitionWithoutAbortingTheTick(t *testing.T) {
	revisions := &captureRevisions{errs: []error{errors.New("spec conflict"), nil, nil}}
	synced, err := newSync(
		&fakeFunctions{defs: []function.Definition{activeFunction(7, 1), activeFunction(9, 1), activeFunction(11, 1)}},
		revisions,
	).RunTenant(context.Background(), Scope{TenantID: "tenant-a", Namespace: "finance"})
	if err != nil {
		t.Fatalf("one bad definition must not abort the tick: %v", err)
	}
	if synced != 2 || len(revisions.revs) != 2 {
		t.Fatalf("synced=%d revisions=%d, want the two healthy rows", synced, len(revisions.revs))
	}
	if revisions.revs[0].Identity.SourceUID != "function-9" {
		t.Fatalf("the wrong row was skipped: %q", revisions.revs[0].Identity.SourceUID)
	}
}

func TestSyncAbortsWhenFunctionsCannotBeListed(t *testing.T) {
	synced, err := newSync(&fakeFunctions{err: errors.New("connection refused")}, &captureRevisions{}).
		RunTenant(context.Background(), Scope{TenantID: "tenant-a", Namespace: "finance"})
	if err == nil {
		t.Fatal("expected the list failure to abort the tick")
	}
	if synced != 0 {
		t.Fatalf("synced = %d on a failed list", synced)
	}
}

func TestSyncRequiresDependencies(t *testing.T) {
	if _, err := newSync(nil, &captureRevisions{}).RunTenant(context.Background(), Scope{TenantID: "t", Namespace: "ns"}); err == nil {
		t.Fatal("expected missing function source to be refused")
	}
	if _, err := newSync(&fakeFunctions{}, nil).RunTenant(context.Background(), Scope{TenantID: "t", Namespace: "ns"}); err == nil {
		t.Fatal("expected missing revision writer to be refused")
	}
}
