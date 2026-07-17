package main

import (
	"context"
	"fmt"
	"time"
)

type FaultSpec struct {
	Name                    string
	Offset                  time.Duration
	DisconnectFor           time.Duration
	RequireActiveContainers int
}

func StandardFaultPlan() []FaultSpec {
	return []FaultSpec{
		{Name: "scheduler", Offset: 180 * time.Minute},
		{Name: "dispatcher", Offset: 190 * time.Minute},
		{Name: "worker", Offset: 200 * time.Minute, RequireActiveContainers: 20},
		{Name: "admin-api", Offset: 210 * time.Minute},
		{Name: "postgres", Offset: 220 * time.Minute, DisconnectFor: 30 * time.Second},
	}
}

// knownFault reports whether name is a component the fault injector can target.
func knownFault(name string) bool {
	switch name {
	case "scheduler", "dispatcher", "worker", "admin-api", "postgres":
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
		{Name: "dispatcher", ReadyTimeout: 2 * time.Minute, BusinessTimeout: 5 * time.Minute},
		{Name: "worker", ReadyTimeout: 2 * time.Minute, BusinessTimeout: 10 * time.Minute},
		{Name: "admin-api", ReadyTimeout: 2 * time.Minute, BusinessTimeout: 3 * time.Minute},
		{Name: "postgres", ReadyTimeout: 3 * time.Minute, BusinessTimeout: 10 * time.Minute},
	}
}

func ValidateFaultOrder(plan []FaultSpec) error {
	want := []string{"scheduler", "dispatcher", "worker", "admin-api", "postgres"}
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
	switch name {
	case "scheduler":
		return []string{"kubectl", "rollout", "restart", "deployment/orbitjob-scheduler", "-n", namespace}, nil
	case "dispatcher":
		return []string{"kubectl", "rollout", "restart", "deployment/orbitjob-dispatcher", "-n", namespace}, nil
	case "worker":
		return []string{"kubectl", "delete", "pod", "-n", namespace, "-l", "app.kubernetes.io/name=orbitjob-worker"}, nil
	case "admin-api":
		return []string{"kubectl", "rollout", "restart", "deployment/orbitjob-admin-api", "-n", namespace}, nil
	case "postgres":
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown fault %s", name)
	}
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
