package main

import (
	"context"
	"strings"
	"testing"
)

func TestStatusCannotReportZeroRestartsWithoutPodEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, pods string
		wantOK     bool
		wantCount  string
	}{
		{"real zero", podsJSON(0), true, "0 restarts"},
		{"observed restarts", podsJSON(3), true, "3 restarts"},
		{"list denied", "", false, ""},
		{"malformed JSON", "{", false, ""},
		{"null response", "null", false, ""},
		{"missing list", `{}`, false, ""},
		{"non-list items", `{"items":{}}`, false, ""},
		{"empty list", `{"items":[]}`, false, ""},
		{"null pod", `{"items":[null]}`, false, ""},
		{"missing statuses", `{"items":[{"status":{}}]}`, false, ""},
		{"missing counter", `{"items":[{"status":{"containerStatuses":[{}]}}]}`, false, ""},
		{"string counter", `{"items":[{"status":{"containerStatuses":[{"restartCount":"0"}]}}]}`, false, ""},
		{"negative counter", `{"items":[{"status":{"containerStatuses":[{"restartCount":-1}]}}]}`, false, ""},
		{"fractional counter", `{"items":[{"status":{"containerStatuses":[{"restartCount":0.5}]}}]}`, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kube := healthyFakeKube()
			key := "get pods -n orbitjob-system -l app.kubernetes.io/name=orbitjob-operator"
			if tc.pods == "" {
				delete(kube.commands, key)
			} else {
				kube.commands[key] = tc.pods
			}
			lines, failed := inspectInstall(context.Background(), kube, statusFlags{namespace: "orbitjob-system", release: "orbitjob", monitoringNS: "monitoring"})
			if failed == tc.wantOK {
				t.Fatalf("wantOK=%v failed=%v:\n%s", tc.wantOK, failed, renderLines(lines))
			}
			for _, line := range lines {
				if !strings.HasPrefix(line.Detail, "deployment orbitjob-operator:") {
					continue
				}
				if tc.wantOK {
					if line.Failed || line.Status != "[OK]" || !strings.Contains(line.Detail, tc.wantCount) {
						t.Fatalf("observed pod evidence misreported: %+v", line)
					}
				} else if !line.Failed || line.Status != "[FAIL]" || !strings.Contains(line.Detail, "inspection failed") || strings.Contains(line.Detail, "0 restarts") {
					t.Fatalf("missing pod evidence misreported: %+v", line)
				}
			}
		})
	}
}

func TestStatusJSONFailsWhenOnlyPodListIsUnreadable(t *testing.T) {
	kube := healthyFakeKube()
	delete(kube.commands, "get pods -n orbitjob-system -l app.kubernetes.io/name=orbitjob-operator")
	withFakeKube(t, func() kubeRunner { return kube })
	out := captureStdout(t, func() {
		if code := run(context.Background(), []string{"status", "--json"}); code != exitGeneral {
			t.Fatalf("status exit=%d, want %d", code, exitGeneral)
		}
	})
	if !strings.Contains(out, `"status": "[FAIL]"`) || strings.Contains(out, "orbitjob-operator: 1/1 ready, 0 restarts") {
		t.Fatalf("JSON status claimed healthy inspection:\n%s", out)
	}
}
