package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestObservabilityRequiresOwnDashboardAndValidLiveEvidence(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, configmap, grafana string
		wantSuccess              bool
	}{
		{"provisioned and loaded", "valid", "200", true},
		{"no Grafana port forward", "valid", "down", true},
		{"missing own dashboard", "missing", "down", false},
		{"unrelated dashboard", "unrelated", "down", false},
		{"missing sidecar label", "unlabeled", "down", false},
		{"malformed payload", "malformed", "down", false},
		{"wrong payload UID", "wrong-uid", "down", false},
		{"reachable Grafana lacks dashboard", "valid", "404", false},
		{"reachable Grafana failure", "valid", "500", false},
		{"wrong live dashboard", "valid", "wrong-uid", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			// A path can contain these letters without requesting curl -f.
			// Keep this deterministic instead of depending on mktemp's suffix.
			tmpDir := filepath.Join(dir, "fixture-fS")
			if err := os.Mkdir(tmpDir, 0o700); err != nil {
				t.Fatal(err)
			}
			payload := `{"uid":"orbitjob-run-ledger"}`
			name, label := "orbitjob-run-ledger-dashboard", "1"
			switch tc.configmap {
			case "unrelated":
				name = "unrelated-dashboard"
			case "unlabeled":
				label = "0"
			case "malformed":
				payload = "{"
			case "wrong-uid":
				payload = `{"uid":"other"}`
			}
			configmap, err := json.Marshal(map[string]any{
				"metadata": map[string]any{"name": name, "labels": map[string]string{"grafana_dashboard": label}},
				"data":     map[string]string{"orbitjob-run-ledger.json": payload},
			})
			if err != nil {
				t.Fatal(err)
			}
			cmPath := filepath.Join(dir, "dashboard.json")
			if err := os.WriteFile(cmPath, configmap, 0o600); err != nil {
				t.Fatal(err)
			}
			for tool, body := range map[string]string{
				"helm": "#!/bin/sh\nexit 0\n",
				"kubectl": `#!/bin/sh
case " $* " in
  *" get pvc "*) echo kube-prometheus-stack-prometheus-db ;;
  *" get configmap orbitjob-run-ledger-dashboard "*)
    [ "$CM_MODE" != missing ] || exit 1
    cat "$CM_FILE" ;;
  *" get configmap "*) echo 'unrelated-dashboard 1 1d' ;;
  *) exit 1 ;;
esac
`,
				"python3": `#!/bin/sh
source=$(cat)
case "$source" in
  *"config = get("*|*"/api/v1/rules"*) exit 0 ;;
esac
printf '%s\n' "$source" | "$REAL_PYTHON" "$@"
`,
				"curl": `#!/bin/sh
output= code_requested=false fail_http=false url=
for arg in "$@"; do
  case "$arg" in -f*|-[!-]*f*|--fail|--fail-with-body) fail_http=true ;; esac
done
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) output=$2; shift 2 ;;
    -w) code_requested=true; shift 2 ;;
    *) url=$1; shift ;;
  esac
done
case "$url" in
  */-/ready) exit 0 ;;
  */api/dashboards/uid/*) ;;
  *) exit 1 ;;
esac
[ "$GRAFANA_MODE" != down ] || exit 7
code=$GRAFANA_MODE
body='{"dashboard":{"uid":"orbitjob-run-ledger"}}'
if [ "$code" = wrong-uid ]; then code=200; body='{"dashboard":{"uid":"other"}}'; fi
if [ -n "$output" ]; then printf '%s' "$body" > "$output"; else printf '%s' "$body"; fi
if [ "$code_requested" = true ]; then printf '%s' "$code"; fi
if [ "$fail_http" = true ] && [ "$code" -ge 400 ]; then exit 22; fi
`,
			} {
				writeGateExecutable(t, filepath.Join(dir, "fakebin", tool), body)
			}
			script, err := filepath.Abs("observability-status.sh")
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", script)
			cmd.Dir = dir
			cmd.Env = append(gateEnvironment("PATH", "CM_MODE", "CM_FILE", "GRAFANA_MODE", "REAL_PYTHON", "TMPDIR"),
				"PATH="+filepath.Join(dir, "fakebin")+":"+os.Getenv("PATH"), "CM_MODE="+tc.configmap,
				"CM_FILE="+cmPath, "GRAFANA_MODE="+tc.grafana, "REAL_PYTHON="+python, "TMPDIR="+tmpDir)
			out, err := cmd.CombinedOutput()
			if (err == nil) != tc.wantSuccess {
				t.Fatalf("success=%v, want %v:\n%s", err == nil, tc.wantSuccess, out)
			}
			if tc.grafana == "down" && tc.wantSuccess && !strings.Contains(string(out), "cannot confirm") {
				t.Fatalf("unavailable Grafana was not distinguished from confirmed load:\n%s", out)
			}
		})
	}
}
