package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

type CanaryReceipt struct {
	CaseID         string          `json:"case_id"`
	Trigger        TriggerResponse `json:"trigger"`
	SubmittedAfter time.Time       `json:"submitted_after"`
	BeforeFault    *RecoveryRun    `json:"before_fault,omitempty"`
	Observed       RecoveryRun     `json:"observed"`
	Budget         *RecoveryBudget `json:"budget,omitempty"`
}

type FaultRecoveryReceipt struct {
	ManifestSHA256            string          `json:"manifest_sha256"`
	Before                    RecoveryTarget  `json:"before"`
	After                     RecoveryTarget  `json:"after"`
	JournalStart              JournalCursor   `json:"journal_start"`
	JournalEnd                JournalCursor   `json:"journal_end"`
	DatabaseUnavailable       bool            `json:"database_unavailable"`
	DatabaseDownTarget        *RecoveryTarget `json:"database_down_target,omitempty"`
	DatabaseDownCompletedJobs []string        `json:"database_down_completed_jobs,omitempty"`
	Canaries                  []CanaryReceipt `json:"canaries"`
	Error                     string          `json:"error,omitempty"`
	Interrupted               bool            `json:"interrupted,omitempty"`
	TimedOut                  bool            `json:"timed_out,omitempty"`
	BusinessElapsedSeconds    float64         `json:"business_elapsed_seconds"`
}

type RecoveryProber struct {
	Manifest       RecoveryManifest
	Prepared       PreparedRecovery
	DB             *sql.DB
	Clients        map[string]*APIClient
	FixtureURL     string
	SetupError     error
	monotonicStart time.Time
}

func newRecoveryProber(runDir string, cfg Config, dsn, fixture string, clients map[string]*APIClient) *RecoveryProber {
	p := &RecoveryProber{Clients: clients, FixtureURL: fixture}
	p.Manifest, p.SetupError = loadRecoveryManifest(filepath.Join(runDir, "generated"), cfg)
	if p.SetupError == nil {
		p.Prepared, p.SetupError = loadPreparedRecovery(runDir, p.Manifest)
	}
	if p.SetupError == nil && len(p.Manifest.Canaries) > 0 {
		if dsn == "" {
			p.SetupError = fmt.Errorf("recovery requires an independently readable OrbitJob DSN")
		} else {
			p.DB, p.SetupError = sql.Open("postgres", dsn)
		}
	}
	return p
}

func recoveryGate(name string) RecoveryGate {
	for _, gate := range StandardRecoveryGates() {
		if gate.Name == name {
			return gate
		}
	}
	return RecoveryGate{Name: name, ReadyTimeout: 2 * time.Minute}
}

func (p *RecoveryProber) canaries(f FaultSpec) []int {
	var indices []int
	for i, c := range p.Manifest.Canaries {
		if c.Fault == f {
			indices = append(indices, i)
		}
	}
	return indices
}

func (p *RecoveryProber) before(ctx context.Context, f FaultSpec) *FaultRecoveryReceipt {
	r := &FaultRecoveryReceipt{ManifestSHA256: recoveryDigest(p.Manifest)}
	if p.SetupError != nil {
		r.Error = p.SetupError.Error()
		return r
	}
	if len(p.canaries(f)) == 0 {
		r.Error = "planned fault has no prepared business probe"
		return r
	}
	var err error
	r.Before, err = readRecoveryTarget(ctx, f)
	if err == nil {
		var journal JournalSnapshot
		journal, err = readJournalSnapshot(ctx, p.FixtureURL, p.Manifest.RunID)
		r.JournalStart = journal.Cursor()
	}
	if err != nil {
		r.Error = err.Error()
		r.Interrupted = ctx.Err() != nil
		return r
	}
	if f.Name == "postgres" {
		for _, i := range p.canaries(f) {
			if p.Manifest.Canaries[i].Stage == "pending" {
				x, err := p.submit(ctx, i)
				if err == nil {
					err = waitRecoveryObservation(ctx, 30*time.Second, func(ctx context.Context) (bool, error) {
						var observed RecoveryRun
						observed, err = collectRecoveryRun(ctx, p.DB, p.Manifest.Canaries[i], p.Prepared.Canaries[i], x.Trigger)
						if err != nil {
							return false, err
						}
						journal, err := readJournalSnapshot(ctx, p.FixtureURL, p.Manifest.RunID)
						if err != nil {
							return false, err
						}
						if err = verifyJournalContinuity(&r.JournalStart, journal); err != nil {
							return false, err
						}
						if observed.Phase == "Running" && len(observed.Attempts) == 1 && observed.OwnedJobUID != "" && !observed.JobSucceeded && recoveryEffectCount(journal, p.Manifest.Canaries[i], r.JournalStart) == 1 {
							x.BeforeFault = &observed
							return true, nil
						}
						return false, nil
					})
				}
				r.Canaries = append(r.Canaries, x)
				if err != nil {
					r.Error = "prepare in-flight canary: " + err.Error()
					r.Interrupted = ctx.Err() != nil
					return r
				}
				if _, err = p.checkpoint(ctx, i, "in-flight", nil, x.BeforeFault, nil); err != nil {
					r.Error = err.Error()
					return r
				}
			}
		}
	}
	if _, err = p.checkpoint(ctx, p.freshIndex(f), "fault", &r.Before, nil, nil); err != nil {
		r.Error = err.Error()
		r.Interrupted = ctx.Err() != nil
	}
	return r
}

func (p *RecoveryProber) submit(ctx context.Context, i int) (CanaryReceipt, error) {
	c, b := p.Manifest.Canaries[i], p.Prepared.Canaries[i]
	x := CanaryReceipt{CaseID: c.Definition.CaseID}
	if p.Clients[b.Tenant] == nil {
		return x, fmt.Errorf("canary tenant client unavailable")
	}
	checkpoint, err := p.checkpoint(ctx, i, "submit", nil, nil, nil)
	if err != nil {
		return x, err
	}
	x.SubmittedAfter = checkpoint.DatabaseTime
	x.Trigger, err = p.Clients[b.Tenant].TriggerJob(ctx, b.RevisionID, c.Nonce)
	return x, err
}

func (p *RecoveryProber) release(ctx context.Context, caseID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.FixtureURL+"/recovery/release", bytes.NewReader(mustJSON(map[string]string{"case_id": caseID})))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != 200 {
		return fmt.Errorf("release recovery hold: HTTP %d", response.StatusCode)
	}
	return nil
}

func (p *RecoveryProber) during(ctx context.Context, f FaultSpec, r *FaultRecoveryReceipt) {
	if f.Name != "postgres" || r.Error != "" {
		return
	}
	err := waitRecoveryObservation(ctx, 20*time.Second, func(ctx context.Context) (bool, error) {
		target, err := readRecoveryTarget(ctx, f)
		if err != nil {
			return false, err
		}
		if target.Desired != 0 || target.Ready != 0 || len(target.PodUIDs) != 0 {
			return false, nil
		}
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := p.DB.PingContext(probeCtx); err == nil {
			return false, nil
		}
		r.DatabaseUnavailable = true
		r.DatabaseDownTarget = &target
		return true, nil
	})
	if err == nil {
		_, err = p.checkpoint(ctx, p.freshIndex(f), "down", r.DatabaseDownTarget, nil, nil)
	}
	if err == nil {
		for _, x := range r.Canaries {
			if x.BeforeFault == nil {
				continue
			}
			if err = p.release(ctx, x.CaseID); err != nil {
				break
			}
			a := x.BeforeFault.Attempts[0]
			err = waitRecoveryObservation(ctx, 20*time.Second, func(ctx context.Context) (bool, error) {
				var job batchv1.Job
				if err := readRecoveryObject(ctx, "job", x.Trigger.Namespace, a.JobName, &job); err != nil {
					return false, err
				}
				if string(job.UID) != a.JobUID {
					return false, fmt.Errorf("in-flight Job replaced during database outage")
				}
				for _, c := range job.Status.Conditions {
					if c.Type == batchv1.JobComplete && c.Status == corev1.ConditionTrue {
						return true, nil
					}
				}
				return false, nil
			})
			if err == nil {
				r.DatabaseDownCompletedJobs = append(r.DatabaseDownCompletedJobs, a.JobUID)
				for _, i := range p.canaries(f) {
					if p.Manifest.Canaries[i].Definition.CaseID == x.CaseID {
						native := *x.BeforeFault
						native.JobSucceeded = true
						_, err = p.checkpoint(ctx, i, "native-complete", nil, &native, nil)
					}
				}
			}
			if err != nil {
				break
			}
		}
	}
	if err != nil {
		r.Error = "database outage observation: " + err.Error()
		r.Interrupted = ctx.Err() != nil
	}
}

func (p *RecoveryProber) cleanup(r *FaultRecoveryReceipt) {
	// Release a held fixture request even if injection, observation or user
	// cancellation failed. This deadline is separate from the canceled run.
	for _, x := range r.Canaries {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = p.release(ctx, x.CaseID)
		cancel()
	}
}

func (p *RecoveryProber) after(ctx context.Context, f FaultSpec, r *FaultRecoveryReceipt) {
	if r.Error != "" {
		return
	}
	gate := recoveryGate(f.Name)
	if gate.BusinessTimeout <= 0 {
		r.Error = "business probe is unsupported"
		return
	}
	start := time.Now()
	defer func() { r.BusinessElapsedSeconds = time.Since(start).Seconds() }()
	err := waitRecoveryObservation(ctx, gate.ReadyTimeout, func(ctx context.Context) (bool, error) {
		var err error
		r.After, err = readRecoveryTarget(ctx, f)
		return targetReplaced(r.Before, r.After), err
	})
	if err != nil {
		r.Error = "target rollout observation: " + err.Error()
		r.Interrupted = ctx.Err() != nil
		return
	}
	if _, err = p.checkpoint(ctx, p.freshIndex(f), "ready", &r.After, nil, nil); err != nil {
		r.Error = err.Error()
		r.Interrupted = ctx.Err() != nil
		return
	}
	businessCtx, cancel := context.WithTimeout(ctx, gate.BusinessTimeout)
	defer cancel()
	for _, i := range p.canaries(f) {
		c, b := p.Manifest.Canaries[i], p.Prepared.Canaries[i]
		var x *CanaryReceipt
		if c.Stage == "pending" {
			for j := range r.Canaries {
				if r.Canaries[j].CaseID == c.Definition.CaseID {
					x = &r.Canaries[j]
				}
			}
		} else {
			var submitted CanaryReceipt
			submitted, err = p.submit(businessCtx, i)
			r.Canaries = append(r.Canaries, submitted)
			x = &r.Canaries[len(r.Canaries)-1]
			if err == nil && f.Name == "admin-api" {
				var replay TriggerResponse
				replay, err = p.Clients[b.Tenant].TriggerJob(businessCtx, b.RevisionID, c.Nonce)
				if err == nil && (replay.Namespace != x.Trigger.Namespace || replay.Name != x.Trigger.Name || replay.OccurrenceKey != x.Trigger.OccurrenceKey) {
					err = fmt.Errorf("admin replay changed canary identity")
				}
			}
		}
		if err != nil {
			break
		}
		if x == nil {
			err = fmt.Errorf("in-flight canary receipt missing")
			break
		}
		err = waitRecoveryObservation(businessCtx, gate.BusinessTimeout, func(ctx context.Context) (bool, error) {
			var err error
			x.Observed, err = collectRecoveryRun(ctx, p.DB, c, b, x.Trigger)
			if err != nil {
				return false, err
			}
			if x.Observed.Phase == "Failed" || x.Observed.Phase == "Canceled" {
				return false, fmt.Errorf("canary terminated as %s", x.Observed.Phase)
			}
			if !canaryRunRecovered(x.Observed, b, x.Trigger) {
				return false, nil
			}
			if f.Name == "scheduler" {
				budget, err := collectRecoveryBudget(ctx, p.DB, b, x.Observed.ID)
				x.Budget = &budget
				if err != nil {
					return false, err
				}
				if !budgetRecovered(budget, x.SubmittedAfter) {
					return false, nil
				}
			}
			journal, err := readJournalSnapshot(ctx, p.FixtureURL, p.Manifest.RunID)
			if err != nil {
				return false, err
			}
			if err = verifyJournalContinuity(&r.JournalStart, journal); err != nil {
				return false, err
			}
			r.JournalEnd = journal.Cursor()
			return recoveryEffectCount(journal, c, r.JournalStart) == 1, nil
		})
		if err == nil {
			_, err = p.checkpoint(businessCtx, i, "observed", nil, &x.Observed, x.Budget)
		}
		if err != nil {
			break
		}
	}
	if err != nil {
		r.Error = err.Error()
		r.Interrupted = ctx.Err() != nil
		r.TimedOut = !r.Interrupted && (businessCtx.Err() == context.DeadlineExceeded)
	}
	if err == nil {
		journal, sourceErr := readJournalSnapshot(businessCtx, p.FixtureURL, p.Manifest.RunID)
		if sourceErr == nil {
			sourceErr = verifyJournalContinuity(&r.JournalStart, journal)
		}
		if sourceErr != nil {
			r.Error = sourceErr.Error()
			r.Interrupted = ctx.Err() != nil
		} else {
			r.JournalEnd = journal.Cursor()
		}
	}
}

func (p *RecoveryProber) restored(ctx context.Context, f FaultSpec, r *FaultRecoveryReceipt) {
	if r.Error != "" {
		return
	}
	if _, err := p.checkpoint(ctx, p.freshIndex(f), "restored", nil, nil, nil); err != nil {
		r.Error = err.Error()
		r.Interrupted = ctx.Err() != nil
	}
}

func canaryRunRecovered(r RecoveryRun, b PreparedCanary, t TriggerResponse) bool {
	return r.ID > 0 && r.Tenant == b.Tenant && r.SourceUID == b.SourceUID && r.Revision == b.RevisionID && r.Occurrence == t.OccurrenceKey && r.Trigger == "Manual" && r.Phase == phaseSucceeded && len(r.Attempts) == 1 && r.Attempts[0].Number == 1 && r.Attempts[0].Phase == phaseSucceeded && r.Attempts[0].JobUID != "" && r.Attempts[0].JobUID == r.OwnedJobUID && r.CRUID != "" && r.Attempts[0].OwnerUID == r.CRUID && r.JobSucceeded
}

func waitRecoveryObservation(ctx context.Context, budget time.Duration, check func(context.Context) (bool, error)) error {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		ok, err := check(ctx)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if ok {
			return nil
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
