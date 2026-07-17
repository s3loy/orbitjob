package main

import (
	"testing"
	"time"
)

func TestStandardFaultPlan(t *testing.T) {
	plan := StandardFaultPlan()
	want := []string{"scheduler", "dispatcher", "worker", "admin-api", "postgres"}
	names := FaultNames(plan)
	for i, name := range want {
		if names[i] != name {
			t.Fatalf("fault %d = %s, want %s", i, names[i], name)
		}
	}
	if plan[2].RequireActiveContainers < 20 {
		t.Fatalf("worker active requirement = %d", plan[2].RequireActiveContainers)
	}
	if plan[4].DisconnectFor != 30*time.Second {
		t.Fatalf("postgres disconnect = %s", plan[4].DisconnectFor)
	}
}

func TestValidateFaultOrderRejectsOverlap(t *testing.T) {
	plan := []FaultSpec{
		{Name: "scheduler", Offset: 180 * time.Minute},
		{Name: "dispatcher", Offset: 180 * time.Minute},
	}
	if err := ValidateFaultOrder(plan); err == nil {
		t.Fatal("overlapping faults should be rejected")
	}
}

func TestValidateFaultOrderAcceptsStandardPlan(t *testing.T) {
	if err := ValidateFaultOrder(StandardFaultPlan()); err != nil {
		t.Fatal(err)
	}
}

func TestFaultInjectionCommands(t *testing.T) {
	for _, name := range []string{"scheduler", "dispatcher", "worker", "admin-api"} {
		cmd, err := FaultInjectionCommand(name, "orbitjob")
		if err != nil {
			t.Fatal(err)
		}
		if len(cmd) == 0 {
			t.Fatalf("%s produced empty command", name)
		}
	}
	if _, err := FaultInjectionCommand("postgres", "orbitjob"); err != nil {
		t.Fatalf("postgres should return nil command, not error: %v", err)
	}
}

func TestFaultPlanFromConfigDefaultsToStandard(t *testing.T) {
	plan := FaultPlanFromConfig(Config{})
	standard := StandardFaultPlan()
	if len(plan) != len(standard) {
		t.Fatalf("default plan has %d faults, want %d", len(plan), len(standard))
	}
	for i := range standard {
		if plan[i] != standard[i] {
			t.Fatalf("fault %d = %+v, want %+v", i, plan[i], standard[i])
		}
	}
}

func TestFaultPlanFromConfigUsesCustomPlan(t *testing.T) {
	cfg := Config{Faults: FaultsConfig{Plan: []FaultPhase{
		{Name: "worker", Offset: 30 * time.Minute},
		{Name: "postgres", Offset: 40 * time.Minute},
	}}}
	plan := FaultPlanFromConfig(cfg)
	if len(plan) != 2 {
		t.Fatalf("plan has %d faults, want 2", len(plan))
	}
	if plan[0].Name != "worker" || plan[0].Offset != 30*time.Minute {
		t.Fatalf("fault 0 = %+v", plan[0])
	}
	if plan[1].Name != "postgres" || plan[1].Offset != 40*time.Minute {
		t.Fatalf("fault 1 = %+v", plan[1])
	}
}
