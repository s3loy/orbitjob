package v1alpha1

import "testing"

func TestReplaceCondition(t *testing.T) {
	got := ReplaceCondition([]Condition{{Type: "Ready", Status: "False"}}, Condition{Type: "Ready", Status: "True"})
	if len(got) != 1 || got[0].Status != "True" {
		t.Fatalf("%+v", got)
	}
	got = ReplaceCondition(got, Condition{Type: "Synced", Status: "True"})
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
}
