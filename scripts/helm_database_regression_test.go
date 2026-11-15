package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDatabaseChartGateInspectsEntireDeployment(t *testing.T) {
	for _, ownerInRuntime := range []bool{false, true} {
		dir := t.TempDir()
		chart := filepath.Join(dir, "chart")
		if err := os.MkdirAll(chart, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(chart, "values.schema.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		manifest := "kind: Job\nspec:\n  keys:\n  - key: bootstrap-owner-dsn\n  - key: migrator-dsn\n---\nkind: Deployment\nmetadata:\n  name: runtime\nspec:\n  keys:\n  - key: admin-dsn\n  - key: runtime-dsn\n" + strings.Repeat("  # more deployment configuration\n", 100)
		if ownerInRuntime {
			manifest += "  - key: bootstrap-owner-dsn\n"
		}
		if err := os.WriteFile(filepath.Join(dir, "rendered.yaml"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		writeGateExecutable(t, filepath.Join(dir, "fakebin", "helm"), `#!/bin/sh
case " $* " in
  *" --show-only "*) printf 'kind: Deployment\nspec:\n  name: custom-database\n  key: custom-runtime\n' ;;
  *) cat "$CHART_RENDER" ;;
esac
`)
		script, err := filepath.Abs("helm-database-test.sh")
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("bash", script, chart)
		cmd.Dir = dir
		cmd.Env = append(gateEnvironment("PATH", "CHART_RENDER"),
			"PATH="+filepath.Join(dir, "fakebin")+":"+os.Getenv("PATH"), "CHART_RENDER="+filepath.Join(dir, "rendered.yaml"))
		out, err := cmd.CombinedOutput()
		if (err != nil) != ownerInRuntime {
			t.Fatalf("ownerInRuntime=%v success=%v:\n%s", ownerInRuntime, err == nil, out)
		}
		if ownerInRuntime && !strings.Contains(string(out), "runtime deployment references bootstrap owner credential") {
			t.Fatalf("gate did not report the leaked owner credential:\n%s", out)
		}
	}
}
