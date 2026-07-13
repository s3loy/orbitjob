package main

import (
	"reflect"
	"testing"
)

func TestGenerateStandardManifest(t *testing.T) {
	cfg, err := LoadConfig("../../test/load/config/standard.yaml")
	if err != nil {
		t.Fatal(err)
	}
	scenarios, err := LoadScenarios("../../test/load/scenarios")
	if err != nil {
		t.Fatal(err)
	}
	images, err := LoadImageLock("../../test/load/config/images.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	a, err := Generate(cfg, scenarios, images)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Generate(cfg, scenarios, images)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Definitions) != 1200 || !reflect.DeepEqual(a, b) {
		t.Fatalf("manifest size=%d deterministic=%v", len(a.Definitions), reflect.DeepEqual(a, b))
	}
	counts := map[string]int{}
	for _, definition := range a.Definitions {
		counts[definition.Category]++
	}
	for category, want := range cfg.Definitions.Categories {
		if counts[category] != want {
			t.Errorf("%s=%d want=%d", category, counts[category], want)
		}
	}
}

func TestWebhookOriginUsesManualProductTrigger(t *testing.T) {
	manifest := mustGenerate(t)
	count := 0
	for _, definition := range manifest.Definitions {
		if definition.TriggerOrigin != "webhook" {
			continue
		}
		count++
		if definition.ProductTriggerType != "manual" || definition.Request["trigger_type"] != "manual" {
			t.Fatalf("%s has unsupported product trigger", definition.CaseID)
		}
	}
	if count != 120 {
		t.Fatalf("webhook-origin definitions=%d", count)
	}
}

func mustGenerate(t *testing.T) Manifest {
	t.Helper()
	cfg, _ := LoadConfig("../../test/load/config/standard.yaml")
	scenarios, _ := LoadScenarios("../../test/load/scenarios")
	images, _ := LoadImageLock("../../test/load/config/images.lock.yaml")
	manifest, err := Generate(cfg, scenarios, images)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}
