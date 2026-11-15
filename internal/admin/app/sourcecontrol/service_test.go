package sourcecontrol

import (
	"context"
	"errors"
	"testing"
	"time"

	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/sourcecontrol"
	"orbitjob/internal/domain/validation"
	"orbitjob/internal/platform/scan"
)

type repositoryStub struct {
	resolve func(context.Context, string, string, int64) (Target, error)
	read    func(context.Context, Target) (sourcecontrol.State, error)
	update  func(context.Context, Target, Change, string) (sourcecontrol.State, error)
}

func (r repositoryStub) Resolve(ctx context.Context, tenant, kind string, id int64) (Target, error) {
	return r.resolve(ctx, tenant, kind, id)
}

func (r repositoryStub) Read(ctx context.Context, target Target) (sourcecontrol.State, error) {
	return r.read(ctx, target)
}

func (r repositoryStub) Update(ctx context.Context, target Target, change Change, actor string) (sourcecontrol.State, error) {
	return r.update(ctx, target, change, actor)
}

func TestResolveRejectsInvalidIdentityBeforeRepositoryAccess(t *testing.T) {
	tenant := scan.GenerateID()
	for _, tt := range []struct {
		name, tenant, kind, field string
		id                        int64
	}{
		{name: "missing tenant", kind: "jobs", id: 1, field: "tenant_id"},
		{name: "zero id", tenant: tenant, kind: "jobs", field: "id"},
		{name: "negative id", tenant: tenant, kind: "checks", id: -1, field: "id"},
		{name: "unknown kind", tenant: tenant, kind: "runs", id: 1, field: "kind"},
		{name: "missing kind", tenant: tenant, id: 1, field: "kind"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(repositoryStub{resolve: func(context.Context, string, string, int64) (Target, error) {
				t.Fatal("invalid identity reached repository")
				return Target{}, nil
			}})
			got, err := svc.Resolve(context.Background(), tt.tenant, tt.kind, tt.id)
			var invalid *validation.Error
			if !errors.As(err, &invalid) || invalid.Field != tt.field {
				t.Fatalf("Resolve error = %v, want validation for %s", err, tt.field)
			}
			if got != (Target{}) {
				t.Fatalf("invalid identity returned target %+v", got)
			}
		})
	}
}

func TestResolvePreservesAuthorizedTargetAndRepositoryErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tenant, group := scan.GenerateID(), scan.GenerateID()
	for _, tt := range []struct{ kind, mode string }{
		{kind: "jobs", mode: sourcecontrol.Kubernetes},
		{kind: "workflows", mode: sourcecontrol.Workflow},
		{kind: "functions", mode: sourcecontrol.Function},
		{kind: "checks", mode: sourcecontrol.Check},
	} {
		t.Run(tt.kind, func(t *testing.T) {
			want := Target{TenantID: tenant, Kind: tt.kind, ID: 17, Mode: tt.mode, SourceUID: scan.GenerateID(), ResourceGroupID: group}
			calls := 0
			svc := NewService(repositoryStub{resolve: func(gotCtx context.Context, gotTenant, gotKind string, gotID int64) (Target, error) {
				calls++
				if gotCtx != ctx || gotTenant != tenant || gotKind != tt.kind || gotID != want.ID {
					t.Fatalf("Resolve arguments = (%v, %q, %q, %d)", gotCtx, gotTenant, gotKind, gotID)
				}
				return want, nil
			}})
			got, err := svc.Resolve(ctx, tenant, tt.kind, want.ID)
			if err != nil || got != want || calls != 1 {
				t.Fatalf("Resolve = (%+v, %v), calls=%d, want (%+v, nil) once", got, err, calls, want)
			}
		})
	}
	wantErr := &resource.NotFoundError{Resource: "checks", ID: int64(17)}
	svc := NewService(repositoryStub{resolve: func(context.Context, string, string, int64) (Target, error) {
		return Target{}, wantErr
	}})
	if _, err := svc.Resolve(ctx, tenant, "checks", 17); !errors.Is(err, wantErr) {
		t.Fatalf("Resolve error = %v, want original not-found error", err)
	}
}

func TestGetReturnsRepositoryStateWithoutLosingControlMetadata(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	target := Target{TenantID: scan.GenerateID(), Kind: "functions", ID: 17, Mode: sourcecontrol.Function, SourceUID: "function-17", ResourceGroupID: scan.GenerateID()}
	want := sourcecontrol.State{Mode: target.Mode, SourceUID: target.SourceUID, Version: 8, SchedulePaused: true, ExecutionPaused: true, Tombstoned: true, Actor: "user:" + scan.GenerateID(), UpdatedAt: time.Date(2026, 11, 15, 2, 0, 0, 0, time.UTC)}
	wantErr := errors.New("control read unavailable")
	for _, repositoryErr := range []error{nil, wantErr} {
		calls := 0
		svc := NewService(repositoryStub{read: func(gotCtx context.Context, gotTarget Target) (sourcecontrol.State, error) {
			calls++
			if gotCtx != ctx || gotTarget != target {
				t.Fatalf("Read lost trusted context or parent: target=%+v", gotTarget)
			}
			return want, repositoryErr
		}})
		got, err := svc.Get(ctx, target)
		if got != want || !errors.Is(err, repositoryErr) || calls != 1 {
			t.Fatalf("Get = (%+v, %v), calls=%d, want repository result (%+v, %v) once", got, err, calls, want, repositoryErr)
		}
	}
}

func TestUpdateRejectsInvalidChangeBeforeRepositoryAccess(t *testing.T) {
	for _, tt := range []struct {
		name, actor, field string
		version            int64
	}{
		{name: "negative version", version: -1, actor: "user:" + scan.GenerateID(), field: "version"},
		{name: "missing actor", version: 0, field: "actor"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(repositoryStub{update: func(context.Context, Target, Change, string) (sourcecontrol.State, error) {
				t.Fatal("invalid change reached repository")
				return sourcecontrol.State{}, nil
			}})
			got, err := svc.Update(context.Background(), Target{}, Change{Version: tt.version}, tt.actor)
			var invalid *validation.Error
			if !errors.As(err, &invalid) || invalid.Field != tt.field {
				t.Fatalf("Update error = %v, want validation for %s", err, tt.field)
			}
			if got != (sourcecontrol.State{}) {
				t.Fatalf("invalid change returned state %+v", got)
			}
		})
	}
}

func TestUpdatePreservesCASVersionAndDoesNotRetryConflict(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	target := Target{TenantID: scan.GenerateID(), Kind: "checks", ID: 42, Mode: sourcecontrol.Check, SourceUID: "check-42", ResourceGroupID: scan.GenerateID()}
	actor := "user:" + scan.GenerateID()
	conflict := &resource.ConflictError{Resource: "source_control", ID: target.SourceUID, Message: "refresh before saving"}
	for _, tt := range []struct {
		name   string
		change Change
		err    error
	}{
		{name: "create explicit controls", change: Change{Version: 0, SchedulePaused: true}},
		{name: "update controls", change: Change{Version: 7, ExecutionPaused: true}},
		{name: "stale controls", change: Change{Version: 6, SchedulePaused: true, ExecutionPaused: true}, err: conflict},
		{name: "storage failure", change: Change{Version: 7}, err: errors.New("control write unavailable")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			want := sourcecontrol.State{Mode: target.Mode, SourceUID: target.SourceUID, Version: 8, SchedulePaused: true, ExecutionPaused: true, Tombstoned: true, Actor: actor, UpdatedAt: time.Date(2026, 11, 15, 2, 3, 0, 0, time.UTC)}
			calls := 0
			svc := NewService(repositoryStub{update: func(gotCtx context.Context, gotTarget Target, gotChange Change, gotActor string) (sourcecontrol.State, error) {
				calls++
				if gotCtx != ctx || gotTarget != target || gotChange != tt.change || gotActor != actor {
					t.Fatalf("Update lost trusted identity or CAS inputs: target=%+v change=%+v actor=%q", gotTarget, gotChange, gotActor)
				}
				return want, tt.err
			}})
			got, err := svc.Update(ctx, target, tt.change, actor)
			if got != want || !errors.Is(err, tt.err) || calls != 1 {
				t.Fatalf("Update = (%+v, %v), calls=%d, want unchanged repository result once", got, err, calls)
			}
		})
	}
}
