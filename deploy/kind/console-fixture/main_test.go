package main

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"orbitjob/internal/core/app/execution"
)

func TestGuardRefusesUnboundedOrDifferentInstallations(t *testing.T) {
	valid := options{context: fixtureContext, namespace: fixtureNamespace, timeout: time.Minute}
	if err := validateGuard(valid, "1", "/private/test-kubeconfig"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		guard, kube string
		change      func(*options)
	}{
		{"", "/private/test-kubeconfig", func(*options) {}},
		{"1", "", func(*options) {}},
		{"1", "/one:/two", func(*options) {}},
		{"1", "/private/test-kubeconfig", func(o *options) { o.context = "kind-orbitjob-dev" }},
		{"1", "/private/test-kubeconfig", func(o *options) { o.namespace = "default" }},
		{"1", "/private/test-kubeconfig", func(o *options) { o.timeout = 0 }},
	} {
		o := valid
		tc.change(&o)
		if validateGuard(o, tc.guard, tc.kube) == nil {
			t.Fatalf("unsafe guard accepted: %+v", o)
		}
	}
}

func TestRunChecksGuardBeforeOpeningExternalConnections(t *testing.T) {
	t.Setenv("ORBITJOB_CONSOLE_FIXTURE", "")
	t.Setenv("KUBECONFIG", "/does-not-exist")
	if err := run([]string{"status"}); err == nil || !strings.Contains(err.Error(), "ORBITJOB_CONSOLE_FIXTURE") {
		t.Fatal("missing guard did not stop before connection setup")
	}
}

func TestAdminDSNIsBoundToForwardAndDoesNotLeakCredentials(t *testing.T) {
	secret := "postgres://orbitjob_admin:test-password@orbitjob-postgres.orbitjob.svc:5432/orbitjob?host=outside&user=postgres&sslmode=require"
	local, err := localAdminDSN(secret, 15439)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(local)
	if u.Host != "127.0.0.1:15439" || u.User.Username() != "orbitjob_admin" || u.Query().Get("host") != "" || u.Query().Get("user") != "" {
		t.Fatal("secret query options escaped the fixture port-forward")
	}
	for _, candidate := range []string{
		strings.Replace(secret, "orbitjob-postgres.orbitjob.svc", "production.example", 1),
		strings.Replace(secret, "orbitjob_admin:", "postgres:", 1),
		strings.Replace(secret, "/orbitjob?", "/production?", 1),
		"postgres://orbitjob_admin:test-password@%not-a-host",
	} {
		_, err := localAdminDSN(candidate, 15439)
		if err == nil || strings.Contains(err.Error(), "test-password") {
			t.Fatal("unsafe or credential-bearing DSN error")
		}
	}
}

func TestKindConfigRequiresVerifiedLocalEndpoint(t *testing.T) {
	makeConfig := func(server string, insecure bool) []byte {
		data, _ := json.Marshal(map[string]any{"current-context": fixtureContext,
			"clusters": []any{map[string]any{"cluster": map[string]any{"server": server, "certificate-authority-data": []byte("ca"), "insecure-skip-tls-verify": insecure}}},
			"users":    []any{map[string]any{"user": map[string]any{"client-certificate-data": []byte("cert"), "client-key-data": []byte("key")}}}})
		return data
	}
	if _, err := fixtureClientConfig(makeConfig("https://127.0.0.1:1234", false)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{makeConfig("https://remote.example:1234", false), makeConfig("https://127.0.0.1:1234", true), makeConfig("http://127.0.0.1:1234", false)} {
		if _, err := fixtureClientConfig(raw); err == nil {
			t.Fatal("unverified endpoint accepted")
		}
	}
}

func TestCleanupCannotFollowForeignIDsOrRecreatedResources(t *testing.T) {
	owned := map[string]map[int64]bool{"function-11": {4: true}}
	m := &manifest{Runs: []ownedRun{{Name: "function-11-abcd1234", UID: "original-uid", Revision: 4}}}
	if err := verifyRememberedRuns(m, owned); err != nil {
		t.Fatal(err)
	}
	foreign := &manifest{Runs: []ownedRun{{Name: "function-22-abcd1234", UID: "foreign", Revision: 4}}}
	if verifyRememberedRuns(foreign, owned) == nil {
		t.Fatal("foreign manifest run accepted")
	}
	obj := unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"namespace": fixtureNamespace, "uid": "original-uid"}, "spec": map[string]any{"scheduledJobRef": map[string]any{"uid": "function-11"}, "definitionRevision": int64(99)}}}
	if _, err := matchesRun(obj, owned); err == nil {
		t.Fatal("foreign revision accepted for owned Function")
	}
	labels := map[string]string{execution.LabelRunName: "function-11-abcd1234", execution.LabelRunUID: "new-uid", execution.LabelRevisionID: "4"}
	if ours, err := matchesExecution(labels, m); ours || err != nil {
		t.Fatal("recreated execution adopted by name")
	}
	labels[execution.LabelRunUID] = "original-uid"
	labels[execution.LabelRevisionID] = "99"
	if _, err := matchesExecution(labels, m); err == nil {
		t.Fatal("mismatched ownership labels accepted")
	}
}

func TestRetryFixtureActuallyFailsFirstAttemptAndSucceedsSecond(t *testing.T) {
	command, ok := caseCommand("retry")
	if !ok {
		t.Fatal("retry fixture missing")
	}
	for _, tc := range []struct {
		hostname string
		success  bool
	}{
		{"oj-function-11-abcd1234-1-podhash", false}, {"oj-function-11-abcd1234-2-podhash", true},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, "sh", "-c", command)
		cmd.Env = append(os.Environ(), "HOSTNAME="+tc.hostname)
		err := cmd.Run()
		cancel()
		if (err == nil) != tc.success {
			t.Fatalf("hostname %s: %v", tc.hostname, err)
		}
	}
}
