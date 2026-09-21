package main

// The coverage gate is worth its build time only if it fails when it must
// (ADR 0007). These tests build throwaway modules, run the real
// scripts/check-coverage.sh against them, and assert the exit status: a tier
// package without tests fails, a documented declaration-only package warns,
// a profile with no data fails, a module with no tier package fails, and the
// default 60% bar distinguishes under-covered from covered packages. Exception
// entries are validated rather than trusted, and the threshold is fixed policy.

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
	return runGateWithBaseline(t, dir, coverFile, filepath.Join(dir, "no-baseline.txt"))
}

func runGateWithBaseline(t *testing.T, dir, coverFile, baselineFile string) (string, int) {
	return runGateWithEnv(t, dir, coverFile, baselineFile)
}

func runGateWithEnv(t *testing.T, dir, coverFile, baselineFile string, extraEnv ...string) (string, int) {
	t.Helper()
	cmd := exec.Command("bash", gatePath(t), coverFile)
	cmd.Dir = dir
	cmd.Env = make([]string, 0, len(os.Environ())+len(extraEnv)+1)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "BASELINE_FILE=") ||
			strings.HasPrefix(entry, "CORE_MIN=") ||
			strings.HasPrefix(entry, "IO_MIN=") {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "BASELINE_FILE="+baselineFile)
	cmd.Env = append(cmd.Env, extraEnv...)
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

// A declaration-only package emits no profile line. It remains in the check
// set, but an explicit exception documents why no percentage can be measured.
func TestGateAllowsDocumentedDeclarationOnlyPackage(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":                            probeGoMod,
		"internal/core/domain/dead/dead.go": "package dead\n\ntype T struct{ N int }\n",
		"internal/platform/thing/thing.go":  "package thing\n\nfunc Other() int { return 2 }\n",
		"internal/platform/thing/thing_test.go": "package thing\n\nimport \"testing\"\n\n" +
			"func TestOther(t *testing.T) {\n\tif Other() != 2 {\n\t\tt.Fatal(\"Other\")\n\t}\n}\n",
		"coverage-baseline.txt": "probe/internal/core/domain/dead # declaration-only\n",
	})
	profile := profileFor(t, dir)
	out, code := runGateWithBaseline(t, dir, profile, filepath.Join(dir, "coverage-baseline.txt"))
	if code != 0 {
		t.Fatalf("gate failed on a documented declaration-only package:\n%s", out)
	}
	if !strings.Contains(out, "documented exception") {
		t.Fatalf("gate did not report the exception:\n%s", out)
	}
}

func TestGateFailsOnUndocumentedDeclarationOnlyPackage(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":                            probeGoMod,
		"internal/core/domain/dead/dead.go": "package dead\n\ntype T struct{ N int }\n",
		"internal/platform/thing/thing.go":  "package thing\n\nfunc Other() int { return 2 }\n",
		"internal/platform/thing/thing_test.go": "package thing\n\nimport \"testing\"\n\n" +
			"func TestOther(t *testing.T) {\n\tif Other() != 2 {\n\t\tt.Fatal(\"Other\")\n\t}\n}\n",
	})
	out, code := runGate(t, dir, profileFor(t, dir))
	if code == 0 {
		t.Fatalf("gate passed an undocumented declaration-only package:\n%s", out)
	}
	if !strings.Contains(out, "no coverage data and no test files") {
		t.Fatalf("gate did not report the missing coverage data:\n%s", out)
	}
}

func TestGateRejectsMeasurablePackageException(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": probeGoMod,
		"internal/core/domain/probe/probe.go": "package probe\n\n" +
			"func Covered() int { return 1 }\n" +
			"func Uncovered() int { return 2 }\n",
		"internal/core/domain/probe/probe_test.go": "package probe\n\nimport \"testing\"\n\n" +
			"func TestCovered(t *testing.T) {\n\tif Covered() != 1 {\n\t\tt.Fatal(\"Covered\")\n\t}\n}\n",
		"coverage-baseline.txt": "probe/internal/core/domain/probe # declaration-only\n",
	})
	out, code := runGateWithBaseline(t, dir, profileFor(t, dir), filepath.Join(dir, "coverage-baseline.txt"))
	if code == 0 {
		t.Fatalf("gate accepted a measurable package exception:\n%s", out)
	}
	if !strings.Contains(out, "has measurable statements") {
		t.Fatalf("gate did not explain the invalid exception:\n%s", out)
	}
}

func TestGateRejectsMalformedOrStaleExceptions(t *testing.T) {
	tests := map[string]struct {
		entry string
		want  string
	}{
		"missing rationale": {entry: "probe/internal/core/domain/dead\n", want: "missing an inline rationale"},
		"stale package":     {entry: "probe/internal/core/domain/missing # stale\n", want: "not a current coverage-tier package"},
		"out of scope":      {entry: "probe/internal/platform/thing # not a tier\n", want: "not a current coverage-tier package"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir := writeModule(t, map[string]string{
				"go.mod":                            probeGoMod,
				"internal/core/domain/dead/dead.go": "package dead\n\ntype T struct{ N int }\n",
				"internal/platform/thing/thing.go":  "package thing\n\nfunc Other() int { return 2 }\n",
				"coverage-baseline.txt":             tc.entry,
			})
			out, code := runGateWithBaseline(t, dir, profileFor(t, dir), filepath.Join(dir, "coverage-baseline.txt"))
			if code == 0 {
				t.Fatalf("gate accepted invalid exception:\n%s", out)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("gate did not report %q:\n%s", tc.want, out)
			}
		})
	}
}

func TestGateUsesSixtyPercentDefault(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": probeGoMod,
		"internal/core/domain/probe/probe.go": "package probe\n\n" +
			"func Covered() int { return 1 }\n" +
			"func Uncovered() int { return 2 }\n",
		"internal/core/domain/probe/probe_test.go": "package probe\n\nimport \"testing\"\n\n" +
			"func TestCovered(t *testing.T) {\n\tif Covered() != 1 {\n\t\tt.Fatal(\"Covered\")\n\t}\n}\n",
	})
	out, code := runGate(t, dir, profileFor(t, dir))
	if code == 0 {
		t.Fatalf("gate passed at 50%% coverage:\n%s", out)
	}
	if !strings.Contains(out, "(min 60%)") {
		t.Fatalf("gate did not report the 60%% default:\n%s", out)
	}
}

func TestGateDoesNotAllowThresholdOverride(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": probeGoMod,
		"internal/core/domain/probe/probe.go": "package probe\n\n" +
			"func Covered() int { return 1 }\n" +
			"func Uncovered() int { return 2 }\n",
		"internal/core/domain/probe/probe_test.go": "package probe\n\nimport \"testing\"\n\n" +
			"func TestCovered(t *testing.T) {\n\tif Covered() != 1 {\n\t\tt.Fatal(\"Covered\")\n\t}\n}\n",
	})
	out, code := runGateWithEnv(t, dir, profileFor(t, dir), filepath.Join(dir, "no-baseline.txt"), "CORE_MIN=-1")
	if code == 0 {
		t.Fatalf("gate allowed an environment override to disable policy:\n%s", out)
	}
	if !strings.Contains(out, "(min 60%)") {
		t.Fatalf("gate did not retain the fixed 60%% policy:\n%s", out)
	}
}

func TestGatePassesAtSixtyPercentBoundary(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": probeGoMod,
		"internal/core/domain/probe/probe.go": "package probe\n\n" +
			"func One() int { return 1 }\nfunc Two() int { return 2 }\nfunc Three() int { return 3 }\n" +
			"func Four() int { return 4 }\nfunc Five() int { return 5 }\n",
		"internal/core/domain/probe/probe_test.go": "package probe\n\nimport \"testing\"\n\n" +
			"func TestThree(t *testing.T) {\n\tif One()+Two()+Three() != 6 {\n\t\tt.Fatal(\"sum\")\n\t}\n}\n",
	})
	out, code := runGate(t, dir, profileFor(t, dir))
	if code != 0 {
		t.Fatalf("gate failed at exactly 60%%:\n%s", out)
	}
}

func TestGateAppliesSixtyPercentToStoreTier(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod": probeGoMod,
		"internal/admin/store/probe/probe.go": "package probe\n\n" +
			"func Covered() int { return 1 }\nfunc Uncovered() int { return 2 }\n",
		"internal/admin/store/probe/probe_test.go": "package probe\n\nimport \"testing\"\n\n" +
			"func TestCovered(t *testing.T) {\n\tif Covered() != 1 {\n\t\tt.Fatal(\"Covered\")\n\t}\n}\n",
	})
	out, code := runGate(t, dir, profileFor(t, dir))
	if code == 0 {
		t.Fatalf("store tier passed below 60%%:\n%s", out)
	}
	if !strings.Contains(out, "(min 60%)") {
		t.Fatalf("store tier did not use 60%%:\n%s", out)
	}
}

func TestGateFailsOnPartialProfile(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"go.mod":                               probeGoMod,
		"internal/core/domain/one/one.go":      "package one\n\nfunc Value() int { return 1 }\n",
		"internal/core/domain/one/one_test.go": "package one\n\nimport \"testing\"\nfunc TestValue(t *testing.T) { Value() }\n",
		"internal/core/domain/two/two.go":      "package two\n\nfunc Value() int { return 2 }\n",
		"internal/core/domain/two/two_test.go": "package two\n\nimport \"testing\"\nfunc TestValue(t *testing.T) { Value() }\n",
	})
	cmd := exec.Command("go", "test", "-coverprofile=cover.out", "./internal/core/domain/one")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go test in fixture: %v\n%s", err, out)
	}
	out, code := runGate(t, dir, filepath.Join(dir, "cover.out"))
	if code == 0 {
		t.Fatalf("gate passed a partial profile:\n%s", out)
	}
	if !strings.Contains(out, "profile stale?") {
		t.Fatalf("gate did not report the omitted package:\n%s", out)
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
