package main

import (
	"testing"
	"time"
)

func TestStandardFaultPlan(t *testing.T) {
	plan := StandardFaultPlan()
	want := []string{"scheduler", "operator", "admin-api", "postgres"}
	names := FaultNames(plan)
	for i, name := range want {
		if names[i] != name {
			t.Fatalf("fault %d = %s, want %s", i, names[i], name)
		}
	}
	if plan[3].DisconnectFor != 30*time.Second {
		t.Fatalf("postgres disconnect = %s", plan[3].DisconnectFor)
	}
}

func TestValidateFaultOrderRejectsOverlap(t *testing.T) {
	plan := []FaultSpec{
		{Name: "scheduler", Offset: 180 * time.Minute},
		{Name: "operator", Offset: 180 * time.Minute},
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
	for _, name := range []string{"scheduler", "operator", "admin-api"} {
		cmd, err := FaultInjectionCommand(name, "orbitjob")
		if err != nil {
			t.Fatal(err)
		}
		if len(cmd) == 0 {
			t.Fatalf("%s produced empty command", name)
		}
	}
	// postgres scales down and restores up.
	inject, restore, err := FaultInjectionPlan("postgres", "orbitjob")
	if err != nil {
		t.Fatal(err)
	}
	if len(inject) == 0 || len(restore) == 0 {
		t.Fatalf("postgres plan incomplete: inject=%v restore=%v", inject, restore)
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
		{Name: "operator", Offset: 30 * time.Minute},
		{Name: "postgres", Offset: 40 * time.Minute},
	}}}
	plan := FaultPlanFromConfig(cfg)
	if len(plan) != 2 {
		t.Fatalf("plan has %d faults, want 2", len(plan))
	}
	if plan[0].Name != "operator" || plan[0].Offset != 30*time.Minute {
		t.Fatalf("fault 0 = %+v", plan[0])
	}
	if plan[1].Name != "postgres" || plan[1].Offset != 40*time.Minute {
		t.Fatalf("fault 1 = %+v", plan[1])
	}
}

// A fault has to name the namespace its target actually lives in. PostgreSQL
// and the control plane are in different namespaces, and a fault addressed to
// the wrong one scales a Deployment that is not there -- the injection command
// succeeds and nothing happens.
func TestFaultNamespaceTargetsTheRightPlace(t *testing.T) {
	if got := FaultNamespace("postgres"); got != DatabaseNamespace() {
		t.Fatalf("postgres fault targets %q, want %q", got, DatabaseNamespace())
	}
	for _, name := range []string{"scheduler", "operator", "admin-api"} {
		if got := FaultNamespace(name); got != WorkloadNamespace() {
			t.Fatalf("%s fault targets %q, want %q", name, got, WorkloadNamespace())
		}
	}
}

// A postgres fault declared in a config carries only a name and an offset, so
// DisconnectFor is zero. The restore step must not depend on it: gating restore
// on that field left PostgreSQL scaled to zero with nothing to bring it back.
func TestConfigDeclaredPostgresFaultStillHasARestoreStep(t *testing.T) {
	plan := FaultPlanFromConfig(Config{Faults: FaultsConfig{Plan: []FaultPhase{
		{Name: "postgres", Offset: 25 * time.Minute},
	}}})
	if len(plan) != 1 {
		t.Fatalf("expected one fault, got %d", len(plan))
	}
	if plan[0].DisconnectFor != 0 {
		t.Fatalf("expected no disconnect window, got %s", plan[0].DisconnectFor)
	}
	_, restore, err := FaultInjectionPlan("postgres", FaultNamespace("postgres"))
	if err != nil {
		t.Fatalf("plan postgres fault: %v", err)
	}
	if len(restore) == 0 {
		t.Fatal("postgres fault has no restore command")
	}
}
