package main

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
)

func faultCheckID(f FaultSpec) string { return fmt.Sprintf("fault-recovery:%s@%s", f.Name, f.Offset) }

func collectFaultRecoveryChecks(ctx context.Context, runDir string, cfg Config, stats RunStats, dsn string, journal JournalSnapshot) map[string]CheckResult {
	checks := map[string]CheckResult{}
	m, err := loadRecoveryManifest(filepath.Join(runDir, "generated"), cfg)
	var prepared PreparedRecovery
	if err == nil {
		prepared, err = loadPreparedRecovery(runDir, m)
	}
	var db *sql.DB
	if err == nil {
		db, err = sql.Open("postgres", dsn)
	}
	if db != nil {
		defer func() { _ = db.Close() }()
	}
	for _, f := range activeFaultPlan(cfg) {
		check := CheckResult{ID: faultCheckID(f), Status: VerdictInconclusive}
		checks[check.ID] = check
		if err != nil {
			check.Message = "recovery preparation unavailable: " + err.Error()
			checks[check.ID] = check
			continue
		}
		var records []FaultRecord
		for _, r := range stats.Faults {
			if r.Name == f.Name && r.ScheduledOffset == f.Offset {
				records = append(records, r)
			}
		}
		if len(records) != 1 || records[0].Recovery == nil {
			check.Message = "structured recovery observations missing"
			checks[check.ID] = check
			continue
		}
		r := records[0].Recovery
		if r.ManifestSHA256 != recoveryDigest(m) {
			check.Message = "recovery observations do not match generated canaries"
			checks[check.ID] = check
			continue
		}
		if stats.StartedAt.IsZero() || !stats.FinishedAt.After(stats.StartedAt) {
			check.Message = "execution observation window missing"
			checks[check.ID] = check
			continue
		}
		if continuityErr := verifyJournalContinuity(stats.JournalStart, journal); continuityErr != nil {
			check.Message = continuityErr.Error()
			checks[check.ID] = check
			continue
		}
		if !targetReplaced(r.Before, r.After) || verifyJournalContinuity(&r.JournalStart, journal) != nil || r.JournalStart.Head < stats.JournalStart.Head || r.JournalEnd.BootID != journal.BootID || r.JournalEnd.RunID != journal.RunID || r.JournalEnd.Head < r.JournalStart.Head || r.JournalEnd.Head > journal.Head {
			check.Message = "fault replacement or continuous journal interval was not observed"
			checks[check.ID] = check
			continue
		}
		currentTarget, targetErr := readRecoveryTarget(ctx, f)
		if targetErr != nil || currentTarget.UID != r.After.UID || currentTarget.Generation < r.After.Generation || !targetReplaced(r.Before, currentTarget) {
			check.Message = "live fault target does not corroborate the recorded replacement"
			checks[check.ID] = check
			continue
		}
		if f.Name == "postgres" && (!r.DatabaseUnavailable || r.DatabaseDownTarget == nil || r.DatabaseDownTarget.UID != r.Before.UID || r.DatabaseDownTarget.Desired != 0 || r.DatabaseDownTarget.Ready != 0 || len(r.DatabaseDownTarget.PodUIDs) != 0) {
			check.Message = "database outage was not independently observed"
			checks[check.ID] = check
			continue
		}
		gate := recoveryGate(f.Name)
		if gate.BusinessTimeout <= 0 {
			check.Message = "component business probe unsupported"
			checks[check.ID] = check
			continue
		}
		var expected []int
		for i, c := range m.Canaries {
			if c.Fault == f {
				expected = append(expected, i)
			}
		}
		if len(expected) == 0 || len(r.Canaries) != len(expected) {
			check.Message = "planned recovery canaries incomplete"
			checks[check.ID] = check
			continue
		}
		check.Status = VerdictPass
		for _, i := range expected {
			c, b := m.Canaries[i], prepared.Canaries[i]
			var receipts []CanaryReceipt
			for _, x := range r.Canaries {
				if x.CaseID == c.Definition.CaseID {
					receipts = append(receipts, x)
				}
			}
			if len(receipts) != 1 {
				check.Status = VerdictInconclusive
				check.Message = "canary receipts missing or duplicated"
				break
			}
			x := receipts[0]
			fresh, sourceErr := collectRecoveryRun(ctx, db, c, b, x.Trigger)
			if sourceErr != nil {
				check.Status = VerdictInconclusive
				check.Message = "re-read canary source: " + sourceErr.Error()
				break
			}
			budget := (*RecoveryBudget)(nil)
			if f.Name == "scheduler" {
				value, budgetErr := collectRecoveryBudget(ctx, db, b, fresh.ID)
				if budgetErr != nil {
					check.Status = VerdictInconclusive
					check.Message = "re-read scheduler SLO: " + budgetErr.Error()
					break
				}
				budget = &value
			}
			status, message := evaluateRecoveryCanary(c, b, x, fresh, budget, journal, r.JournalStart)
			if status == VerdictPass {
				if timelineErr := recoveryTimelineCheck(journal, m, records[0], c, x, fresh, stats.StartedAt, stats.FinishedAt); timelineErr != nil {
					status, message = VerdictInconclusive, timelineErr.Error()
				}
			}
			if c.Stage == "pending" {
				completed := false
				for _, uid := range r.DatabaseDownCompletedJobs {
					if uid != "" && uid == fresh.OwnedJobUID {
						completed = true
					}
				}
				if !completed {
					status, message = VerdictInconclusive, "original Job completion during the database outage was not observed"
				}
			}
			if status == VerdictInconclusive && r.TimedOut && !r.Interrupted && !x.SubmittedAfter.IsZero() && x.Trigger.OccurrenceKey != "" && (fresh.ID == 0 || fresh.Phase == "Running" || fresh.Phase == "Pending") {
				status, message = VerdictFail, "canary did not recover within the complete observation window"
			}
			if status != VerdictPass {
				check.Status = status
				check.Message = message
				break
			}
		}
		if check.Status == VerdictPass {
			switch {
			case r.Interrupted:
				check.Status = VerdictInconclusive
				check.Message = "business observation interrupted"
			case r.TimedOut:
				check.Status = VerdictFail
				check.Message = "business recovery exceeded its wall-clock deadline"
			case r.Error != "":
				check.Status = VerdictInconclusive
				check.Message = "business observation failed: " + r.Error
			case r.BusinessElapsedSeconds <= 0 || r.BusinessElapsedSeconds > (gate.ReadyTimeout+gate.BusinessTimeout).Seconds()+1:
				check.Status = VerdictInconclusive
				check.Message = "bounded business observation window missing"
			default:
				check.Message = "manifest canaries re-read from ledger, owned Jobs and continuous fixture journal"
				check.Evidence = []string{"generated/recovery.json", "recovery-prepared.json", "run-stats.json#/faults"}
			}
		}
		checks[check.ID] = check
	}
	return checks
}

func evaluateRecoveryCanary(c RecoveryCanary, b PreparedCanary, x CanaryReceipt, current RecoveryRun, budget *RecoveryBudget, journal JournalSnapshot, start JournalCursor) (Verdict, string) {
	count := recoveryEffectCount(journal, c, start)
	if count > 1 {
		return VerdictFail, "canary produced duplicate side effects"
	}
	if current.Phase == phaseFailed || current.Phase == phaseCanceled || x.Observed.Phase == phaseFailed || x.Observed.Phase == phaseCanceled {
		return VerdictFail, "canary reached an unexpected terminal state"
	}
	if len(current.Attempts) > 1 || len(x.Observed.Attempts) > 1 {
		return VerdictFail, "single-attempt recovery canary was executed more than once"
	}
	if x.SubmittedAfter.IsZero() || x.Observed.CreatedAt.Before(x.SubmittedAfter) || !canaryRunRecovered(x.Observed, b, x.Trigger) || !canaryRunRecovered(current, b, x.Trigger) || x.Observed.ID != current.ID || x.Observed.OwnedJobUID != current.OwnedJobUID || !x.Observed.CreatedAt.Equal(current.CreatedAt) || count != 1 {
		return VerdictInconclusive, "canary lacks matching bounded terminal observation, original Job or independent effect"
	}
	if c.Stage == "pending" {
		before := x.BeforeFault
		if before == nil || before.ID != current.ID || before.Phase != "Running" || len(before.Attempts) != 1 || before.OwnedJobUID != current.OwnedJobUID || before.JobSucceeded || before.Attempts[0].OwnerUID != current.CRUID {
			return VerdictInconclusive, "original in-flight canary identity was not preserved across the outage"
		}
	}
	if c.Fault.Name == "scheduler" {
		if x.Budget == nil || budget == nil || x.Budget.SourceUID != b.SourceUID || budget.SourceUID != b.SourceUID || x.Budget.SLOID != b.SLOID || budget.SLOID != b.SLOID || x.Budget.SLIID != b.SLIID || budget.SLIID != b.SLIID || !budgetRecovered(*x.Budget, x.SubmittedAfter) || !budgetRecovered(*budget, x.SubmittedAfter) || !x.Budget.EventAt.Equal(budget.EventAt) {
			return VerdictInconclusive, "scheduler has no fresh matching evaluated SLO result for the canary event"
		}
	}
	return VerdictPass, "canary recovered"
}
