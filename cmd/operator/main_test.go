package main

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
	"orbitjob/internal/operator"
)

// ---------------------------------------------------------------------------
// loadTenantResolver
// ---------------------------------------------------------------------------

func TestLoadTenantResolver_NamespaceMapping(t *testing.T) {
	t.Setenv("OPERATOR_NAMESPACE_TENANTS", "ns-a=tenant-a, ns-b=tenant-b")
	t.Setenv("OPERATOR_TENANT_DEFAULT", "")

	resolver, err := loadTenantResolver()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mapping, ok := resolver.(operator.NamespaceTenantResolver)
	if !ok {
		t.Fatalf("expected NamespaceTenantResolver, got %T", resolver)
	}
	want := map[string]string{"ns-a": "tenant-a", "ns-b": "tenant-b"}
	if !reflect.DeepEqual(map[string]string(mapping.Tenants), want) {
		t.Fatalf("tenants = %v, want %v", mapping.Tenants, want)
	}
	if ids := resolver.TenantIDs(); !reflect.DeepEqual(ids, []string{"tenant-a", "tenant-b"}) {
		t.Fatalf("tenant ids = %v, want [tenant-a tenant-b]", ids)
	}
}

func TestLoadTenantResolver_DefaultTenantFallback(t *testing.T) {
	t.Setenv("OPERATOR_NAMESPACE_TENANTS", "")
	t.Setenv("OPERATOR_TENANT_DEFAULT", "tenant-solo")

	resolver, err := loadTenantResolver()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fallback, ok := resolver.(operator.DefaultTenantResolver)
	if !ok {
		t.Fatalf("expected DefaultTenantResolver, got %T", resolver)
	}
	if fallback.Tenant != "tenant-solo" {
		t.Fatalf("tenant = %q, want %q", fallback.Tenant, "tenant-solo")
	}
	if ids := resolver.TenantIDs(); !reflect.DeepEqual(ids, []string{"tenant-solo"}) {
		t.Fatalf("tenant ids = %v, want [tenant-solo]", ids)
	}
}

func TestLoadTenantResolver_RequiresTenantConfiguration(t *testing.T) {
	t.Setenv("OPERATOR_NAMESPACE_TENANTS", "")
	t.Setenv("OPERATOR_TENANT_DEFAULT", "")

	_, err := loadTenantResolver()
	if err == nil || !strings.Contains(err.Error(), "OPERATOR_NAMESPACE_TENANTS is required") {
		t.Fatalf("expected missing-mapping error, got %v", err)
	}
}

func TestLoadTenantResolver_RejectsInvalidMapping(t *testing.T) {
	t.Setenv("OPERATOR_NAMESPACE_TENANTS", "ns-a=tenant-a,ns-without-tenant")
	t.Setenv("OPERATOR_TENANT_DEFAULT", "tenant-solo")

	_, err := loadTenantResolver()
	if err == nil || !strings.Contains(err.Error(), "invalid namespace/tenant mapping") {
		t.Fatalf("expected invalid mapping error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// defaultWorkers
// ---------------------------------------------------------------------------

func TestDefaultWorkersIsTwo(t *testing.T) {
	if got := defaultWorkers(); got != 2 {
		t.Fatalf("defaultWorkers() = %d, want 2", got)
	}
}

// ---------------------------------------------------------------------------
// loop dependency fakes
// ---------------------------------------------------------------------------

type fakeRevisionSource struct {
	activeCalls atomic.Int64
	activeErr   error
}

func (f *fakeRevisionSource) ActiveRevisions(context.Context, string) ([]revision.Revision, error) {
	f.activeCalls.Add(1)
	if f.activeErr != nil {
		return nil, f.activeErr
	}
	return nil, nil
}

func (f *fakeRevisionSource) CountOpenRuns(context.Context, string, string) (int, error) {
	return 0, nil
}

type unusedRunRecorder struct{}

func (unusedRunRecorder) CreateOccurrenceForTenant(context.Context, string, jobrun.JobRun, int, string) (jobrun.StoredRun, bool, error) {
	return jobrun.StoredRun{}, false, errors.New("run recorder must not be called")
}

type unusedPublisher struct{}

func (unusedPublisher) Publish(context.Context, v1alpha1.JobRun) error {
	return errors.New("publisher must not be called")
}

type unusedPruner struct{}

func (unusedPruner) PrunableRuns(context.Context, string, string, int, int) ([]jobrun.PrunableRun, error) {
	return nil, errors.New("pruner must not be called")
}

func (unusedPruner) DeleteRun(context.Context, string, int64) (bool, error) {
	return false, errors.New("delete run must not be called")
}

type unusedRemover struct{}

func (unusedRemover) Remove(context.Context, string, string, string) error {
	return errors.New("remover must not be called")
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// loopFinished reports whether the done channel closes within the timeout.
func loopFinished(done <-chan struct{}, timeout time.Duration) bool {
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// ---------------------------------------------------------------------------
// runRetentionLoop
// ---------------------------------------------------------------------------

func TestRunRetentionLoop_DisabledByInvalidInterval(t *testing.T) {
	for _, interval := range []string{"", "0", "-2", "soon"} {
		done := make(chan struct{})
		go func() {
			defer close(done)
			runRetentionLoop(context.Background(), quietLogger(), retentionDeps{
				Revisions: &fakeRevisionSource{},
				Pruner:    &unusedPruner{},
				Remover:   unusedRemover{},
				Tenants:   []string{"tenant-a"},
				Interval:  interval,
			})
		}()
		if !loopFinished(done, time.Second) {
			t.Fatalf("interval %q: retention loop did not exit immediately", interval)
		}
	}
}

func TestRunRetentionLoop_StopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runRetentionLoop(ctx, quietLogger(), retentionDeps{
			Revisions: &fakeRevisionSource{},
			Pruner:    &unusedPruner{},
			Remover:   unusedRemover{},
			Tenants:   []string{"tenant-a"},
			Interval:  "1",
		})
	}()

	cancel()
	if !loopFinished(done, time.Second) {
		t.Fatal("retention loop did not stop after context cancel")
	}
}

// ---------------------------------------------------------------------------
// runScheduleLoop
// ---------------------------------------------------------------------------

func TestRunScheduleLoop_DisabledByInvalidInterval(t *testing.T) {
	for _, interval := range []string{"", "0", "-1", "five"} {
		revisions := &fakeRevisionSource{}
		done := make(chan struct{})
		go func() {
			defer close(done)
			runScheduleLoop(context.Background(), quietLogger(), scheduleDeps{
				Revisions: revisions,
				Runs:      unusedRunRecorder{},
				Publisher: unusedPublisher{},
				Tenants:   []string{"tenant-a"},
				Interval:  interval,
			})
		}()
		if !loopFinished(done, time.Second) {
			t.Fatalf("interval %q: schedule loop did not exit immediately", interval)
		}
		if got := revisions.activeCalls.Load(); got != 0 {
			t.Fatalf("interval %q: active revisions calls = %d, want 0", interval, got)
		}
	}
}

func TestRunScheduleLoop_TicksUntilContextCancel(t *testing.T) {
	// A failing revision source makes every tick an error, which is exactly the
	// path the loop must survive: log, keep going, stop only when the context ends.
	revisions := &fakeRevisionSource{activeErr: errors.New("revision store unavailable")}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		runScheduleLoop(ctx, quietLogger(), scheduleDeps{
			Revisions: revisions,
			Runs:      unusedRunRecorder{},
			Publisher: unusedPublisher{},
			Tenants:   []string{"tenant-a"},
			Interval:  "1",
		})
	}()

	deadline := time.Now().Add(3 * time.Second)
	for revisions.activeCalls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if revisions.activeCalls.Load() == 0 {
		t.Fatal("schedule loop never ticked with a valid interval")
	}

	cancel()
	if !loopFinished(done, 2*time.Second) {
		t.Fatal("schedule loop did not stop after context cancel")
	}
}

// ---------------------------------------------------------------------------
// run wiring
// ---------------------------------------------------------------------------

// wiringRecorder captures what run() hands to the injectable dependencies.
// run() executes on the test goroutine, so plain fields are safe.
type wiringRecorder struct {
	ran        bool
	dsn        string
	config     operator.Config
	controller operator.Controller
}

// stubWiring swaps the process's injectable dependencies for test doubles and
// restores the originals when the test ends.
func stubWiring(t *testing.T) *wiringRecorder {
	t.Helper()

	origOpenDB, origInCluster, origRun := openDBFn, inClusterFn, runFn
	t.Cleanup(func() {
		openDBFn, inClusterFn, runFn = origOpenDB, origInCluster, origRun
	})

	recorder := &wiringRecorder{}
	runFn = func(_ context.Context, controller operator.Controller) error {
		recorder.ran = true
		recorder.controller = controller
		return nil
	}
	// A stub that fails is a safe default; individual tests replace it.
	inClusterFn = func(operator.Config) (operator.Controller, error) {
		return operator.Controller{}, errors.New("in-cluster config unavailable")
	}
	return recorder
}

func openDBSucceeding(t *testing.T, db *sql.DB, recorder *wiringRecorder) {
	t.Helper()
	openDBFn = func(dsn string) (*sql.DB, error) {
		recorder.dsn = dsn
		return db, nil
	}
}

func clearDSNEnv(t *testing.T) {
	t.Helper()
	t.Setenv("OPERATOR_DSN", "")
	t.Setenv("RUNTIME_DSN", "")
	t.Setenv("DATABASE_DSN", "")
}

func requireRunError(t *testing.T, want string) {
	t.Helper()
	err := run()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error containing %q, got %v", want, err)
	}
}

func TestRun_FailsWithoutDatabaseDSN(t *testing.T) {
	clearDSNEnv(t)
	stubWiring(t)
	requireRunError(t, "resolve database dsn")
}

func TestRun_FailsWhenDatabaseOpenFails(t *testing.T) {
	clearDSNEnv(t)
	t.Setenv("OPERATOR_DSN", "postgres://operator-test")

	recorder := stubWiring(t)
	openDBFn = func(string) (*sql.DB, error) {
		return nil, errors.New("connection refused")
	}
	requireRunError(t, "open database")
	if recorder.ran {
		t.Fatal("run() must not reach the controller when the database is unavailable")
	}
}

func TestRun_FailsWhenDatabasePingFails(t *testing.T) {
	clearDSNEnv(t)
	t.Setenv("OPERATOR_DSN", "postgres://operator-test")

	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectPing().WillReturnError(errors.New("database is down"))

	recorder := stubWiring(t)
	openDBSucceeding(t, db, recorder)
	requireRunError(t, "ping database")
	if recorder.ran {
		t.Fatal("run() must not reach the controller when the database is down")
	}
}

func TestRun_FailsWithoutTenantConfiguration(t *testing.T) {
	clearDSNEnv(t)
	t.Setenv("OPERATOR_DSN", "postgres://operator-test")
	t.Setenv("OPERATOR_NAMESPACE_TENANTS", "")
	t.Setenv("OPERATOR_TENANT_DEFAULT", "")

	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	recorder := stubWiring(t)
	openDBSucceeding(t, db, recorder)
	requireRunError(t, "OPERATOR_NAMESPACE_TENANTS is required")
}

func TestRun_FailsWhenInClusterControllerFails(t *testing.T) {
	clearDSNEnv(t)
	t.Setenv("OPERATOR_DSN", "postgres://operator-test")
	t.Setenv("OPERATOR_NAMESPACE_TENANTS", "tasks=tenant-a")

	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	recorder := stubWiring(t)
	openDBSucceeding(t, db, recorder)
	inClusterFn = func(operator.Config) (operator.Controller, error) {
		return operator.Controller{}, errors.New("no cluster reachable")
	}
	requireRunError(t, "build in-cluster controller")
}

func TestRun_FailsWithoutDynamicClient(t *testing.T) {
	clearDSNEnv(t)
	t.Setenv("OPERATOR_DSN", "postgres://operator-test")
	t.Setenv("OPERATOR_NAMESPACE_TENANTS", "tasks=tenant-a")

	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	recorder := stubWiring(t)
	openDBSucceeding(t, db, recorder)
	inClusterFn = func(operator.Config) (operator.Controller, error) {
		// A controller without a dynamic client is a wiring defect run() must
		// reject instead of starting an operator that reconciles nothing.
		return operator.Controller{Kubernetes: fake.NewSimpleClientset()}, nil
	}
	requireRunError(t, "dynamic client is required")
}

func TestRun_WiresControllerAndRuns(t *testing.T) {
	clearDSNEnv(t)
	t.Setenv("OPERATOR_DSN", "postgres://operator-test")
	t.Setenv("OPERATOR_NAMESPACE_TENANTS", "tasks=tenant-a")
	// Invalid intervals make the singleton loops exit immediately, so the
	// test does not wait on a tick; port 0 binds a random free port.
	t.Setenv("OPERATOR_SCHEDULE_INTERVAL_SEC", "not-a-number")
	t.Setenv("OPERATOR_RETENTION_INTERVAL_MIN", "not-a-number")
	t.Setenv("OPERATOR_HEALTH_PORT", "0")

	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectPing()

	recorder := stubWiring(t)
	openDBSucceeding(t, db, recorder)
	inClusterFn = func(cfg operator.Config) (operator.Controller, error) {
		recorder.config = cfg
		return operator.Controller{
			Dynamic:    dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()),
			Kubernetes: fake.NewSimpleClientset(),
		}, nil
	}

	if err := run(); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !recorder.ran {
		t.Fatal("run() finished without invoking the controller run")
	}
	if recorder.dsn != "postgres://operator-test" {
		t.Fatalf("open database dsn = %q, want the resolved OPERATOR_DSN", recorder.dsn)
	}
	if recorder.config.Workers != 2 {
		t.Fatalf("controller workers = %d, want 2", recorder.config.Workers)
	}
	if recorder.config.Resync != resyncInterval {
		t.Fatalf("controller resync = %s, want %s", recorder.config.Resync, resyncInterval)
	}

	controller := recorder.controller
	if controller.Dynamic == nil || controller.Kubernetes == nil {
		t.Fatal("controller is missing its Kubernetes clients")
	}
	if controller.Reconcile == nil {
		t.Fatal("controller reconcile handler is not wired")
	}
	if controller.Log == nil {
		t.Fatal("controller logger is not wired")
	}
}
