package main

import (
	"strings"
	"testing"
	"time"
)

func TestInterruptedRecoveryIsIncompleteAndNeverReportedRecovered(t *testing.T) {
	cfg := Config{Duration: time.Minute, Faults: FaultsConfig{Plan: []FaultPhase{{Name: "operator", Offset: time.Second}}}}
	stats := RunStats{FaultPlan: activeFaultPlan(cfg), Faults: []FaultRecord{{Name: "operator", ScheduledOffset: time.Second, InjectedAt: "observed", InjectionSucceeded: true, Ready: true, RecoveryError: "context canceled", RecoveryIncomplete: true, BusinessRecovered: true, BusinessEvidence: "self-reported"}}}
	checks := faultEvidenceChecks(stats, cfg)
	if len(checks) != 1 || checks[0].Status != VerdictInconclusive {
		t.Fatalf("interrupted probe is not a proven business failure: %+v", checks)
	}
	stats.Faults[0].RecoveryError = ""
	body := buildReportBody(RunRecord{}, Result{}, &stats)
	if !strings.Contains(body, "business recovery UNVERIFIED") || strings.Contains(body, "business recovered") {
		t.Fatalf("report treats recorded claims as recovery evidence: %s", body)
	}
}
