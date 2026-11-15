package main

import (
	"context"
	"fmt"
	"time"
)

func activeFaultPlan(cfg Config) []FaultSpec {
	plan := make([]FaultSpec, 0)
	for _, fault := range FaultPlanFromConfig(cfg) {
		if fault.Offset >= 0 && fault.Offset < cfg.Duration {
			plan = append(plan, fault)
		}
	}
	return plan
}

// The stats caller cannot snapshot until the injector has completed its bounded
// compensations. Cancellation stops waits and recovery probes, not restoration.
func runEngineWithFaults(ctx context.Context, engine *RunEngine, clock func() time.Duration, plan []FaultSpec, probers ...*RecoveryProber) ([]FaultRecord, error) {
	runCtx, cancel := context.WithCancel(ctx)
	recorder := &FaultRecorder{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		injectFaultsAt(runCtx, engine.VirtualElapsed, plan, recorder, probers...)
	}()
	err := engine.Run(runCtx, clock)
	// Normal schedule completion drains an already-started bounded probe.
	// Cancellation and engine failure still stop observations immediately.
	if err != nil {
		cancel()
	}
	engine.Stop()
	<-done
	cancel()
	return recorder.Records(), err
}

func injectFaultsAt(ctx context.Context, clock func() time.Duration, plan []FaultSpec, rec *FaultRecorder, probers ...*RecoveryProber) {
	var prober *RecoveryProber
	if len(probers) > 0 {
		prober = probers[0]
	}
	for _, fault := range plan {
		for clock() < fault.Offset {
			timer := time.NewTimer(10 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		if ctx.Err() != nil {
			return
		}
		started := time.Now()
		record := FaultRecord{Name: fault.Name, ScheduledOffset: fault.Offset, InjectedAt: started.UTC().Format(time.RFC3339Nano)}
		if prober != nil {
			record.Recovery = prober.before(ctx, fault)
		}
		started = time.Now()
		record.InjectedAt = started.UTC().Format(time.RFC3339Nano)
		inject, restore, err := FaultInjectionPlan(fault.Name, FaultNamespace(fault.Name))
		if err == nil && len(inject) > 0 {
			var during func(context.Context)
			if prober != nil {
				during = func(ctx context.Context) { prober.during(ctx, fault, record.Recovery) }
			}
			err = executeFaultDuring(ctx, fault, inject, restore, during)
		}
		if err != nil {
			if prober != nil {
				prober.cleanup(record.Recovery)
			}
			record.InjectError = err.Error()
			rec.add(record)
			fmt.Printf("fault: %s injection/restore failed: %s\n", fault.Name, record.InjectError)
			continue
		}
		record.InjectionSucceeded = true
		if prober != nil {
			prober.restored(ctx, fault, record.Recovery)
		}
		gate := recoveryGate(fault.Name)
		if err := WaitForRecovery(ctx, gate, deploymentReadyCheck(fault.Name, FaultNamespace(fault.Name))); err != nil {
			record.RecoveryError = err.Error()
			record.RecoveryIncomplete = ctx.Err() != nil
		} else {
			record.Ready = true
			record.ReadySeconds = time.Since(started).Seconds()
		}
		if prober != nil {
			if record.Ready {
				prober.after(ctx, fault, record.Recovery)
			}
			prober.cleanup(record.Recovery)
		}
		rec.add(record)
		fmt.Printf("fault: %s ready=%v; business recovery requires independent verification\n", fault.Name, record.Ready)
	}
}

func sameFaultPlan(a, b []FaultSpec) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func faultEvidenceChecks(stats RunStats, cfg Config, observed ...map[string]CheckResult) []CheckResult {
	expected := activeFaultPlan(cfg)
	if !sameFaultPlan(stats.FaultPlan, expected) || (cfg.Qualification && len(expected) == 0) {
		return []CheckResult{{ID: "fault-plan", Status: VerdictInconclusive, Message: "persisted fault plan is missing or differs from the selected profile"}}
	}
	checks := make([]CheckResult, 0, len(expected))
	if len(stats.Faults) != len(expected) {
		checks = append(checks, CheckResult{ID: "fault-record-count", Status: VerdictInconclusive, Message: "fault records do not match the persisted plan"})
	}
	for _, fault := range expected {
		check := CheckResult{ID: fmt.Sprintf("fault-recovery:%s@%s", fault.Name, fault.Offset), Status: VerdictInconclusive}
		var records []FaultRecord
		for _, record := range stats.Faults {
			if record.Name == fault.Name && record.ScheduledOffset == fault.Offset {
				records = append(records, record)
			}
		}
		if len(records) != 1 {
			check.Message = fmt.Sprintf("expected one fault record, got %d", len(records))
		} else {
			record := records[0]
			switch {
			case record.InjectError != "" || !record.InjectionSucceeded || record.InjectedAt == "":
				check.Message = "fault injection/restore was not proven successful: " + record.InjectError
			case record.RecoveryError != "":
				check.Message = "readiness observation interrupted: " + record.RecoveryError
				if !record.RecoveryIncomplete {
					check.Status, check.Message = VerdictFail, "readiness recovery failed: "+record.RecoveryError
				}
			case !record.Ready:
				check.Message = "readiness recovery evidence missing"
			default:
				if len(observed) > 0 {
					if independent, ok := observed[0][check.ID]; ok {
						check = independent
						break
					}
				}
				// Without a collected component probe, persisted claims cannot
				// substitute for one, even if old/manually edited stats set
				// business_recovered or supply a free-text evidence string.
				check.Message = "business recovery unverified; ready replicas and recorded claims are not independent business evidence"
			}
		}
		checks = append(checks, check)
	}
	return checks
}
