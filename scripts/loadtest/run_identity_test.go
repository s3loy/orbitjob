package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/oklog/ulid/v2"
	kubevalidation "k8s.io/apimachinery/pkg/util/validation"
)

func generateIdentityFixture(t *testing.T, root, id string) ([]string, []Definition) {
	t.Helper()
	if err := runGenerate([]string{
		"--config", "../../test/load/config/smoke.yaml", "--profile", "smoke",
		"--images", "../../test/load/config/images.lock.yaml",
		"--scenarios", "../../test/load/scenarios", "--run-root", root, "--run-id", id,
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig("../../test/load/config/smoke.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = resolveRunTenants(cfg, root, id, false)
	if err != nil {
		t.Fatal(err)
	}
	definitions, err := loadGeneratedDefinitions(filepath.Join(root, id, "generated", "definitions.json"))
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Tenants, definitions
}

func TestRunIdentitiesAreGeneratedOnceAndIsolatedAcrossRuns(t *testing.T) {
	root := t.TempDir()
	firstIDs, firstDefinitions := generateIdentityFixture(t, root, "first")
	repeatedIDs, repeatedDefinitions := generateIdentityFixture(t, root, "first")
	if !reflect.DeepEqual(firstIDs, repeatedIDs) || !reflect.DeepEqual(firstDefinitions, repeatedDefinitions) {
		t.Fatal("reloading the same run changed its tenant identities or declarations")
	}
	secondIDs, secondDefinitions := generateIdentityFixture(t, root, "second")
	seen := map[string]bool{}
	for _, id := range append(firstIDs, secondIDs...) {
		if _, err := ulid.ParseStrict(id); err != nil || seen[id] {
			t.Fatalf("tenant identity is invalid or reused: %q, %v", id, err)
		}
		seen[id] = true
	}
	for i, first := range firstDefinitions {
		if first.CaseID == secondDefinitions[i].CaseID {
			t.Fatal("different runs can reuse the same ScheduledJob revision as evidence")
		}
		if problems := kubevalidation.IsDNS1123Subdomain(first.CaseID); len(problems) != 0 {
			t.Fatalf("generated Kubernetes name %q is invalid: %v", first.CaseID, problems)
		}
	}
}

func TestStartedRunEvidenceCannotBeOverwritten(t *testing.T) {
	root := t.TempDir()
	generateIdentityFixture(t, root, "started")
	dir := filepath.Join(root, "started")
	if err := claimRun(dir, "started"); err != nil {
		t.Fatal(err)
	}
	if err := claimRun(dir, "started"); err == nil {
		t.Fatal("started one run twice")
	}
	if err := ensureRunNotStarted(root, "started"); err == nil {
		t.Fatal("allowed regeneration over started-run evidence")
	}
}

func TestRunIdentityRejectsChangedConfigAndTraversal(t *testing.T) {
	root := t.TempDir()
	generateIdentityFixture(t, root, "recorded")
	cfg, err := LoadConfig("../../test/load/config/smoke.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Seed = "different"
	if _, err := resolveRunTenants(cfg, root, "recorded", false); err == nil {
		t.Fatal("accepted a different configuration for recorded run identities")
	}
	if _, err := resolveRunTenants(cfg, root, "../outside", true); err == nil {
		t.Fatal("created identities outside the run directory")
	}
	body, err := os.ReadFile(filepath.Join(root, "recorded", "tenant-ids.json"))
	if err != nil || !json.Valid(body) {
		t.Fatalf("identity record is unreadable: %v", err)
	}
}
