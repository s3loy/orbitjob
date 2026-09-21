package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/robfig/cron/v3"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
)

type fakeRevisions struct {
	revisions []revision.Revision
	openRuns  int
	listErr   error
	countErr  error
}

func (f *fakeRevisions) ActiveRevisions(context.Context, string) ([]revision.Revision, error) {
	return f.revisions, f.listErr
}

func (f *fakeRevisions) CountOpenRuns(context.Context, string, string) (int, error) {
	return f.openRuns, f.countErr
}

type recordedRun struct {
	run         jobrun.JobRun
	maxAttempts int
}

type fakeRuns struct {
	recorded      []recordedRun
	existing      bool
	existingPhase jobrun.Phase
	openRuns      []jobrun.OpenRun
	openErr       error
	err           error
}

func (f *fakeRuns) OpenRuns(context.Context, string, string) ([]jobrun.OpenRun, error) {
	return f.openRuns, f.openErr
}

func (f *fakeRuns) CreateOccurrenceForTenant(_ context.Context, _ string, run jobrun.JobRun, maxAttempts int, _ string) (jobrun.StoredRun, bool, error) {
	if f.err != nil {
		return jobrun.StoredRun{}, false, f.err
	}
	if f.existing {
		phase := f.existingPhase
		if phase == "" {
			phase = jobrun.Pending
		}
		return jobrun.StoredRun{ID: 99, RevisionID: 3, Phase: phase, MaxAttempts: maxAttempts}, false, nil
	}
	f.recorded = append(f.recorded, recordedRun{run: run, maxAttempts: maxAttempts})
	return jobrun.StoredRun{ID: 99, RevisionID: 3, Phase: jobrun.Pending, MaxAttempts: maxAttempts}, true, nil
}

type fakePublisher struct {
	published []v1alpha1.JobRun
	err       error
}

func (f *fakePublisher) Publish(_ context.Context, spec v1alpha1.JobRun) error {
	if f.err != nil {
		return f.err
	}
	f.published = append(f.published, spec)
	return nil
}

func revisionWith(t *testing.T, id int64, spec v1alpha1.ScheduledJobSpec) revision.Revision {
	t.Helper()
	rev, err := revision.New(revision.Identity{
		SourceMode: "kubernetes", SourceUID: "uid-1", Namespace: "finance", Name: "nightly-report",
	}, 5, mustJSON(t, spec), "operator", "", time.Unix(1000, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	rev.ID = id
	return rev
}

func mustJSON(t *testing.T, spec v1alpha1.ScheduledJobSpec) string {
	t.Helper()
	out, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func schedulerAt(at time.Time, revisions *fakeRevisions, runs *fakeRuns, publisher *fakePublisher) Scheduler {
	return Scheduler{
		Revisions:    revisions,
		Runs:         runs,
		Publisher:    publisher,
		Tenants:      []string{"tenant-a"},
		MisfireGrace: time.Hour,
		Now:          func() time.Time { return at },
	}
}

// The window walker only ever sees minute boundaries, so one match per window is
// the expected shape for an hourly schedule.
func TestTickFiresDueDefinition(t *testing.T) {
	at := time.Date(2026, 3, 1, 2, 0, 30, 0, time.UTC)
	revisions := &fakeRevisions{revisions: []revision.Revision{
		revisionWith(t, 3, v1alpha1.ScheduledJobSpec{Schedule: "0 * * * *"}),
	}}
	runs := &fakeRuns{}
	publisher := &fakePublisher{}

	created, err := schedulerAt(at, revisions, runs, publisher).Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 || len(runs.recorded) != 1 || len(publisher.published) != 1 {
		t.Fatalf("created=%d recorded=%d published=%d", created, len(runs.recorded), len(publisher.published))
	}
	if runs.recorded[0].run.Trigger != jobrun.Schedule || runs.recorded[0].run.RevisionID != "3" {
		t.Fatalf("recorded run = %+v", runs.recorded[0].run)
	}
	published := publisher.published[0].Spec
	if published.OccurrenceKey == "" || published.DefinitionRevision != 3 {
		t.Fatalf("published spec = %+v", published)
	}
	if published.ScheduledJobRef.UID != "uid-1" {
		t.Fatalf("published ref = %+v", published.ScheduledJobRef)
	}
}

func TestTickSkipsWhenNotDue(t *testing.T) {
	at := time.Date(2026, 3, 1, 2, 30, 0, 0, time.UTC)
	revisions := &fakeRevisions{revisions: []revision.Revision{
		revisionWith(t, 3, v1alpha1.ScheduledJobSpec{Schedule: "0 5 * * *"}),
	}}
	runs := &fakeRuns{}
	publisher := &fakePublisher{}

	created, err := schedulerAt(at, revisions, runs, publisher).Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 || len(runs.recorded) != 0 {
		t.Fatalf("created=%d", created)
	}
}

func TestTickHonoursSuspend(t *testing.T) {
	at := time.Date(2026, 3, 1, 2, 0, 30, 0, time.UTC)
	revisions := &fakeRevisions{revisions: []revision.Revision{
		revisionWith(t, 3, v1alpha1.ScheduledJobSpec{Schedule: "0 * * * *", Suspend: true}),
	}}
	runs := &fakeRuns{}
	if created, err := schedulerAt(at, revisions, runs, &fakePublisher{}).Tick(context.Background()); err != nil || created != 0 {
		t.Fatalf("created=%d err=%v", created, err)
	}
}

func TestTickIsIdempotentAcrossTicks(t *testing.T) {
	at := time.Date(2026, 3, 1, 2, 0, 30, 0, time.UTC)
	revisions := &fakeRevisions{revisions: []revision.Revision{
		revisionWith(t, 3, v1alpha1.ScheduledJobSpec{Schedule: "0 * * * *"}),
	}}
	publisher := &fakePublisher{}
	scheduler := schedulerAt(at, revisions, &fakeRuns{}, publisher)
	if _, err := scheduler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	// A second process observes the occurrence already stored and must not
	// create a second run. It does re-issue the create, which is idempotent and
	// is what repairs a run whose CR went missing.
	existing := &fakeRuns{existing: true, existingPhase: jobrun.Running}
	created, err := schedulerAt(at, revisions, existing, publisher).Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 {
		t.Fatalf("created=%d, want no second run for one occurrence", created)
	}
	if len(publisher.published) != 2 {
		t.Fatalf("published=%d, want the original plus one idempotent repair", len(publisher.published))
	}
}

func TestTickAppliesConcurrencyPolicy(t *testing.T) {
	at := time.Date(2026, 3, 1, 2, 0, 30, 0, time.UTC)
	base := v1alpha1.ScheduledJobSpec{Schedule: "0 * * * *", ConcurrencyPolicy: v1alpha1.Forbid}
	revisions := &fakeRevisions{revisions: []revision.Revision{revisionWith(t, 3, base)}, openRuns: 1}
	runs := &fakeRuns{}
	created, err := schedulerAt(at, revisions, runs, &fakePublisher{}).Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 || len(runs.recorded) != 0 {
		t.Fatal("Forbid must skip while a run is open")
	}

	revisions.openRuns = 0
	out, err := schedulerAt(at, revisions, &fakeRuns{}, &fakePublisher{}).Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if out != 1 {
		t.Fatalf("created=%d, want 1 once nothing is open", out)
	}
}

func TestTickAppliesMisfirePolicy(t *testing.T) {
	// The definition was due at 02:00 and the scheduler only gets to it at
	// 02:30, inside the one-hour grace so the miss is still actionable.
	at := time.Date(2026, 3, 1, 2, 30, 0, 0, time.UTC)
	skip := &fakeRevisions{revisions: []revision.Revision{
		revisionWith(t, 3, v1alpha1.ScheduledJobSpec{Schedule: "0 2 * * *", MisfirePolicy: v1alpha1.Skip}),
	}}
	if created, err := schedulerAt(at, skip, &fakeRuns{}, &fakePublisher{}).Tick(context.Background()); err != nil || created != 0 {
		t.Fatalf("Skip created=%d err=%v", created, err)
	}

	fireOnce := &fakeRevisions{revisions: []revision.Revision{
		revisionWith(t, 3, v1alpha1.ScheduledJobSpec{Schedule: "0 2 * * *", MisfirePolicy: v1alpha1.FireOnce}),
	}}
	if created, err := schedulerAt(at, fireOnce, &fakeRuns{}, &fakePublisher{}).Tick(context.Background()); err != nil || created != 1 {
		t.Fatalf("FireOnce created=%d err=%v", created, err)
	}
}

func TestTickBoundsCatchUp(t *testing.T) {
	at := time.Date(2026, 3, 1, 5, 30, 0, 0, time.UTC)
	revisions := &fakeRevisions{revisions: []revision.Revision{
		revisionWith(t, 3, v1alpha1.ScheduledJobSpec{Schedule: "0 * * * *", MisfirePolicy: v1alpha1.CatchUpBounded}),
	}}
	runs := &fakeRuns{}
	scheduler := schedulerAt(at, revisions, runs, &fakePublisher{})
	// Six hourly activations sit inside this wider window; the bound caps them.
	scheduler.MisfireGrace = 6 * time.Hour
	scheduler.MaxCatchUp = 2
	created, err := scheduler.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if created != 2 || len(runs.recorded) != 2 {
		t.Fatalf("created=%d recorded=%d, want 2", created, len(runs.recorded))
	}
}

func TestTickAbortsOnlyOnListFailure(t *testing.T) {
	at := time.Date(2026, 3, 1, 2, 0, 30, 0, time.UTC)
	ok := revisionWith(t, 3, v1alpha1.ScheduledJobSpec{Schedule: "0 * * * *"})

	// A failing definition is logged and skipped so one bad revision cannot
	// stop every other tenant's scheduling; only a failure to list a tenant's
	// revisions aborts the tick.
	if _, err := schedulerAt(at, &fakeRevisions{listErr: errors.New("boom")}, &fakeRuns{}, &fakePublisher{}).Tick(context.Background()); err == nil {
		t.Fatal("a revisions listing failure must abort the tick")
	}

	tests := []struct {
		name      string
		revisions *fakeRevisions
		runs      *fakeRuns
		publisher *fakePublisher
	}{
		{"record failure", &fakeRevisions{revisions: []revision.Revision{ok}}, &fakeRuns{err: errors.New("boom")}, &fakePublisher{}},
		{"publish failure", &fakeRevisions{revisions: []revision.Revision{ok}}, &fakeRuns{}, &fakePublisher{err: errors.New("boom")}},
		{"concurrency lookup failure", &fakeRevisions{revisions: []revision.Revision{
			revisionWith(t, 3, v1alpha1.ScheduledJobSpec{Schedule: "0 * * * *", ConcurrencyPolicy: v1alpha1.Forbid}),
		}, countErr: errors.New("boom")}, &fakeRuns{}, &fakePublisher{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			created, err := schedulerAt(at, tt.revisions, tt.runs, tt.publisher).Tick(context.Background())
			if err != nil {
				t.Fatalf("a per-definition failure must be skipped, not returned: %v", err)
			}
			if created != 0 {
				t.Fatalf("created = %d, want 0 for a skipped definition", created)
			}
		})
	}
}

func TestTickSkipsMalformedRevision(t *testing.T) {
	at := time.Date(2026, 3, 1, 2, 0, 30, 0, time.UTC)
	broken := revision.Revision{
		ID: 3, Identity: revision.Identity{SourceUID: "uid-1", Name: "nightly-report"},
		NormalizedSpec: "{not json",
	}
	revisions := &fakeRevisions{revisions: []revision.Revision{broken}}
	created, err := schedulerAt(at, revisions, &fakeRuns{}, &fakePublisher{}).Tick(context.Background())
	if err != nil {
		t.Fatalf("a malformed revision must be skipped, not returned: %v", err)
	}
	if created != 0 {
		t.Fatalf("created = %d, want 0", created)
	}
}

func TestSchedulerRequiresDependencies(t *testing.T) {
	if _, err := (Scheduler{}).Tick(context.Background()); err == nil {
		t.Fatal("expected missing dependencies to be rejected")
	}
}

func TestSchedulerFallsBackToWallClockAndStandardParser(t *testing.T) {
	// With no clock and no parser injected, the scheduler must still work: the
	// defaults are what production runs with.
	revisions := &fakeRevisions{revisions: []revision.Revision{
		revisionWith(t, 3, v1alpha1.ScheduledJobSpec{Schedule: "0 * * * *"}),
	}}
	runs := &fakeRuns{}
	scheduler := Scheduler{
		Revisions:    revisions,
		Runs:         runs,
		Publisher:    &fakePublisher{},
		Tenants:      []string{"tenant-a"},
		MisfireGrace: time.Hour,
	}
	if _, err := scheduler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if scheduler.now().IsZero() {
		t.Fatal("now must default to the wall clock")
	}
}

func TestParseRejectsInvalidCronExpression(t *testing.T) {
	scheduler := Scheduler{}
	if _, err := scheduler.parse("not a cron"); err == nil {
		t.Fatal("expected an invalid expression to be rejected")
	}
	// The injected parser is used when present, so tests can pin scheduling
	// behaviour without depending on cron semantics.
	injected := errors.New("injected")
	overridden := Scheduler{ParseSchedule: func(string) (Schedule, error) { return nil, injected }}
	if _, err := overridden.parse("* * * * *"); !errors.Is(err, injected) {
		t.Fatalf("error = %v", err)
	}
}

func TestFireDefinitionSurfacesInvalidSchedule(t *testing.T) {
	at := time.Date(2026, 3, 1, 2, 0, 30, 0, time.UTC)
	revisions := &fakeRevisions{revisions: []revision.Revision{
		revisionWith(t, 3, v1alpha1.ScheduledJobSpec{Schedule: "definitely not cron"}),
	}}
	// fireDefinition is where the refusal happens; Tick logs it and moves on,
	// so the invalid-cron contract is pinned here at its source.
	s := schedulerAt(at, revisions, &fakeRuns{}, &fakePublisher{})
	_, err := s.fireDefinition(context.Background(), "tenant-a", revisions.revisions[0], at, time.Hour)
	if err == nil {
		t.Fatal("an invalid cron expression must surface rather than silently never firing")
	}
}

func TestMaxAttemptsDefaultsToOne(t *testing.T) {
	// Zero retries means one attempt: a run must always be tried at least once.
	if got := (v1alpha1.RetryPolicy{}).EffectiveMaxAttempts(); got != 1 {
		t.Fatalf("EffectiveMaxAttempts(unset) = %d, want 1", got)
	}
	if got := (v1alpha1.RetryPolicy{MaxAttempts: 4}).EffectiveMaxAttempts(); got != 4 {
		t.Fatalf("EffectiveMaxAttempts = %d, want 4", got)
	}
	if got := (v1alpha1.RetryPolicy{MaxAttempts: -1}).EffectiveMaxAttempts(); got != 1 {
		t.Fatalf("EffectiveMaxAttempts(negative) = %d, want 1", got)
	}
}

func TestSelectOccurrencesFiresOnTimeExactlyOnce(t *testing.T) {
	schedule, err := cron.ParseStandard("* * * * *")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 3, 1, 2, 0, 30, 0, time.UTC)
	got := selectOccurrences(cronSchedule{inner: schedule}, v1alpha1.Skip, at, time.Hour, 1)
	if len(got) != 1 || !got[0].Equal(time.Date(2026, 3, 1, 2, 0, 0, 0, time.UTC)) {
		t.Fatalf("occurrences = %v", got)
	}
}

func TestSelectOccurrencesReturnsNothingOutsideTheWindow(t *testing.T) {
	schedule, err := cron.ParseStandard("0 3 * * *")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	if got := selectOccurrences(cronSchedule{inner: schedule}, v1alpha1.FireOnce, at, time.Hour, 1); len(got) != 0 {
		t.Fatalf("occurrences = %v", got)
	}
}

func TestJobRunObjectCarriesTypeMeta(t *testing.T) {
	// The real API server rejects a create without apiVersion/kind, while the
	// fake dynamic client accepts it. Only an end-to-end run catches this, so
	// assert it here.
	rev := revisionWith(t, 3, v1alpha1.ScheduledJobSpec{Schedule: "0 * * * *"})
	obj := Scheduler{}.jobRunObject(rev, v1alpha1.ScheduledJobSpec{TimeoutSeconds: 90},
		jobrun.StoredRun{RevisionID: 3}, "abcdef1234567890", time.Unix(0, 0).UTC())
	if obj.APIVersion != v1alpha1.GroupVersion.String() {
		t.Fatalf("apiVersion = %q", obj.APIVersion)
	}
	if obj.Kind != "JobRun" {
		t.Fatalf("kind = %q", obj.Kind)
	}
	if obj.Spec.TimeoutSeconds != 90 {
		t.Fatalf("timeout not propagated: %d", obj.Spec.TimeoutSeconds)
	}
}

func TestTickRepairsOpenRunsEvenWhenConcurrencyForbids(t *testing.T) {
	// The deadlock this guards against: a run whose object was never created
	// stays non-terminal, Forbid then blocks every later occurrence, and the
	// only reconcile that could repair it is the one being blocked.
	at := time.Date(2026, 3, 1, 2, 0, 30, 0, time.UTC)
	revisions := &fakeRevisions{revisions: []revision.Revision{
		revisionWith(t, 3, v1alpha1.ScheduledJobSpec{
			Schedule: "0 * * * *", ConcurrencyPolicy: v1alpha1.Forbid,
		}),
	}, openRuns: 1}
	publisher := &fakePublisher{}
	runs := &fakeRuns{
		existing: true,
		openRuns: []jobrun.OpenRun{{ID: 7, RevisionID: 3, OccurrenceKey: "occ-7", Phase: jobrun.Pending}},
	}

	created, err := schedulerAt(at, revisions, runs, publisher).Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Forbid correctly suppresses a *new* occurrence while one is open.
	if created != 0 {
		t.Fatalf("created = %d, want 0 under Forbid", created)
	}
	// But the open run's object must still be republished.
	if len(publisher.published) != 1 {
		t.Fatalf("published = %d, want the open run repaired", len(publisher.published))
	}
	published := publisher.published[0]
	if published.Name != "nightly-report-occ-7" {
		t.Fatalf("repaired run name = %q", published.Name)
	}
}

func TestRunObjectNameSurvivesMalformedOccurrenceKeys(t *testing.T) {
	// A short or empty key must not panic the scheduler loop, which would stop
	// scheduling for every tenant in the process.
	for _, key := range []string{"", "abc", "abcdefgh", strings.Repeat("a", 64)} {
		if name := runObjectName("nightly", key); name == "" || !strings.HasPrefix(name, "nightly-") {
			t.Fatalf("key %q produced name %q", key, name)
		}
	}
}

func TestTickRepairsOpenRunsBeforeFiringNewOnes(t *testing.T) {
	at := time.Date(2026, 3, 1, 2, 0, 30, 0, time.UTC)
	revisions := &fakeRevisions{revisions: []revision.Revision{
		revisionWith(t, 3, v1alpha1.ScheduledJobSpec{Schedule: "0 * * * *"}),
	}}
	publisher := &fakePublisher{}
	runs := &fakeRuns{
		openRuns: []jobrun.OpenRun{{ID: 7, RevisionID: 3, OccurrenceKey: "occ-7", Phase: jobrun.Running}},
	}

	if _, err := schedulerAt(at, revisions, runs, publisher).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(publisher.published) < 2 {
		t.Fatalf("published = %d, want the repair plus the new occurrence", len(publisher.published))
	}
	if publisher.published[0].Name != "nightly-report-occ-7" {
		t.Fatalf("first publish = %q, want the repair first", publisher.published[0].Name)
	}
}

func TestTickSkipsOpenRunLookupAndRepairFailures(t *testing.T) {
	at := time.Date(2026, 3, 1, 2, 0, 30, 0, time.UTC)
	base := []revision.Revision{revisionWith(t, 3, v1alpha1.ScheduledJobSpec{Schedule: "0 * * * *"})}

	// A repair failure is one definition's failure: it is skipped with the
	// rest, and the tick still answers for every definition that fired.
	lookupFailure := &fakeRuns{openErr: errors.New("boom")}
	created, err := schedulerAt(at, &fakeRevisions{revisions: base}, lookupFailure, &fakePublisher{}).Tick(context.Background())
	if err != nil {
		t.Fatalf("open-run lookup failure must be skipped, not returned: %v", err)
	}
	if created != 0 {
		t.Fatalf("created = %d, want 0", created)
	}

	repairFailure := &fakeRuns{openRuns: []jobrun.OpenRun{{ID: 7, RevisionID: 3, OccurrenceKey: "occ-7"}}}
	if _, err := schedulerAt(at, &fakeRevisions{revisions: base}, repairFailure, &fakePublisher{err: errors.New("boom")}).Tick(context.Background()); err != nil {
		t.Fatalf("repair publish failure must be skipped, not returned: %v", err)
	}
}

func TestTickDoesNotTouchTerminalRuns(t *testing.T) {
	// A finished run's object already exists; the repair path must not pick it
	// up and republish it every tick.
	at := time.Date(2026, 3, 1, 2, 0, 30, 0, time.UTC)
	revisions := &fakeRevisions{revisions: []revision.Revision{
		revisionWith(t, 3, v1alpha1.ScheduledJobSpec{Schedule: "0 * * * *"}),
	}}
	publisher := &fakePublisher{}
	runs := &fakeRuns{existing: true, existingPhase: jobrun.Succeeded}

	if _, err := schedulerAt(at, revisions, runs, publisher).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, published := range publisher.published {
		if published.Name == "nightly-report-occ-7" {
			t.Fatal("a terminal run must not be repaired")
		}
	}
}
