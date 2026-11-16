package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDatabaseVolumeUpgradeReportsRecoveryFailures(t *testing.T) {
	for _, tc := range []struct {
		name, failure         string
		wantCode              int
		wantClaim, diagnostic string
	}{
		{"success", "", 0, "new", "[OK] active database claim:"},
		{"operator scale restore", "operator-scale", 1, "new", "restore deployment/orbitjob-operator replicas to 3"},
		{"operator rollout restore", "operator-rollout", 1, "new", "wait for deployment/orbitjob-operator recovery"},
		{"database rollout restore", "database-rollout", 1, "new", "wait for PostgreSQL recovery"},
		{"database scale restore", "database-scale", 1, "new", "restore PostgreSQL replicas to 2"},
		{"archive rollout restore", "archive-rollout", 1, "new", "wait for statefulset/orbitjob-log-archive recovery"},
		{"copy failure", "copy", 36, "original-database", "copy not accepted"},
		{"database check failure", "database-check", 37, "original-database", "copy not accepted"},
		{"original failure and failed restore", "database-check-and-operator-scale", 37, "original-database", "recovery incomplete"},
		{"rollback failure", "database-check-and-rollback", 37, "new", "restore database claim original-database"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			fakebin := filepath.Join(dir, "bin")
			writeGateExecutable(t, filepath.Join(fakebin, "kubectl"), volumeUpgradeKubectl)
			replicas := map[string]int{"deployment/orbitjob-postgres": 2, "deployment/orbitjob-admin-api": 2, "deployment/orbitjob-scheduler": 1, "deployment/orbitjob-operator": 3, "statefulset/orbitjob-log-archive": 0}
			initial, err := json.Marshal(map[string]any{"claim": "original-database", "replicas": replicas, "saved": replicas, "failure": tc.failure, "calls": []any{}, "rollouts": map[string]int{}})
			if err != nil {
				t.Fatal(err)
			}
			statePath := filepath.Join(dir, "state.json")
			if err := os.WriteFile(statePath, initial, 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", "upgrade-local-db-volume.sh")
			cmd.Env = append(gateEnvironment("PATH", "VOLUME_UPGRADE_STATE"), "PATH="+fakebin+":"+os.Getenv("PATH"), "VOLUME_UPGRADE_STATE="+statePath)
			out, runErr := cmd.CombinedOutput()
			code := 0
			if runErr != nil {
				var exit *exec.ExitError
				if !errors.As(runErr, &exit) {
					t.Fatal(runErr)
				}
				code = exit.ExitCode()
			}
			if code != tc.wantCode || !strings.Contains(string(out), tc.diagnostic) {
				t.Fatalf("exit=%d want=%d diagnostic=%q\n%s", code, tc.wantCode, tc.diagnostic, out)
			}
			if (tc.wantCode == 0) != strings.Contains(string(out), "[OK] active database claim:") {
				t.Fatalf("incorrect completion result:\n%s", out)
			}
			data, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			var state struct {
				Claim    string
				Replicas map[string]int
				Calls    [][]string
				Rollouts map[string]int
			}
			if err := json.Unmarshal(data, &state); err != nil {
				t.Fatal(err)
			}
			if tc.wantClaim == "new" {
				if !strings.HasPrefix(state.Claim, "orbitjob-postgres-logs-") {
					t.Fatalf("claim=%s", state.Claim)
				}
			} else if state.Claim != tc.wantClaim {
				t.Fatalf("claim=%s want=%s", state.Claim, tc.wantClaim)
			}
			wantReplicas := replicas
			if strings.Contains(tc.failure, "operator-scale") {
				wantReplicas["deployment/orbitjob-operator"] = 0
			}
			if tc.failure == "database-scale" {
				wantReplicas["deployment/orbitjob-postgres"] = 1
			}
			if !reflect.DeepEqual(state.Replicas, wantReplicas) {
				t.Fatalf("replicas=%v want=%v", state.Replicas, wantReplicas)
			}
			for target := range replicas {
				if state.Rollouts[target] == 0 {
					t.Fatalf("recovery did not observe %s rollout", target)
				}
			}
			for _, call := range state.Calls {
				if len(call) < 2 || call[0] != "--context" || call[1] != "kind-orbitjob-dev" {
					t.Fatalf("unexpected context: %v", call)
				}
				if strings.Contains(strings.Join(call, " "), "delete pvc") {
					t.Fatalf("upgrade deleted a retained claim: %v", call)
				}
			}
		})
	}
}

const volumeUpgradeKubectl = `#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
p = Path(os.environ['VOLUME_UPGRADE_STATE'])
s = json.loads(p.read_text())
a = sys.argv[1:]
s['calls'].append(a)
failure = s['failure']
def done(code=0, output=''):
    p.write_text(json.dumps(s))
    if output: print(output, end='')
    if code: print('injected kubectl failure', file=sys.stderr)
    sys.exit(code)
if a[:2] != ['--context', 'kind-orbitjob-dev']: done(99)
if 'get' in a:
    i = a.index('get')
    target = a[i+1]
    if target == 'deployment':
        done(output=str(s['replicas']['deployment/orbitjob-postgres']) if '.spec.replicas' in a[-1] else s['claim'])
    if target == 'pvc': done(output='standard' if 'storageClassName' in a[-1] else '2Gi')
    if target == 'jobruns': done(output='{"items":[]}')
    if target == 'pod': done(output='Succeeded')
    if target in s['replicas']: done(output=str(s['replicas'][target]))
    done(99)
if 'scale' in a:
    target = a[a.index('scale')+1]
    count = int(next(x.split('=')[1] for x in a if x.startswith('--replicas=')))
    if 'operator-scale' in failure and target == 'deployment/orbitjob-operator' and count == s['saved'][target]: done(42)
    if failure == 'database-scale' and target == 'deployment/orbitjob-postgres' and count == s['saved'][target]: done(46)
    s['replicas'][target] = count
elif 'rollout' in a:
    target = a[a.index('status')+1]
    s['rollouts'][target] = s['rollouts'].get(target, 0)+1
    if failure == 'operator-rollout' and target == 'deployment/orbitjob-operator': done(43)
    if failure == 'archive-rollout' and target == 'statefulset/orbitjob-log-archive': done(47)
    if failure == 'database-rollout' and target == 'deployment/orbitjob-postgres' and s['rollouts'][target] > 1: done(44)
elif 'patch' in a:
    claim = json.loads(a[a.index('-p')+1])[0]['value']
    if failure == 'database-check-and-rollback' and claim == 'original-database': done(45)
    s['claim'] = claim
elif 'exec' in a:
    if failure.startswith('database-check'): done(37)
elif 'apply' in a:
    sys.stdin.read()
    if failure == 'copy': done(36)
elif not any(x in a for x in ['wait', 'delete']): done(99)
done()
`
