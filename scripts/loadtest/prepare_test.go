package main

import "testing"

func TestQualificationLoadManifests(t *testing.T) {
	if err := ValidateLoadManifests("../../deploy/load/namespace.yaml", "../../deploy/load/operations-rbac.yaml"); err != nil {
		t.Fatal(err)
	}
}
