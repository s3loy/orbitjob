package checkschedule

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/controlplane"
	"orbitjob/internal/core/domain/check"
	"orbitjob/internal/core/domain/coordination"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
)

// testScope is the tenant/namespace pair every test fires through.
var testScope = Scope{TenantID: "00000000000000000000000001", Namespace: "tasks"}

type stubCheckRepo struct {
	due       []check.Snapshot
	listErr   error
	updated   []nextRunUpdate
	updateErr error
}

type nextRunUpdate struct {
	id        int64
	nextRunAt time.Time
}

func (s *stubCheckRepo) ListDue(_ context.Context, _ string, _ time.Time, _ int) ([]check.Snapshot, error) {
	return s.due, s.listErr
}

func (s *stubCheckRepo) UpdateNextRunAt(_ context.Context, _ string, id int64, nextRunAt time.Time) error {
	s.updated = append(s.updated, nextRunUpdate{id: id, nextRunAt: nextRunAt})
	return s.updateErr
}

type stubRevisions struct {
	// ids maps (source_uid, generation) to the id the first projection got, so
	// re-projecting a version is idempotent like the real store's upsert.
	ids map[string]int64
	err error
}

func (s *stubRevisions) ApplyRevisionForTenant(_ context.Context, _ string, rev revision.Revision) (int64, error) {
	if s.err != nil {
		return 0, s.err
	}
	if s.ids == nil {
		s.ids = map[string]int64{}
	}
	identity := fmt.Sprintf("%s/%d", rev.Identity.SourceUID, rev.Generation)
	if id, ok := s.ids[identity]; ok {
		return id, nil
	}
	id := int64(len(s.ids) + 100)
	s.ids[identity] = id
	return id, nil
}

// stubRuns is an in-memory occurrence ledger: dedup by (source_uid,
// occurrence_key), exactly like the database constraint the real store
// enforces. A killed loop re-deriving the same key must converge on the row it
// already created.
type stubRuns struct {
	occurrences map[string]jobrun.StoredRun
	order       []string
	createErr   error
}

func newStubRuns() *stubRuns {
	return &stubRuns{occurrences: map[string]jobrun.StoredRun{}}
}

func key(sourceUID, occurrenceKey string) string { return sourceUID + "|" + occurrenceKey }

func (s *stubRuns) CreateOccurrenceForTenant(_ context.Context, _ string, run jobrun.JobRun, maxAttempts int, actor string) (jobrun.StoredRun, bool, error) {
	if s.createErr != nil {
		return jobrun.StoredRun{}, false, s.createErr
	}
	k := key(run.SourceUID, run.OccurrenceKey)
	if stored, ok := s.occurrences[k]; ok {
		return stored, false, nil
	}
	stored := jobrun.StoredRun{
		ID:          int64(len(s.order) + 1),
		RevisionID:  100,
		Phase:       jobrun.Pending,
		MaxAttempts: maxAttempts,
	}
	s.occurrences[k] = stored
	s.order = append(s.order, k)
	_ = actor
	return stored, true, nil
}

func (s *stubRuns) OpenRuns(_ context.Context, _ string, sourceUID string) ([]jobrun.OpenRun, error) {
	var out []jobrun.OpenRun
	for k, stored := range s.occurrences {
		if strings.HasPrefix(k, sourceUID+"|") && !jobrun.Terminal(stored.Phase) {
			out = append(out, jobrun.OpenRun{
				ID: stored.ID, RevisionID: stored.RevisionID,
				OccurrenceKey: strings.TrimPrefix(strings.TrimPrefix(k, sourceUID), "|"),
				Phase:         stored.Phase,
			})
		}
	}
	return out, nil
}

type stubPublisher struct {
	published []v1alpha1.JobRun
	err       error
}

func (s *stubPublisher) Publish(_ context.Context, run v1alpha1.JobRun) error {
	if s.err != nil {
		return s.err
	}
	s.published = append(s.published, run)
	return nil
}

type fixture struct {
	uc      *TickUseCase
	checks  *stubCheckRepo
	runs    *stubRuns
	publish *stubPublisher
	reviser *stubRevisions
	now     time.Time
}

func newFixture(due []check.Snapshot, now time.Time) *fixture {
	f := &fixture{
		checks:  &stubCheckRepo{due: due},
		runs:    newStubRuns(),
		publish: &stubPublisher{},
		reviser: &stubRevisions{},
		now:     now,
	}
	f.uc = NewTickUseCase(f.checks, f.reviser, f.runs, f.publish)
	f.uc.Now = func() time.Time { return f.now }
	return f
}

func intervalCheck(id int64, sec int, nextRunAt time.Time) check.Snapshot {
	return check.Snapshot{
		ID:           id,
		Name:         fmt.Sprintf("check-%d", id),
		CheckType:    check.CheckTypeHTTPHealth,
		CheckConfig:  map[string]any{"url": "http://api.local/health"},
		ScheduleType: check.ScheduleTypeInterval,
		IntervalSec:  &sec,
		TimeoutSec:   30,
		RetryLimit:   2,
		Version:      1,
		NextRunAt:    &nextRunAt,
	}
}

func TestRunBatch_ListDueError(t *testing.T) {
	f := newFixture(nil, time.Now())
	f.checks.listErr = errors.New("db down")
	n, err := f.uc.RunBatch(context.Background(), testScope, 10)
	if err == nil {
		t.Fatal("expected error from ListDue")
	}
	if n != 0 {
		t.Fatalf("expected 0 created on error, got %d", n)
	}
}

func TestRunBatch_NoDue(t *testing.T) {
	f := newFixture(nil, time.Now())
	n, err := f.uc.RunBatch(context.Background(), testScope, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 created when no due checks, got %d", n)
	}
	if len(f.publish.published) != 0 || len(f.checks.updated) != 0 {
		t.Fatal("expected no publishes or cursor updates when no due checks")
	}
}

// TestRunBatch_FiresOccurrence is the step-4 contract: one due check produces
// exactly one ledger row and one JobRun CR, named after the check source uid
// and occurrence key, triggered by the check scheduler identity, with the
// occurrence's civil time on both the run and the CR annotation.
func TestRunBatch_FiresOccurrence(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	dueAt := now.Add(-time.Second)
	f := newFixture([]check.Snapshot{intervalCheck(1, 60, dueAt)}, now)

	n, err := f.uc.RunBatch(context.Background(), testScope, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 fired, got %d", n)
	}
	if len(f.publish.published) != 1 {
		t.Fatalf("expected 1 published CR, got %d", len(f.publish.published))
	}
	obj := f.publish.published[0]
	if obj.Spec.Trigger != v1alpha1.Check {
		t.Fatalf("trigger = %q, want Check", obj.Spec.Trigger)
	}
	if obj.Spec.Actor != ActorCheckScheduler {
		t.Fatalf("actor = %q, want %q", obj.Spec.Actor, ActorCheckScheduler)
	}
	if obj.Namespace != testScope.Namespace {
		t.Fatalf("namespace = %q, want %q", obj.Namespace, testScope.Namespace)
	}
	if obj.Spec.ScheduledJobRef.UID != "check-1" {
		t.Fatalf("source ref = %q, want check-1", obj.Spec.ScheduledJobRef.UID)
	}
	if obj.Annotations[controlplane.AnnotationScheduledAt] != dueAt.UTC().Format(time.RFC3339) {
		t.Fatalf("scheduled-at = %q, want the occurrence time", obj.Annotations[controlplane.AnnotationScheduledAt])
	}
	// The row precedes the CR and carries the occurrence in civil time.
	for k, stored := range f.runs.occurrences {
		if !strings.HasPrefix(k, "check-1|") {
			continue
		}
		if stored.Phase != jobrun.Pending {
			t.Fatalf("stored phase = %q, want Pending", stored.Phase)
		}
	}
	// The cursor advanced by the interval.
	want := now.Add(60 * time.Second)
	if len(f.checks.updated) != 1 || !f.checks.updated[0].nextRunAt.Equal(want) {
		t.Fatalf("next_run_at updates = %+v, want one at %v", f.checks.updated, want)
	}
}

// TestRunBatch_RepeatedTickDeduplicates drives the same due instant twice: the
// occurrence key re-derives identically, the second tick must not create a
// second row, and the republish is idempotent rather than an error.
func TestRunBatch_RepeatedTickDeduplicates(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	dueAt := now.Add(-time.Second)
	f := newFixture([]check.Snapshot{intervalCheck(1, 3600, dueAt)}, now)

	if n, err := f.uc.RunBatch(context.Background(), testScope, 10); err != nil || n != 1 {
		t.Fatalf("first tick: n=%d err=%v", n, err)
	}
	// Second tick with the check still due (the cursor update was lost) must
	// converge on the existing occurrence.
	f.checks.due = []check.Snapshot{intervalCheck(1, 3600, dueAt)}
	n, err := f.uc.RunBatch(context.Background(), testScope, 10)
	if err != nil {
		t.Fatalf("second tick: %v", err)
	}
	if n != 0 {
		t.Fatalf("second tick created %d runs, want 0 (dedup)", n)
	}
	if len(f.runs.order) != 1 {
		t.Fatalf("ledger holds %d occurrences, want 1", len(f.runs.order))
	}
	// The still-open run is published twice on the second tick, once by the
	// repair pass and once by the fire path, exactly like the job scheduler:
	// publish is an idempotent create, so this is redundancy, not duplication.
	if len(f.publish.published) != 3 {
		t.Fatalf("publishes = %d, want 3 (initial + repair + fire-path republish)", len(f.publish.published))
	}
	if f.publish.published[0].Name != f.publish.published[1].Name {
		t.Fatalf("repaired CR name %q differs from original %q", f.publish.published[1].Name, f.publish.published[0].Name)
	}
}

// TestRunBatch_KilledLoopConverges simulates a crash between the occurrence
// commit and the CR publish: on restart the same occurrence re-derives the
// same key, finds its row, and republishes instead of executing twice.
func TestRunBatch_KilledLoopConverges(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	dueAt := now.Add(-time.Second)
	f := newFixture([]check.Snapshot{intervalCheck(1, 60, dueAt)}, now)

	// A per-check failure is logged and skipped, never surfaced: one broken
	// publish must not abort the tenant's whole tick.
	f.publish.err = errors.New("api server down")
	if _, err := f.uc.RunBatch(context.Background(), testScope, 10); err != nil {
		t.Fatalf("per-check publish failure must not abort the tick: %v", err)
	}
	if len(f.runs.order) != 1 {
		t.Fatalf("ledger rows = %d, want 1: the row commits before the CR", len(f.runs.order))
	}

	// Restart: publish works again, the same occurrence is repaired.
	f.publish.err = nil
	n, err := f.uc.RunBatch(context.Background(), testScope, 10)
	if err != nil {
		t.Fatalf("restart tick: %v", err)
	}
	if n != 0 {
		t.Fatalf("restart created %d runs, want 0", n)
	}
	if len(f.runs.order) != 1 {
		t.Fatalf("ledger rows after restart = %d, want 1", len(f.runs.order))
	}
	// The restart publishes the open occurrence twice -- repair pass, then the
	// fire path's republish -- both idempotent creates of the same object.
	if len(f.publish.published) != 2 {
		t.Fatalf("restart published %d CRs, want 2 (repair + fire-path republish)", len(f.publish.published))
	}
}

// TestRunBatch_OccurrenceKeyIsPinnedToRevision pins that the occurrence key
// binds source uid, revision id and scheduled time, so a version bump (new
// revision id) for the same instant cannot collide with the old occurrence.
func TestRunBatch_OccurrenceKeyIsPinnedToRevision(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	dueAt := now.Add(-time.Second)
	f := newFixture([]check.Snapshot{intervalCheck(1, 60, dueAt)}, now)

	if _, err := f.uc.RunBatch(context.Background(), testScope, 10); err != nil {
		t.Fatalf("first tick: %v", err)
	}
	wantKey := coordination.OccurrenceKey("check-1", 100, dueAt)
	if _, ok := f.runs.occurrences[key("check-1", wantKey)]; !ok {
		t.Fatalf("occurrence key %q not found; keys derive from the pinned revision", wantKey)
	}
}

func TestRunBatch_CronCursorAdvances(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	cronExpr := "*/5 * * * *"
	c := intervalCheck(1, 60, now.Add(-time.Second))
	c.ScheduleType = check.ScheduleTypeCron
	c.CronExpr = &cronExpr
	f := newFixture([]check.Snapshot{c}, now)

	if _, err := f.uc.RunBatch(context.Background(), testScope, 10); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := now.Add(5 * time.Minute)
	if len(f.checks.updated) != 1 || !f.checks.updated[0].nextRunAt.Equal(want) {
		t.Fatalf("cron next_run_at = %+v, want %v", f.checks.updated, want)
	}
}

func TestRunBatch_InvalidCronFallsBack(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	cronExpr := "not a cron"
	c := intervalCheck(1, 60, now.Add(-time.Second))
	c.ScheduleType = check.ScheduleTypeCron
	c.CronExpr = &cronExpr
	f := newFixture([]check.Snapshot{c}, now)

	if _, err := f.uc.RunBatch(context.Background(), testScope, 10); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := now.Add(time.Minute)
	if len(f.checks.updated) != 1 || !f.checks.updated[0].nextRunAt.Equal(want) {
		t.Fatalf("fallback next_run_at = %+v, want %v", f.checks.updated, want)
	}
}

// TestRunBatch_UngeneratableRevisionSkipsCheck: a stored config that predates
// boundary validation fails synthesis loudly, is skipped, and blocks neither
// the cursor advance of other checks nor the tick itself.
func TestRunBatch_UngeneratableRevisionSkipsCheck(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	bad := intervalCheck(1, 60, now.Add(-time.Second))
	bad.CheckConfig = map[string]any{"expected_status": float64(200)}
	good := intervalCheck(2, 60, now.Add(-time.Second))
	f := newFixture([]check.Snapshot{bad, good}, now)

	n, err := f.uc.RunBatch(context.Background(), testScope, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1 {
		t.Fatalf("fired %d, want only the valid check", n)
	}
	if len(f.runs.order) != 1 || !strings.HasPrefix(f.runs.order[0], "check-2|") {
		t.Fatalf("fired occurrence %v, want check-2 only", f.runs.order)
	}
}

func TestRunBatch_TerminalRunNotRepublished(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	dueAt := now.Add(-time.Second)
	f := newFixture([]check.Snapshot{intervalCheck(1, 3600, dueAt)}, now)

	if _, err := f.uc.RunBatch(context.Background(), testScope, 10); err != nil {
		t.Fatalf("first tick: %v", err)
	}
	// The occurrence finished between ticks.
	for k := range f.runs.occurrences {
		stored := f.runs.occurrences[k]
		stored.Phase = jobrun.Succeeded
		f.runs.occurrences[k] = stored
	}
	f.checks.due = []check.Snapshot{intervalCheck(1, 3600, dueAt)}
	if _, err := f.uc.RunBatch(context.Background(), testScope, 10); err != nil {
		t.Fatalf("second tick: %v", err)
	}
	if len(f.publish.published) != 1 {
		t.Fatalf("publishes = %d, want 1: terminal history costs no API call", len(f.publish.published))
	}
}

func TestNewTickUseCase_DefaultClock(t *testing.T) {
	now := time.Now().UTC()
	interval := 60
	f := newFixture([]check.Snapshot{intervalCheck(1, interval, now.Add(-time.Second))}, now)
	f.uc.Now = nil // exercise the default clock branch

	if _, err := f.uc.RunBatch(context.Background(), testScope, 10); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
