package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// doctorTestKube scripts the cluster side of doctor for a healthy install,
// including the leader lease and can-i answers.
func doctorTestKube(workflowCRDs bool) *fakeKube {
	f := healthyFakeKube()
	f.commands["get deployment orbitjob-operator -n orbitjob-system"] =
		`{"spec":{"replicas":1,"template":{"spec":{"containers":[{"name":"operator","env":[{"name":"OPERATOR_NAMESPACE_TENANTS","value":"team-a=tenant-a"}]}]}}},"status":{"readyReplicas":1,"replicas":1}}`
	f.commands["get lease orbitjob-operator-singleton -n orbitjob-system"] =
		`{"spec":{"holderIdentity":"orbitjob-operator-7f4954869d-rrj4j"}}`
	f.commands["get secret bootstrap-api-key -n orbitjob-system"] =
		`{"data":{"api-key":"ZHVtbXk="}}` // decodes to dummy
	f.commands["auth can-i create jobruns.workloads.orbitjob.io -n team-a"] = "yes\n"
	f.commands["auth can-i list jobruns.workloads.orbitjob.io -n team-a"] = "yes\n"
	if workflowCRDs {
		f.commands["get crd workflowjobs.workloads.orbitjob.io"] = "customresourcedefinition.apiextensions.k8s.io/workflowjobs.workloads.orbitjob.io\n"
		f.commands["get crd workflowruns.workloads.orbitjob.io"] = "customresourcedefinition.apiextensions.k8s.io/workflowruns.workloads.orbitjob.io\n"
		f.commands["auth can-i list workflowjobs.workloads.orbitjob.io -n team-a"] = "yes\n"
		f.commands["auth can-i list workflowruns.workloads.orbitjob.io -n team-a"] = "yes\n"
	} else {
		f.commands["get crd workflowjobs.workloads.orbitjob.io"] = "NOTFOUND"
		f.commands["get crd workflowruns.workloads.orbitjob.io"] = "NOTFOUND"
	}
	return f
}

func withFakeKube(t *testing.T, kube func() kubeRunner) {
	t.Helper()
	restore := newKubeRunner
	newKubeRunner = kube
	t.Cleanup(func() { newKubeRunner = restore })
}

// TestDoctorHappyPathWithoutEnvKey proves the secret fallback: no flag and no
// env key, yet the API check succeeds through the bootstrap secret.
func TestDoctorHappyPathWithoutEnvKey(t *testing.T) {
	t.Setenv("ORBITJOB_API_KEY", "")
	t.Setenv("ORBITJOB_API_URL", "")
	t.Setenv("ORBITJOB_API", "")
	withFakeKube(t, func() kubeRunner { return doctorTestKube(false) })

	var gotKey string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("Authorization")
		writeItems(t, w, []map[string]any{{"id": "01ABCDEFGHJKLMNPQRSTVWXYZ", "slug": "acme", "name": "Acme", "status": "active"}})
	}))
	defer api.Close()

	out := captureStdout(t, func() {
		err := doctorCommand(context.Background(), []string{"--api-url", api.URL})
		if err != nil {
			t.Errorf("doctor: %v", err)
		}
	})
	if gotKey != "Bearer dummy" {
		t.Fatalf("doctor did not use the secret key: %q", gotKey)
	}
	joined := out
	for _, want := range []string{
		"[OK] API key: resolved from cluster secret bootstrap-api-key",
		"[OK] admin API: /api/v1/tenants 200 (1 tenants)",
		"[OK] leader lease orbitjob-operator-singleton: held by orbitjob-operator-7f4954869d-rrj4j",
		"[OK] tenant namespaces: discovered team-a",
		"[OK] RBAC team-a: can-i create jobruns.workloads.orbitjob.io",
		"WARN RBAC team-a: workflowruns.workloads.orbitjob.io not installed, can-i skipped",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("doctor output missing %q in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "dummy") {
		t.Fatalf("doctor printed the key:\n%s", joined)
	}
}

// TestDoctorReportsForbiddenButClusterStillChecked pins two behaviors at once:
// an API-level 403 is a [FAIL] (exit 1), and the cluster checks still run so
// one broken side does not hide the other.
func TestDoctorReportsForbiddenButClusterStillChecked(t *testing.T) {
	t.Setenv("ORBITJOB_API_KEY", "")
	t.Setenv("ORBITJOB_API_URL", "")
	t.Setenv("ORBITJOB_API", "")
	withFakeKube(t, func() kubeRunner { return doctorTestKube(false) })

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"FORBIDDEN","message":"policy denies tenant list"}}`))
	}))
	defer api.Close()

	err := doctorCommand(context.Background(), []string{"--api-url", api.URL})
	if got := exitCodeOfErr(t, err); got != exitGeneral {
		t.Fatalf("doctor with API 403 exit = %d, want %d", got, exitGeneral)
	}
}

// TestDoctorToleratesWarn pins that a WARN-only run exits 0.
func TestDoctorToleratesWarn(t *testing.T) {
	t.Setenv("ORBITJOB_API_KEY", "otj_k")
	t.Setenv("ORBITJOB_API_URL", "")
	t.Setenv("ORBITJOB_API", "")
	withFakeKube(t, func() kubeRunner { return doctorTestKube(false) })

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeItems(t, w, nil)
	}))
	defer api.Close()

	err := doctorCommand(context.Background(), []string{"--api-url", api.URL})
	if err != nil {
		t.Fatalf("doctor with only WARNs exited %v, want nil", err)
	}
}

// TestDoctorSecretUnreadableFailsClearly covers the case where neither flag
// nor env carries the key and the secret lookup fails.
func TestDoctorSecretUnreadableFailsClearly(t *testing.T) {
	t.Setenv("ORBITJOB_API_KEY", "")
	withFakeKube(t, func() kubeRunner { return newFakeKube() }) // nothing scripted

	out := captureStdout(t, func() {
		err := doctorCommand(context.Background(), nil)
		if got := exitCodeOfErr(t, err); got != exitGeneral {
			t.Errorf("doctor without any key exit = %d, want %d", got, exitGeneral)
		}
	})
	if !strings.Contains(out, "[FAIL] API key") {
		t.Fatalf("doctor should fail the key line clearly:\n%s", out)
	}
}

// TestDoctorReportsDeniedCanI covers the RBAC failure path.
func TestDoctorReportsDeniedCanI(t *testing.T) {
	t.Setenv("ORBITJOB_API_KEY", "otj_k")
	f := doctorTestKube(false)
	f.commands["auth can-i create jobruns.workloads.orbitjob.io -n team-a"] = "no\n"
	withFakeKube(t, func() kubeRunner { return f })

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeItems(t, w, nil)
	}))
	defer api.Close()

	out := captureStdout(t, func() {
		err := doctorCommand(context.Background(), []string{"--api-url", api.URL})
		if got := exitCodeOfErr(t, err); got != exitGeneral {
			t.Errorf("doctor with denied can-i exit = %d, want %d", got, exitGeneral)
		}
	})
	if !strings.Contains(out, "[FAIL] RBAC team-a: can-i create jobruns.workloads.orbitjob.io = no") {
		t.Fatalf("denied can-i not reported:\n%s", out)
	}
}

// TestDoctorJSONMode emits the structured report.
func TestDoctorJSONMode(t *testing.T) {
	t.Setenv("ORBITJOB_API_KEY", "otj_k")
	withFakeKube(t, func() kubeRunner { return doctorTestKube(true) })

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeItems(t, w, nil)
	}))
	defer api.Close()

	out := captureStdout(t, func() {
		err := doctorCommand(context.Background(), []string{"--api-url", api.URL, "--json"})
		if err != nil {
			t.Errorf("doctor: %v", err)
		}
	})
	if !strings.Contains(out, `"status"`) || !strings.Contains(out, `"detail"`) {
		t.Fatalf("doctor --json output wrong:\n%s", out)
	}
	if strings.Contains(out, "dummy") {
		t.Fatalf("doctor --json printed the key:\n%s", out)
	}
}
