package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type FaultSpec struct {
	Name          string
	Offset        time.Duration
	DisconnectFor time.Duration
}

// StandardFaultPlan faults every surviving control-plane component in turn.
//
// The dispatcher and worker Deployments this plan used to fault are gone; the
// operator now hosts the execution path they owned, so it takes their slot.
func StandardFaultPlan() []FaultSpec {
	return []FaultSpec{
		{Name: "scheduler", Offset: 180 * time.Minute},
		{Name: "operator", Offset: 190 * time.Minute},
		{Name: "admin-api", Offset: 210 * time.Minute},
		{Name: "postgres", Offset: 220 * time.Minute, DisconnectFor: 30 * time.Second},
	}
}

// knownFault reports whether name is a component the fault injector can target.
func knownFault(name string) bool {
	switch name {
	case "scheduler", "operator", "admin-api", "postgres":
		return true
	}
	return false
}

// FaultPlanFromConfig returns the configured fault plan, or StandardFaultPlan
// when the profile does not declare one.
func FaultPlanFromConfig(cfg Config) []FaultSpec {
	if len(cfg.Faults.Plan) == 0 {
		return StandardFaultPlan()
	}
	plan := make([]FaultSpec, len(cfg.Faults.Plan))
	for i, fault := range cfg.Faults.Plan {
		plan[i] = FaultSpec{Name: fault.Name, Offset: fault.Offset}
	}
	return plan
}

func FaultNames(plan []FaultSpec) []string {
	names := make([]string, len(plan))
	for i, fault := range plan {
		names[i] = fault.Name
	}
	return names
}

type RecoveryGate struct {
	Name            string
	ReadyTimeout    time.Duration
	BusinessTimeout time.Duration
}

func StandardRecoveryGates() []RecoveryGate {
	return []RecoveryGate{
		{Name: "scheduler", ReadyTimeout: 2 * time.Minute, BusinessTimeout: 5 * time.Minute},
		{Name: "operator", ReadyTimeout: 2 * time.Minute, BusinessTimeout: 10 * time.Minute},
		{Name: "admin-api", ReadyTimeout: 2 * time.Minute, BusinessTimeout: 3 * time.Minute},
		{Name: "postgres", ReadyTimeout: 3 * time.Minute, BusinessTimeout: 10 * time.Minute},
	}
}

func ValidateFaultOrder(plan []FaultSpec) error {
	want := []string{"scheduler", "operator", "admin-api", "postgres"}
	if len(plan) != len(want) {
		return fmt.Errorf("fault plan has %d faults, want %d", len(plan), len(want))
	}
	for i, name := range want {
		if plan[i].Name != name {
			return fmt.Errorf("fault %d is %s, want %s", i, plan[i].Name, name)
		}
	}
	var prev time.Duration
	for _, fault := range plan {
		if fault.Offset <= prev && prev > 0 {
			return fmt.Errorf("fault %s overlaps previous", fault.Name)
		}
		prev = fault.Offset
	}
	return nil
}

func FaultInjectionCommand(name, namespace string) ([]string, error) {
	inject, _, err := FaultInjectionPlan(name, namespace)
	return inject, err
}

// FaultNamespace returns the namespace a fault's target lives in.
//
// The control plane and PostgreSQL sit in different namespaces, so one
// namespace for every fault is wrong for at least one of them: a postgres fault
// addressed to the control-plane namespace scales a Deployment that is not
// there, and the injection reports success while nothing happened.
func FaultNamespace(name string) string {
	if name == "postgres" {
		return DatabaseNamespace()
	}
	return WorkloadNamespace()
}

// FaultInjectionPlan returns the inject command and, for faults with a
// DisconnectFor window, the restore command that undoes it. Components without
// a restore step recover on their own (rollout restart, pod delete).
func FaultInjectionPlan(name, namespace string) (inject, restore []string, err error) {
	switch name {
	case "scheduler":
		return []string{"kubectl", "rollout", "restart", "deployment/orbitjob-scheduler", "-n", namespace}, nil, nil
	case "operator":
		return []string{"kubectl", "rollout", "restart", "deployment/orbitjob-operator", "-n", namespace}, nil, nil
	case "admin-api":
		return []string{"kubectl", "rollout", "restart", "deployment/orbitjob-admin-api", "-n", namespace}, nil, nil
	case "postgres":
		return []string{"kubectl", "scale", "deployment/orbitjob-postgres", "-n", namespace, "--replicas=0"},
			[]string{"kubectl", "scale", "deployment/orbitjob-postgres", "-n", namespace, "--replicas=1"}, nil
	default:
		return nil, nil, fmt.Errorf("unknown fault %s", name)
	}
}

// FaultRecord is one fault injection's evidence: when it fired, how long
// recovery took, and whether injection itself failed.
type FaultRecord struct {
	Name             string  `json:"name"`
	InjectedAt       string  `json:"injected_at"`
	RecoveredSeconds float64 `json:"recovered_seconds"`
	InjectError      string  `json:"inject_error,omitempty"`
}

// FaultRecorder collects FaultRecords from the injection goroutine; read after
// the run ends via Records.
type FaultRecorder struct {
	mu      sync.Mutex
	records []FaultRecord
}

func (r *FaultRecorder) add(rec FaultRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, rec)
}

func (r *FaultRecorder) Records() []FaultRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]FaultRecord, len(r.records))
	copy(out, r.records)
	return out
}

func WaitForRecovery(ctx context.Context, gate RecoveryGate, check func(context.Context) bool) error {
	readyCtx, cancel := context.WithTimeout(ctx, gate.BusinessTimeout)
	defer cancel()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	consecutive := 0
	for {
		select {
		case <-readyCtx.Done():
			return fmt.Errorf("%s recovery timed out", gate.Name)
		case <-ticker.C:
			if check(ctx) {
				consecutive++
				if consecutive >= 2 {
					return nil
				}
			} else {
				consecutive = 0
			}
		}
	}
}
