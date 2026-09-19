package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// fakeKube answers kubectl invocations from a scripted map: full command
// prefix -> stdout. The special value "NOTFOUND" scripts a not-found failure,
// the way kubectl exits for a missing object. Anything unscripted fails
// loudly so a test cannot pass by accident.
type fakeKube struct {
	commands map[string]string
}

func (f *fakeKube) run(_ context.Context, args ...string) ([]byte, error) {
	for prefix, out := range f.commands {
		if hasPrefixArgs(args, strings.Fields(prefix)) {
			if out == "NOTFOUND" {
				return nil, fmt.Errorf("Error from server (NotFound): %s not found", strings.Join(args, " "))
			}
			return []byte(out), nil
		}
	}
	return nil, fmt.Errorf("kubectl %s: unexpected call in test", strings.Join(args, " "))
}

func hasPrefixArgs(args, prefix []string) bool {
	if len(prefix) > len(args) {
		return false
	}
	for i, p := range prefix {
		if args[i] != p {
			return false
		}
	}
	return true
}

func newFakeKube() *fakeKube {
	return &fakeKube{commands: map[string]string{}}
}

// healthyFakeKube scripts the full happy path: release rev 13, all three
// deployments ready with no restarts, schema 13, both core CRDs, monitoring
// present.
func healthyFakeKube() *fakeKube {
	f := newFakeKube()
	f.commands["get secrets -n orbitjob-system -l owner=helm,name=orbitjob"] = helmSecrets(13)
	for _, c := range installComponents {
		f.commands["get deployment "+c+" -n orbitjob-system"] = deploymentJSON(1, 1)
		f.commands["get pods -n orbitjob-system -l app.kubernetes.io/name="+c] = podsJSON(0)
	}
	f.commands["get configmap orbitjob-migrations -n orbitjob-system"] = migrationsJSON()
	f.commands["get crd jobruns.workloads.orbitjob.io"] = "customresourcedefinition.apiextensions.k8s.io/jobruns.workloads.orbitjob.io\n"
	f.commands["get crd scheduledjobs.workloads.orbitjob.io"] = "customresourcedefinition.apiextensions.k8s.io/scheduledjobs.workloads.orbitjob.io\n"
	f.commands["get secrets -n monitoring -l owner=helm,name=kube-prometheus-stack"] = helmSecrets(2)
	return f
}

func helmSecrets(rev int) string {
	return fmt.Sprintf(`{"items":[{"metadata":{"name":"sh.helm.release.v1.orbitjob.v%d"}}]}`, rev)
}

func deploymentJSON(desired, ready int) string {
	return fmt.Sprintf(`{"spec":{"replicas":%d},"status":{"readyReplicas":%d,"replicas":%d}}`, desired, ready, desired)
}

func podsJSON(restarts int) string {
	return fmt.Sprintf(`{"items":[{"status":{"containerStatuses":[{"restartCount":%d,"name":"main"}]}}]}`, restarts)
}

func migrationsJSON() string {
	return `{"data":{"0001_baseline.up.sql":"-- baseline","0002_kubernetes_job_control_plane_foundations.up.sql":"-- x","0003_nullable_resource_group_id.up.sql":"-- y"}}`
}

func TestStatusHappyPath(t *testing.T) {
	kube := healthyFakeKube()
	lines, failed := inspectInstall(context.Background(), kube, statusFlags{namespace: "orbitjob-system", release: "orbitjob", monitoringNS: "monitoring"})
	if failed {
		t.Fatalf("healthy install reported failures: %+v", lines)
	}
	joined := renderLines(lines)
	for _, want := range []string{
		"helm release orbitjob revision 13 in namespace orbitjob-system",
		"deployment orbitjob-admin-api: 1/1 ready, 0 restarts",
		"deployment orbitjob-operator: 1/1 ready, 0 restarts",
		"deployment orbitjob-scheduler: 1/1 ready, 0 restarts",
		"schema migrations shipped: version 3",
		"crd jobruns.workloads.orbitjob.io: present",
		"monitoring release kube-prometheus-stack present in namespace monitoring",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("status missing %q in:\n%s", want, joined)
		}
	}
}

func renderLines(lines []CheckLine) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.Status)
		b.WriteString(" ")
		b.WriteString(l.Detail)
		b.WriteString("\n")
	}
	return b.String()
}

func TestStatusFailuresAndWarnings(t *testing.T) {
	kube := newFakeKube()
	kube.commands["get secrets -n orbitjob-system -l owner=helm,name=orbitjob"] = helmSecrets(7)
	// operator deployment not ready and restarting
	kube.commands["get deployment orbitjob-operator -n orbitjob-system"] = deploymentJSON(1, 0)
	kube.commands["get pods -n orbitjob-system -l app.kubernetes.io/name=orbitjob-operator"] = podsJSON(3)
	kube.commands["get deployment orbitjob-admin-api -n orbitjob-system"] = deploymentJSON(1, 1)
	kube.commands["get pods -n orbitjob-system -l app.kubernetes.io/name=orbitjob-admin-api"] = podsJSON(0)
	kube.commands["get deployment orbitjob-scheduler -n orbitjob-system"] = deploymentJSON(1, 1)
	kube.commands["get pods -n orbitjob-system -l app.kubernetes.io/name=orbitjob-scheduler"] = podsJSON(0)
	// schema configmap missing -> FAIL
	kube.commands["get configmap orbitjob-migrations -n orbitjob-system"] = "NOTFOUND"
	// jobruns CRD missing -> FAIL; scheduledjobs present
	kube.commands["get crd jobruns.workloads.orbitjob.io"] = "NOTFOUND"
	kube.commands["get crd scheduledjobs.workloads.orbitjob.io"] = "customresourcedefinition.apiextensions.k8s.io/scheduledjobs.workloads.orbitjob.io\n"
	// workflow CRDs and monitoring missing -> WARN only
	kube.commands["get crd workflowjobs.workloads.orbitjob.io"] = "NOTFOUND"
	kube.commands["get crd workflowruns.workloads.orbitjob.io"] = "NOTFOUND"
	kube.commands["get secrets -n monitoring -l owner=helm,name=kube-prometheus-stack"] = "NOTFOUND"

	lines, failed := inspectInstall(context.Background(), kube, statusFlags{namespace: "orbitjob-system", release: "orbitjob", monitoringNS: "monitoring"})
	if !failed {
		t.Fatalf("broken install reported no failure:\n%s", renderLines(lines))
	}
	joined := renderLines(lines)
	if !strings.Contains(joined, "[FAIL] deployment orbitjob-operator: 0/1 ready, 3 restarts") {
		t.Fatalf("operator failure line wrong:\n%s", joined)
	}
	if !strings.Contains(joined, "[FAIL] schema migrations") {
		t.Fatalf("missing schema must fail:\n%s", joined)
	}
	if !strings.Contains(joined, "[FAIL] crd jobruns.workloads.orbitjob.io: missing") {
		t.Fatalf("missing core CRD must fail:\n%s", joined)
	}
	if !strings.Contains(joined, "WARN monitoring release") {
		t.Fatalf("missing monitoring must be a WARN:\n%s", joined)
	}
	if !strings.Contains(joined, "helm release orbitjob revision 7") {
		t.Fatalf("release line wrong:\n%s", joined)
	}
}

func TestStatusCommandJSONAndExit(t *testing.T) {
	withFakeKube(t, func() kubeRunner { return healthyFakeKube() })

	out := captureStdout(t, func() {
		if err := statusCommand(context.Background(), nil); err != nil {
			t.Errorf("status: %v", err)
		}
	})
	if !strings.Contains(out, "[OK] helm release orbitjob revision 13") {
		t.Fatalf("status command output wrong:\n%s", out)
	}
}

// TestStatusCommandFailsBrokenInstall pins the command-level exit.
func TestStatusCommandFailsBrokenInstall(t *testing.T) {
	withFakeKube(t, func() kubeRunner { return newFakeKube() }) // everything unscripted -> errors

	out := captureStdout(t, func() {
		err := statusCommand(context.Background(), nil)
		if got := exitCodeOfErr(t, err); got != exitGeneral {
			t.Errorf("all-kubectl-failing status exit = %d, want %d", got, exitGeneral)
		}
	})
	if !strings.Contains(out, "[FAIL]") {
		t.Fatalf("broken install must print [FAIL] lines, got:\n%s", out)
	}
}

func TestShippedSchemaVersionParsing(t *testing.T) {
	v, ok := shippedSchemaVersion(context.Background(), healthyFakeKube(), "orbitjob-system", "orbitjob")
	if !ok || v != 3 {
		t.Fatalf("schema version = %d, %v; want 3, true", v, ok)
	}
}

func TestHelmReleaseRevisionTakesHighest(t *testing.T) {
	f := newFakeKube()
	f.commands["get secrets -n orbitjob-system -l owner=helm,name=orbitjob"] =
		`{"items":[{"metadata":{"name":"sh.helm.release.v1.orbitjob.v3"}},{"metadata":{"name":"sh.helm.release.v1.orbitjob.v13"}},{"metadata":{"name":"other-secret"}}]}`
	rev, ok := helmReleaseRevision(context.Background(), f, "orbitjob-system", "orbitjob")
	if !ok || rev != 13 {
		t.Fatalf("revision = %d, %v; want 13, true", rev, ok)
	}
}
