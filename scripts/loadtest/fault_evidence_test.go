package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFaultVerdictRequiresEveryStageAndPersistedPlan(t *testing.T) {
	cfg := Config{Duration: time.Minute, Faults: FaultsConfig{Plan: []FaultPhase{{Name: "operator", Offset: time.Second}}}}
	plan := activeFaultPlan(cfg)
	complete := FaultRecord{Name: "operator", ScheduledOffset: time.Second, InjectedAt: time.Now().UTC().Format(time.RFC3339Nano), InjectionSucceeded: true, Ready: true, BusinessRecovered: true, BusinessEvidence: "independent scoped observation"}
	for _, tc := range []struct {
		name   string
		mutate func(*RunStats)
		want   Verdict
	}{
		{"self-reported business recovery rejected", func(*RunStats) {}, VerdictInconclusive},
		{"plan missing", func(s *RunStats) { s.FaultPlan = nil }, VerdictInconclusive},
		{"record missing", func(s *RunStats) { s.Faults = nil }, VerdictInconclusive},
		{"injection failed", func(s *RunStats) { s.Faults[0].InjectError = "kubectl failed" }, VerdictInconclusive},
		{"restore unproven", func(s *RunStats) { s.Faults[0].InjectionSucceeded = false }, VerdictInconclusive},
		{"readiness failed", func(s *RunStats) { s.Faults[0].RecoveryError = "readiness timed out" }, VerdictFail},
		{"not ready", func(s *RunStats) { s.Faults[0].Ready = false }, VerdictInconclusive},
		{"ready only", func(s *RunStats) { s.Faults[0].BusinessRecovered = false }, VerdictInconclusive},
		{"business source missing", func(s *RunStats) { s.Faults[0].BusinessEvidence = "" }, VerdictInconclusive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stats := RunStats{FaultPlan: plan, Faults: []FaultRecord{complete}}
			tc.mutate(&stats)
			e := completeVerificationEvidence()
			e.FaultChecks = faultEvidenceChecks(stats, cfg)
			result := EvaluateQualification(RunRecord{Qualification: true}, e, 1)
			if result.Verdict != tc.want {
				t.Fatalf("verdict=%s want=%s: %+v", result.Verdict, tc.want, result)
			}
		})
	}
}

func fakeFaultKubectl(t *testing.T) (string, string) {
	t.Helper()
	bin := t.TempDir()
	down, restored := filepath.Join(bin, "down"), filepath.Join(bin, "restored")
	script := "#!/bin/sh\ncase \"$*\" in\n*--replicas=0*) printf down > \"$FAULT_DOWN\";;\n*--replicas=1*) printf restored > \"$FAULT_RESTORED\";;\n*) printf injected > \"$FAULT_INJECTED\"; printf '{\"status\":{\"readyReplicas\":1}}';;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("FAULT_DOWN", down)
	t.Setenv("FAULT_RESTORED", restored)
	t.Setenv("FAULT_INJECTED", filepath.Join(bin, "injected"))
	return down, restored
}

func TestRunJoinsFaultCompensationBeforeReturningStats(t *testing.T) {
	down, restored := fakeFaultKubectl(t)
	engine := NewRunEngine(nil, PhaseSchedule{Duration: time.Hour}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	clock := func() time.Duration {
		if _, err := os.Stat(down); err == nil {
			return time.Hour
		}
		return 0
	}
	records, err := runEngineWithFaults(ctx, engine, clock, []FaultSpec{{Name: "postgres", DisconnectFor: time.Hour}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(down); err != nil {
		t.Fatal("fake injection did not run", err)
	}
	if _, err := os.Stat(restored); err != nil {
		t.Fatal("stats returned before restoration", err)
	}
	if len(records) != 1 || records[0].InjectError == "" {
		t.Fatalf("incomplete final records: %+v", records)
	}
}

func TestFaultUsesEngineVirtualClockUnderAcceleration(t *testing.T) {
	fakeFaultKubectl(t)
	pace := NewPaceController(FeedbackConfig{}, TuningParameters{}, 0, time.Second)
	pace.paceFactor = 4
	engine := NewRunEngineWithPace(nil, PhaseSchedule{Duration: 3 * time.Hour}, nil, pace, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	clock := func() time.Duration {
		if _, err := os.Stat(os.Getenv("FAULT_INJECTED")); err == nil {
			return 45 * time.Minute
		}
		return 15 * time.Minute
	}
	records, err := runEngineWithFaults(ctx, engine, clock, []FaultSpec{{Name: "operator", Offset: time.Hour}})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].InjectedAt == "" || records[0].ScheduledOffset != time.Hour {
		t.Fatalf("fault did not fire on virtual time: %+v", records)
	}
	if engine.VirtualElapsed() != 3*time.Hour {
		t.Fatalf("virtual clock=%s", engine.VirtualElapsed())
	}
	if records[0].BusinessRecovered {
		t.Fatal("readiness was misreported as business recovery")
	}
}

func TestReadyDeadlineDoesNotUseBusinessBudget(t *testing.T) {
	start := time.Now()
	err := WaitForRecovery(context.Background(), RecoveryGate{Name: "operator", ReadyTimeout: 10 * time.Millisecond, BusinessTimeout: time.Hour}, func(context.Context) bool { return false })
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("readiness deadline ignored: %v", err)
	}
}
