package command

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	runquery "orbitjob/internal/admin/app/run/query"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
	"orbitjob/internal/platform/metrics"
)

// CancelRunUseCase is the write gate for stopping a run: validate, read the run
// tenant-scoped, refuse a ledger row that cannot address its Custom Resource,
// and patch nothing when the run is already terminal. The stubs capture every
// call so each test pins which of those steps actually ran.

var errCancelStoreDown = errors.New("cancel store down")
var errCancelPatchRefused = errors.New("patch refused")

// ulidTenant matches tenants.id: a CHAR(26) ULID, the shape the query gate
// demands before a cancel request is looked up.
const ulidTenant = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

type fakeLocator struct {
	in    runquery.GetInput
	calls int

	target runquery.CancelTarget
	err    error
}

func (f *fakeLocator) GetCancelTarget(_ context.Context, in runquery.GetInput) (runquery.CancelTarget, error) {
	f.calls++
	f.in = in
	return f.target, f.err
}

type fakeCanceller struct {
	namespace, name string
	calls           int

	err error
}

func (f *fakeCanceller) RequestCancel(_ context.Context, namespace, name string) error {
	f.calls++
	f.namespace = namespace
	f.name = name
	return f.err
}

func TestCancelRunRefusedInputNeverLooksUp(t *testing.T) {
	tests := []struct {
		name string
		in   CancelInput

		wantField   string
		wantScope   bool
		wantSubject string
	}{
		{
			name:      "run id zero is refused",
			in:        CancelInput{RunID: 0, TenantID: ulidTenant},
			wantField: "id",
		},
		{
			name:      "a negative run id is refused",
			in:        CancelInput{RunID: -2, TenantID: ulidTenant},
			wantField: "id",
		},
		{
			name:      "a scoped caller is refused because a run has no group",
			in:        CancelInput{RunID: 7, TenantID: ulidTenant, ResourceGroupID: "rg-7"},
			wantScope: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			canceller := &fakeCanceller{}
			uc := NewCancelRunUseCase(&fakeLocator{}, canceller)

			_, err := uc.Cancel(context.Background(), tc.in)
			if err == nil {
				t.Fatal("expected a refusal")
			}

			switch {
			case tc.wantField != "":
				var verr *validation.Error
				if !errors.As(err, &verr) {
					t.Fatalf("got %v (%T), want *validation.Error", err, err)
				}
				if verr.Field != tc.wantField {
					t.Fatalf("refusal names field %q, want %q", verr.Field, tc.wantField)
				}
			case tc.wantScope:
				var serr *resource.ScopeError
				if !errors.As(err, &serr) {
					t.Fatalf("got %v (%T), want *resource.ScopeError", err, err)
				}
				if serr.Resource != "run" || serr.Scope != tc.in.ResourceGroupID {
					t.Fatalf("scope refusal = %+v, want resource %q scope %q", serr, "run", tc.in.ResourceGroupID)
				}
			}

			if canceller.calls != 0 {
				t.Fatalf("refused request patched Kubernetes %d times", canceller.calls)
			}
		})
	}
}

func TestCancelRunRequiresDependencies(t *testing.T) {
	tests := []struct {
		name      string
		locator   runLocator
		canceller runCanceller
		wantText  string
	}{
		{
			name:      "a nil locator is refused",
			locator:   nil,
			canceller: &fakeCanceller{},
			wantText:  "run locator is required",
		},
		{
			name:      "a nil canceller is refused",
			locator:   &fakeLocator{},
			canceller: nil,
			wantText:  "run canceller is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			uc := NewCancelRunUseCase(tc.locator, tc.canceller)

			_, err := uc.Cancel(context.Background(), CancelInput{RunID: 7, TenantID: ulidTenant})
			if err == nil || !strings.Contains(err.Error(), tc.wantText) {
				t.Fatalf("got %v, want it to name %q", err, tc.wantText)
			}
		})
	}
}

func TestCancelRunLookupFailurePropagates(t *testing.T) {
	locator := &fakeLocator{err: errCancelStoreDown}
	uc := NewCancelRunUseCase(locator, &fakeCanceller{})

	_, err := uc.Cancel(context.Background(), CancelInput{RunID: 7, TenantID: ulidTenant})
	if !errors.Is(err, errCancelStoreDown) {
		t.Fatalf("got %v, want the store error", err)
	}
	if !strings.Contains(err.Error(), "read run for cancel") {
		t.Fatalf("error %v does not name the failed step", err)
	}
}

func TestCancelRunUnaddressableTargetRefused(t *testing.T) {
	// The revision row always carries both identity fields, so an empty value
	// means the ledger row is incomplete: patching an unnameable object would
	// silently do nothing, so the request is refused instead.
	tests := []struct {
		name   string
		target runquery.CancelTarget
	}{
		{
			name:   "no definition name",
			target: runquery.CancelTarget{Namespace: "jobs", OccurrenceKey: "k"},
		},
		{
			name:   "no namespace",
			target: runquery.CancelTarget{ScheduledJobName: "nightly", OccurrenceKey: "k"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			canceller := &fakeCanceller{}
			uc := NewCancelRunUseCase(&fakeLocator{target: tc.target}, canceller)

			_, err := uc.Cancel(context.Background(), CancelInput{RunID: 7, TenantID: ulidTenant})
			if err == nil || !strings.Contains(err.Error(), "does not name its definition") {
				t.Fatalf("got %v, want the unaddressable-target refusal", err)
			}
			if canceller.calls != 0 {
				t.Fatalf("unaddressable target patched Kubernetes %d times", canceller.calls)
			}
		})
	}
}

func TestCancelRunTerminalPhaseIsFinalWithoutPatching(t *testing.T) {
	// A finished run is final and the observed phase is the answer; patching
	// stop intent onto it would be a lie about a run that already ended.
	tests := []struct {
		name  string
		phase string
	}{
		{name: "succeeded", phase: string(jobrun.Succeeded)},
		{name: "failed", phase: string(jobrun.Failed)},
		{name: "canceled", phase: string(jobrun.Canceled)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			locator := &fakeLocator{target: runquery.CancelTarget{
				Phase:            tc.phase,
				OccurrenceKey:    "aa1122334455",
				ScheduledJobName: "nightly",
				Namespace:        "jobs",
			}}
			canceller := &fakeCanceller{}
			uc := NewCancelRunUseCase(locator, canceller)

			got, err := uc.Cancel(context.Background(), CancelInput{RunID: 7, TenantID: ulidTenant})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !jobrun.Terminal(jobrun.Phase(got.Phase)) {
				t.Fatalf("phase %q is not terminal", got.Phase)
			}
			if got.Phase != tc.phase {
				t.Fatalf("result phase %q, want the observed %q", got.Phase, tc.phase)
			}
			if got.Namespace != "jobs" || got.OccurrenceKey != "aa1122334455" {
				t.Fatalf("result lost the resource identity: %+v", got)
			}
			if canceller.calls != 0 {
				t.Fatalf("terminal run patched Kubernetes %d times", canceller.calls)
			}
		})
	}
}

func TestCancelRunPatchesStopIntentOnTheDerivedName(t *testing.T) {
	locator := &fakeLocator{target: runquery.CancelTarget{
		Phase:            string(jobrun.Running),
		OccurrenceKey:    "deadbeef00cafe00",
		ScheduledJobName: "nightly-etl",
		Namespace:        "jobs",
	}}
	canceller := &fakeCanceller{}
	uc := NewCancelRunUseCase(locator, canceller)

	before := testutil.ToFloat64(metrics.RunCancelRequestsTotal)
	got, err := uc.Cancel(context.Background(), CancelInput{RunID: 7, TenantID: ulidTenant})
	after := testutil.ToFloat64(metrics.RunCancelRequestsTotal)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if canceller.calls != 1 {
		t.Fatalf("canceller called %d times, want exactly 1", canceller.calls)
	}
	// The JobRun name is the definition name plus the occurrence key's first
	// eight characters (v1alpha1.RunObjectName).
	if canceller.namespace != "jobs" || canceller.name != "nightly-etl-deadbeef" {
		t.Fatalf("patched ns=%q name=%q, want the derived object identity", canceller.namespace, canceller.name)
	}
	if got.Name != canceller.name || got.Namespace != "jobs" || got.Phase != string(jobrun.Running) {
		t.Fatalf("result disagrees with the patch that was sent: %+v", got)
	}
	if after-before != 1 {
		t.Fatalf("cancel counter moved %f, want exactly 1", after-before)
	}
	if locator.in.ID != 7 || locator.in.TenantID != ulidTenant {
		t.Fatalf("lookup got tenant=%q id=%d, want the normalized input", locator.in.TenantID, locator.in.ID)
	}
}

func TestCancelRunPatchFailurePropagates(t *testing.T) {
	locator := &fakeLocator{target: runquery.CancelTarget{
		Phase:            "running",
		OccurrenceKey:    "aa1122334455",
		ScheduledJobName: "nightly",
		Namespace:        "jobs",
	}}
	canceller := &fakeCanceller{err: errCancelPatchRefused}
	uc := NewCancelRunUseCase(locator, canceller)

	_, err := uc.Cancel(context.Background(), CancelInput{RunID: 7, TenantID: ulidTenant})
	if !errors.Is(err, errCancelPatchRefused) {
		t.Fatalf("got %v, want the patch error", err)
	}
	if !strings.Contains(err.Error(), "request cancel for run 7") {
		t.Fatalf("error %v does not name the run", err)
	}
}
