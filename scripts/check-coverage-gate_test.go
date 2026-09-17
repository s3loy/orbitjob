package main

// The coverage gate is worth its build time only if it fails when it must
// (ADR 0007). These tests build throwaway modules, run the real
// scripts/check-coverage.sh against them, and assert the exit status: a tier
// package without tests fails, a tier package the profile never mentions
// fails, a profile with no data fails, a module with no tier package fails,
// and a covered tier package still passes.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// probeGoMod keeps the fixture modules version-agnostic; any toolchain that can
// build this repository accepts it.
const probeGoMod = "module probe\n\ngo 1.21\n"

// gatePath resolves the gate relative to this package's source directory,
// which is the working directory go test gives us.
func gatePath(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs("check-coverage.sh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("gate script: %v", err)
	}
	return path
}

// writeModule lays out a fixture module and returns its root.
func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// runGate runs the gate with dir as its working directory, so the `go list`
// inside it sees the fixture module instead of this repository, and with a
// baseline path that does not exist so this repository's baseline cannot
// change the outcome.
func runGate(t *testing.T, dir, coverFile string) (string, int) {
	t.Helper()
	cmd := exec.Command("bash", gatePath(t), coverFile)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "BASELINE_FILE="+filepath.Join(dir, "no-baseline.txt"))
	out, err := cmd.CombinedOutput()
	switch err := err.(type) {
	case nil:
		return string(out), 0
	case *exec.ExitError:
		return string(out), err.ExitCode()
	default:
		t.Fatalf("run gate: %v\n%s", err, out)
		return "", 0
	}
}

// profileFor produces a real coverage profile for the fixture, the same way
// the Makefile produces the one the gate consumes.
func profileFor(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("go", "test", "-coverprofile=cover.out", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go test in fixture: %v\n%s", err, out)
	}
	return filepath.Join(dir, "cover.out")
}

// A tier package with statements and no test file is instrumented at zero
// percent. The gate has to fail it and has to say how many packages it looked
// at, so a shrinking check set cannot pass as a quiet success.
func TestGateFailsOnUntestedTierPackage(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":                              probeGoMod,
		"internal/core/domain/probe/probe.go": "package probe\n\nfunc Value() int { return 1 }\n",
	})
	out, code := runGate(t, dir, profileFor(t, dir))
	if code == 0 {
		t.Fatalf("gate passed with an untested tier package:\n%s", out)
	}
	if !strings.Contains(out, "checked 1 of 1 tier package(s)") {
		t.Fatalf("gate did not report the check set size:\n%s", out)
	}
	if !strings.Contains(out, "domain/probe") {
		t.Fatalf("gate did not name the untested package:\n%s", out)
	}
}

// A tier package that declares no statements emits no profile line at all, so
// the check set cannot be read off the profile (ADR 0007). This is the shape
// of the three declaration-only packages in this repository. The second,
// non-tier package keeps data rows in the profile, which is what makes this a
// test of the per-package report rather than of the empty-profile report.
func TestGateFailsOnTierPackageTheProfileNeverMentions(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":                            probeGoMod,
		"internal/core/domain/dead/dead.go": "package dead\n\ntype T struct{ N int }\n",
		"internal/platform/thing/thing.go":  "package thing\n\nfunc Other() int { return 2 }\n",
		"internal/platform/thing/thing_test.go": "package thing\n\nimport \"testing\"\n\n" +
			"func TestOther(t *testing.T) {\n\tif Other() != 2 {\n\t\tt.Fatal(\"Other\")\n\t}\n}\n",
	})
	profile := profileFor(t, dir)
	out, code := runGate(t, dir, profile)
	if code == 0 {
		t.Fatalf("gate passed over a tier package the profile never mentions:\n%s", out)
	}
	if !strings.Contains(out, "no coverage data and no test files") {
		t.Fatalf("gate did not report the missing coverage data:\n%s", out)
	}
}

// A profile holding nothing but its header used to print "coverage gate passed"
// over zero packages. It is a failure.
func TestGateFailsOnProfileWithNoData(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":                              probeGoMod,
		"internal/core/domain/probe/probe.go": "package probe\n\nfunc Value() int { return 1 }\n",
	})
	empty := filepath.Join(dir, "empty.out")
	if err := os.WriteFile(empty, []byte("mode: set\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := runGate(t, dir, empty)
	if code == 0 {
		t.Fatalf("gate passed on a profile with no data:\n%s", out)
	}
	if !strings.Contains(out, "holds no coverage data") {
		t.Fatalf("gate did not report the empty profile:\n%s", out)
	}
}

// When nothing in the module lands in a tier, the check set is empty. An empty
// check set is a fault signal, not a pass (ADR 0007).
func TestGateFailsOnEmptyCheckSet(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":                           probeGoMod,
		"internal/platform/probe/probe.go": "package probe\n\nfunc Value() int { return 1 }\n",
	})
	profile := profileFor(t, dir)
	out, code := runGate(t, dir, profile)
	if code == 0 {
		t.Fatalf("gate passed with an empty check set:\n%s", out)
	}
	if !strings.Contains(out, "no package in a coverage tier") {
		t.Fatalf("gate did not report the empty check set:\n%s", out)
	}
}

// The failure conditions above only mean something if a gate over a covered
// tier package still passes. Without this the gate could be failing always.
func TestGatePassesOnCoveredTierPackage(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":                              probeGoMod,
		"internal/core/domain/probe/probe.go": "package probe\n\nfunc Value() int { return 1 }\n",
		"internal/core/domain/probe/probe_test.go": "package probe\n\nimport \"testing\"\n\n" +
			"func TestValue(t *testing.T) {\n\tif Value() != 1 {\n\t\tt.Fatal(\"Value\")\n\t}\n}\n",
	})
	out, code := runGate(t, dir, profileFor(t, dir))
	if code != 0 {
		t.Fatalf("gate failed on a covered tier package:\n%s", out)
	}
	if !strings.Contains(out, "coverage gate passed") {
		t.Fatalf("gate did not report a pass:\n%s", out)
	}
}
