// Package controlplane turns active definition revisions into job runs. It is
// the only component allowed to create a run, so occurrence deduplication and
// concurrency policy live here rather than in the operator.
package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/coordination"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
)

// Label and annotation keys the control plane stamps on published runs.
const (
	LabelTenant           = "orbitjob.io/tenant"
	LabelScheduledJob     = "orbitjob.io/scheduled-job-uid"
	AnnotationScheduledAt = "orbitjob.io/scheduled-at"
)

// ActorScheduler names the scheduler in the ledger when no identity was
// configured for it. A scheduled occurrence has no person behind it, and
// job_run_control_plane.actor is NOT NULL with a non-empty CHECK, so the writer
// must state who asked. A blank actor would defeat the column's whole purpose,
// which is to answer "who triggered this"; a role name is the honest answer for
// work a cron expression triggered.
const ActorScheduler = "orbitjob-scheduler"

// These hard bounds keep an accidental caller configuration from turning one
// definition into an unbounded minute scan or catch-up admission burst.
const MaxScheduleLookback = 24 * time.Hour
const MaxCatchUpOccurrences = 100

// RevisionSource lists the definitions a tenant may schedule. The run recorder
// applies the source's concurrency policy atomically with admission.
type RevisionSource interface {
	ActiveRevisions(ctx context.Context, tenantID string) ([]revision.Revision, error)
}

// RunRecorder stores a run, records who asked for it, and reports whether this
// call created it.
type RunRecorder interface {
	CreateOccurrenceForTenant(ctx context.Context, tenantID string, run jobrun.JobRun, maxAttempts int, actor string) (jobrun.StoredRun, bool, error)
}

// OpenRunLister reports runs that have not finished. The scheduler repairs their
// Kubernetes representation before applying concurrency policy.
type OpenRunLister interface {
	OpenRuns(ctx context.Context, tenantID, sourceUID string) ([]jobrun.OpenRun, error)
}

// RunPublisher exposes a created run to Kubernetes. It must be idempotent: the
// run row already exists, so a failure here is repaired by re-publishing.
type RunPublisher interface {
	Publish(ctx context.Context, spec v1alpha1.JobRun) error
}

// Scheduler fires due definitions. One instance per process is expected; if
// several run, occurrence deduplication in the database keeps them from creating
// duplicate runs, and the losers converge on the winner's run.
type Scheduler struct {
	Revisions RevisionSource
	Runs      RunRecorder
	Publisher RunPublisher
	// Tenants are the tenants this instance schedules for.
	Tenants []string
	// Actor is the identity recorded against every run this scheduler creates.
	// A scheduled occurrence has no human behind it; leaving it empty falls back
	// to ActorScheduler rather than writing a blank actor the schema rejects.
	Actor string
	// MisfireGrace bounds the lookback window. An occurrence older than this is
	// a misfire and is handled by the definition's misfire policy.
	MisfireGrace time.Duration
	// Now defaults to time.Now.
	Now func() time.Time
	// ParseSchedule overrides the cron parser; tests use it to pin behaviour.
	ParseSchedule func(string) (Schedule, error)
	// MaxCatchUp bounds how many missed occurrences a CatchUpBounded definition
	// may fire in one tick.
	MaxCatchUp int
	// SourceExists checks current definition liveness before creating new work.
	SourceExists func(context.Context, revision.Revision) (bool, error)
}

// Schedule reports whether an instant is an activation of a cron expression.
type Schedule interface {
	Matches(time.Time) bool
}

// cronSchedule adapts robfig/cron to Schedule. The library only offers Next, so
// an instant matches when it is its own predecessor's successor.
type cronSchedule struct{ inner cron.Schedule }

func (c cronSchedule) Matches(t time.Time) bool {
	return c.inner.Next(t.Add(-time.Second)).Equal(t)
}

// Tick fires every definition that is due at the current instant and returns how
// many runs it created. A definition that fails is logged and skipped, so one bad
// revision cannot stop every other tenant's scheduling; only a failure to list a
// tenant's revisions aborts the tick.
func (s Scheduler) Tick(ctx context.Context) (int, error) {
	if s.Revisions == nil || s.Runs == nil || s.Publisher == nil {
		return 0, fmt.Errorf("scheduler dependencies are required")
	}
	now := s.now()
	grace := s.MisfireGrace
	if grace <= 0 {
		grace = time.Hour
	}

	created := 0
	for _, tenant := range s.Tenants {
		revisions, err := s.Revisions.ActiveRevisions(ctx, tenant)
		if err != nil {
			return created, fmt.Errorf("list active revisions for tenant %s: %w", tenant, err)
		}
		for _, rev := range revisions {
			count, err := s.fireDefinition(ctx, tenant, rev, now, grace)
			if err != nil {
				slog.Error("scheduling definition failed; skipping it",
					"tenant", tenant,
					"definition", rev.Identity.Name,
					"revision_id", rev.ID,
					"error", err,
				)
				continue
			}
			created += count
		}
	}
	return created, nil
}

// repairOpenRuns re-publishes the Kubernetes object for every unfinished run of
// a definition. Publish is idempotent, so a run that already has its object
// costs one API call and changes nothing.
func (s Scheduler) repairOpenRuns(ctx context.Context, tenant string, rev revision.Revision) error {
	lister, ok := s.Runs.(OpenRunLister)
	if !ok {
		return nil
	}
	open, err := lister.OpenRuns(ctx, tenant, rev.Identity.SourceUID)
	if err != nil {
		return fmt.Errorf("list open runs for %s: %w", rev.Identity.Name, err)
	}
	for _, run := range open {
		pinned := rev
		if reader, ok := s.Revisions.(interface {
			RevisionByID(context.Context, string, int64) (revision.Revision, error)
		}); ok {
			pinned, err = reader.RevisionByID(ctx, tenant, run.RevisionID)
			if err != nil {
				return err
			}
			if pinned.Identity.SourceUID != rev.Identity.SourceUID {
				return fmt.Errorf("repair revision source differs from recorded run")
			}
		} else if run.RevisionID != rev.ID {
			return fmt.Errorf("pinned revision reader is required for repair")
		}
		var spec v1alpha1.ScheduledJobSpec
		if err = json.Unmarshal([]byte(pinned.NormalizedSpec), &spec); err != nil {
			return fmt.Errorf("decode pinned repair revision %d: %w", pinned.ID, err)
		}
		obj := s.jobRunObject(pinned, spec, jobrun.StoredRun{
			ID: run.ID, RevisionID: run.RevisionID, Phase: run.Phase,
		}, run.OccurrenceKey, run.ScheduledFor)
		if run.Trigger != "" {
			obj.Spec.Trigger = v1alpha1.Trigger(run.Trigger)
		}
		if run.Actor != "" {
			obj.Spec.Actor = run.Actor
		}
		if pinned.Identity.SourceMode != "kubernetes" {
			obj.OwnerReferences = nil
		}
		obj.Spec.CancelRequested = run.Phase == jobrun.CancelRequested || run.Phase == jobrun.CancelUnknown
		if err := s.Publisher.Publish(ctx, obj); err != nil {
			return fmt.Errorf("repair run %d for %s: %w", run.ID, rev.Identity.Name, err)
		}
	}
	return nil
}

func (s Scheduler) fireDefinition(ctx context.Context, tenant string, rev revision.Revision, now time.Time, grace time.Duration) (int, error) {
	if s.SourceExists != nil {
		alive, err := s.SourceExists(ctx, rev)
		if err != nil {
			return 0, err
		}
		if !alive {
			return 0, nil
		}
	}

	var spec v1alpha1.ScheduledJobSpec
	if err := json.Unmarshal([]byte(rev.NormalizedSpec), &spec); err != nil {
		return 0, fmt.Errorf("decode revision %d for %s: %w", rev.ID, rev.Identity.Name, err)
	}
	if err := s.repairOpenRuns(ctx, tenant, rev); err != nil {
		return 0, err
	}
	if spec.Suspend || spec.Schedule == "" {
		return 0, nil
	}

	schedule, err := s.parse(spec.Schedule)
	if err != nil {
		return 0, fmt.Errorf("definition %s has an invalid schedule: %w", rev.Identity.Name, err)
	}

	// Repair before policy. A run whose object was never created sits in a
	// non-terminal phase forever, and under Forbid that run blocks every future
	// occurrence of its own definition -- including the reconcile that would
	// have repaired it. Publishing first breaks that cycle.
	occurrences := selectOccurrences(schedule, spec.MisfirePolicy, now, grace, s.MaxCatchUp)
	if len(occurrences) == 0 {
		return 0, nil
	}

	created := 0
	for _, scheduledAt := range occurrences {
		if !rev.CreatedAt.IsZero() && scheduledAt.Before(rev.CreatedAt) {
			continue
		}
		occurrenceKey := coordination.OccurrenceKey(rev.Identity.SourceUID, rev.ID, scheduledAt)
		if reader, ok := s.Runs.(interface {
			RunByOccurrence(context.Context, string, string, string) (jobrun.StoredRun, bool, error)
		}); ok {
			_, found, err := reader.RunByOccurrence(ctx, tenant, rev.Identity.SourceUID, occurrenceKey)
			if err != nil {
				return created, err
			}
			if found {
				continue
			}
		}
		if s.SourceExists != nil {
			alive, err := s.SourceExists(ctx, rev)
			if err != nil {
				return created, err
			}
			if !alive {
				return created, nil
			}
		}
		// RevisionID is the opaque stored identity; the run pins exactly the
		// revision the scheduler read, so editing the definition mid-tick
		// cannot change what an already-decided occurrence executes.
		state, isNew, err := s.Runs.CreateOccurrenceForTenant(ctx, tenant, jobrun.JobRun{
			SourceUID:     rev.Identity.SourceUID,
			RevisionID:    fmt.Sprint(rev.ID),
			OccurrenceKey: occurrenceKey,
			Trigger:       jobrun.Schedule,
			Phase:         jobrun.Pending,
			ScheduledFor:  scheduledAt,
		}, spec.RetryPolicy.EffectiveMaxAttempts(), s.actor())
		var blocked *jobrun.AdmissionBlockedError
		if errors.As(err, &blocked) {
			if blocked.Policy == string(v1alpha1.Replace) {
				if err := s.cancelRuns(ctx, rev, blocked.CancelTargets); err != nil {
					return created, err
				}
			}
			return created, nil
		}
		if err != nil {
			return created, fmt.Errorf("create run for %s: %w", rev.Identity.Name, err)
		}
		// Publish even for an occurrence that already exists. Publish is
		// idempotent, and a run whose CR was lost (the row committed but the
		// create failed, or the controller restarted in between) would otherwise
		// be stranded: deduplication would skip it forever and nothing would
		// ever execute. Terminal runs are skipped so history does not cost an
		// API call every tick.
		if !isNew && jobrun.Terminal(state.Phase) {
			continue
		}
		if err := s.Publisher.Publish(ctx, s.jobRunObject(rev, spec, state, occurrenceKey, scheduledAt)); err != nil {
			return created, fmt.Errorf("publish run for %s: %w", rev.Identity.Name, err)
		}
		if isNew {
			created++
			if spec.ConcurrencyPolicy == v1alpha1.Forbid || spec.ConcurrencyPolicy == v1alpha1.Replace {
				break
			}
		}
	}
	return created, nil
}

// jobRunObject renders the JobRun CR for a stored run. The name is derived from
// the occurrence key so a retry after a failed publish adopts the same object
// instead of creating a second run.
func (s Scheduler) jobRunObject(rev revision.Revision, spec v1alpha1.ScheduledJobSpec, state jobrun.StoredRun, occurrenceKey string, scheduledAt time.Time) v1alpha1.JobRun {
	return v1alpha1.JobRun{
		TypeMeta: metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "JobRun"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      runObjectName(rev.Identity.Name, occurrenceKey),
			Namespace: rev.Identity.Namespace,
			Labels: map[string]string{
				LabelTenant:       rev.Identity.Namespace,
				LabelScheduledJob: rev.Identity.SourceUID,
			},
			Annotations: scheduleAnnotation(scheduledAt),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: v1alpha1.GroupVersion.String(),
				Kind:       "ScheduledJob",
				Name:       rev.Identity.Name,
				UID:        types.UID(rev.Identity.SourceUID),
				// Deleting the definition garbage-collects its runs rather than
				// leaving orphans that no reconciler owns.
				Controller:         ptr(true),
				BlockOwnerDeletion: ptr(false),
			}},
		},
		Spec: v1alpha1.JobRunSpec{
			ScheduledJobRef: v1alpha1.ObjectReference{
				Name: rev.Identity.Name,
				UID:  rev.Identity.SourceUID,
			},
			DefinitionRevision: state.RevisionID,
			Trigger:            v1alpha1.Schedule,
			// The CRD requires an actor. The run row already records who asked,
			// and repair re-publishes a CR whose actor is not read back, so this
			// value names the scheduler rather than reconstructing the original
			// triggerer; the ledger row stays the authority.
			Actor:         s.actor(),
			OccurrenceKey: occurrenceKey,
			// The deadline is pinned on the run, not read live from the
			// definition, so editing the definition cannot extend a run already
			// in flight.
			TimeoutSeconds: spec.TimeoutSeconds,
		},
	}
}

func ptr[T any](v T) *T { return &v }

// runObjectName delegates to the shared JobRun naming rule. The derivation
// lives in one place so the scheduler, the admin API and a cancel request all
// address the same object for one occurrence.
func runObjectName(scheduledJobName, occurrenceKey string) string {
	return v1alpha1.RunObjectName(scheduledJobName, occurrenceKey)
}

// scheduleAnnotation records the occurrence time. Repair passes a zero time,
// which means "unknown", and an absent annotation is better than a wrong one.
func scheduleAnnotation(at time.Time) map[string]string {
	if at.IsZero() {
		return nil
	}
	return map[string]string{AnnotationScheduledAt: at.UTC().Format(time.RFC3339)}
}

// selectOccurrences decides which schedule activations to fire.
//
// Inside the window the newest activation is either on time (it happened in the
// current minute) or a misfire. On-time activations always fire. A misfire is
// decided by policy: Skip drops it, FireOnce fires only the newest, and
// CatchUpBounded fires up to maxCatchUp of the newest. The window is bounded by
// grace, so a definition that was idle far longer than its grace simply has
// nothing due rather than replaying an unbounded backlog.
func selectOccurrences(schedule Schedule, policy v1alpha1.MisfirePolicy, now time.Time, grace time.Duration, maxCatchUp int) []time.Time {
	if grace <= 0 {
		grace = time.Hour
	}
	if grace > MaxScheduleLookback {
		grace = MaxScheduleLookback
	}
	if maxCatchUp > MaxCatchUpOccurrences {
		maxCatchUp = MaxCatchUpOccurrences
	}
	matches := matchesInWindow(schedule, now, grace)
	if len(matches) == 0 {
		return nil
	}
	newest := matches[len(matches)-1:]
	if now.Sub(matches[len(matches)-1]) < time.Minute {
		return newest
	}
	switch policy {
	case v1alpha1.Skip:
		return nil
	case v1alpha1.CatchUpBounded:
		if maxCatchUp < 1 {
			maxCatchUp = 1
		}
		if len(matches) > maxCatchUp {
			return matches[len(matches)-maxCatchUp:]
		}
		return matches
	default:
		// FireOnce, and the unset default: a scheduler that just came up runs
		// the job once rather than replaying history.
		return newest
	}
}

// matchesInWindow walks the lookback window minute by minute. The window keeps
// the decision a pure function of (schedule, now) with no stored cursor to drift.
func matchesInWindow(schedule Schedule, now time.Time, grace time.Duration) []time.Time {
	start := now.Add(-grace).UTC().Truncate(time.Minute)
	end := now.UTC().Truncate(time.Minute)
	var matches []time.Time
	for t := start; !t.After(end); t = t.Add(time.Minute) {
		if schedule.Matches(t) {
			matches = append(matches, t)
		}
	}
	return matches
}

func (s Scheduler) parse(expression string) (Schedule, error) {
	if s.ParseSchedule != nil {
		return s.ParseSchedule(expression)
	}
	parsed, err := cron.ParseStandard(expression)
	if err != nil {
		return nil, err
	}
	return cronSchedule{inner: parsed}, nil
}

func (s Scheduler) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// actor is the identity to record against a run this scheduler creates. A
// configured identity wins; otherwise the scheduler names itself, because an
// empty actor is refused by the CRD, by the store and by the column's CHECK.
func (s Scheduler) actor() string {
	if v := strings.TrimSpace(s.Actor); v != "" {
		return v
	}
	return ActorScheduler
}

func (s Scheduler) cancelRuns(ctx context.Context, rev revision.Revision, runs []jobrun.OpenRun) error {
	canceler, ok := s.Publisher.(interface {
		RequestCancel(context.Context, string, string, ...v1alpha1.RunIdentity) error
	})
	if !ok {
		return fmt.Errorf("run canceler required for Replace")
	}
	for _, run := range runs {
		trigger := v1alpha1.Trigger(run.Trigger)
		if trigger == "" {
			trigger = v1alpha1.Schedule
		}
		if err := canceler.RequestCancel(ctx, rev.Identity.Namespace, rev.Identity.Name, v1alpha1.RunIdentity{SourceUID: rev.Identity.SourceUID, OccurrenceKey: run.OccurrenceKey, RevisionID: run.RevisionID, Trigger: trigger}); err != nil {
			return err
		}
	}
	return nil
}
