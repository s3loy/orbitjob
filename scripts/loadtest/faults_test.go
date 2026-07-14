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
