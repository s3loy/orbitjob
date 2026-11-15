package main

import "fmt"

// A report renders the saved verdict; it cannot upgrade contradictory or
// incomplete execution evidence into a fresh PASS by omitting failed reads.
func validateReportPass(result Result, stats RunStats) error {
	if result.Verdict != VerdictPass {
		return nil
	}
	if !stats.Completed {
		return fmt.Errorf("PASS contradicts an incomplete execution")
	}
	checks := executionEvidenceChecks(stats, Evidence{ManualRuns: stats.AcceptedTriggers})
	if checks[0].Status != VerdictPass {
		return fmt.Errorf("PASS contradicts execution stats: %s", checks[0].Message)
	}
	statuses := make(map[string]Verdict)
	for _, check := range result.Checks {
		if check.ID == "" || check.Status != VerdictPass || statuses[check.ID] != "" {
			return fmt.Errorf("PASS has missing, duplicate or non-PASS correctness checks")
		}
		statuses[check.ID] = check.Status
	}
	for _, check := range append(VerifyCorrectness(Evidence{}), checks...) {
		if statuses[check.ID] != VerdictPass {
			return fmt.Errorf("PASS lacks required check %s", check.ID)
		}
	}
	if statuses["journal-continuity"] != VerdictPass || stats.JournalStart == nil {
		return fmt.Errorf("PASS lacks continuous fixture journal evidence")
	}
	if stats.JournalStart.RunID != stats.RunID || stats.JournalStart.BootID == "" || stats.JournalStart.Head < 0 {
		return fmt.Errorf("PASS has invalid journal baseline identity")
	}
	if stats.Qualification && len(stats.FaultPlan) == 0 {
		return fmt.Errorf("qualification PASS lacks its fault plan")
	}
	for _, fault := range stats.FaultPlan {
		if statuses[faultCheckID(fault)] != VerdictPass || len(stats.Faults) == 0 {
			return fmt.Errorf("PASS lacks required fault recovery check %s", faultCheckID(fault))
		}
		matching := 0
		for _, record := range stats.Faults {
			if record.Name == fault.Name && record.ScheduledOffset == fault.Offset {
				matching++
				if !record.InjectionSucceeded || !record.Ready || record.InjectError != "" || record.RecoveryError != "" || record.Recovery == nil || record.Recovery.Interrupted || record.Recovery.TimedOut || record.Recovery.Error != "" {
					return fmt.Errorf("PASS contradicts incomplete fault observations")
				}
			}
		}
		if matching != 1 {
			return fmt.Errorf("PASS lacks exactly one observation per planned fault")
		}
	}
	return nil
}
