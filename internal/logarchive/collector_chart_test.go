package logarchive

import (
	"os/exec"
	"strings"
	"testing"
)

func TestCollectorChartPersistenceAndQuotaPrerequisites(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("Helm is required for collector chart validation")
	}
	for _, tc := range []struct {
		name   string
		values []string
		fail   bool
		want   string
	}{
		{"default node state", nil, false, "/var/lib/orbitjob-collectors/own/collector-check"},
		{"development ephemeral", []string{"logs.collectorPersistence.mode=ephemeral"}, false, "emptyDir:"},
		{"production undeclared quota", []string{"logs.collectorPersistence.profile=production"}, true, "administrator-provisioned directory quota"},
		{"production ephemeral", []string{"logs.collectorPersistence.profile=production", "logs.collectorPersistence.quotaVerified=true", "logs.collectorPersistence.mode=ephemeral"}, true, "administrator-provisioned directory quota"},
		{"production declared quota", []string{"logs.collectorPersistence.profile=production", "logs.collectorPersistence.quotaVerified=true"}, false, "administrator-declared"},
		{"production insufficient quota", []string{"logs.collectorPersistence.profile=production", "logs.collectorPersistence.quotaVerified=true", "logs.collectorPersistence.quotaBytes=201326591"}, true, "administrator-provisioned directory quota"},
		{"production larger queue", []string{"logs.collectorPersistence.profile=production", "logs.collectorPersistence.quotaVerified=true", "logs.collectorBuffer=1G"}, true, "administrator-provisioned directory quota"},
		{"path traversal", []string{"logs.collectorPersistence.hostPathBase=/var/lib/../shared"}, true, "pattern"},
		{"queue integer overflow", []string{"logs.collectorBuffer=999999999999999999999G"}, true, "64-GiB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"template", "collector-check", "../../charts/orbitjob", "--namespace", "own", "--set", "operator.namespaceTenants=own=tenant", "--show-only", "templates/log-collector.yaml"}
			for _, v := range tc.values {
				args = append(args, "--set", v)
			}
			output, err := exec.Command("helm", args...).CombinedOutput()
			if (err != nil) != tc.fail || !strings.Contains(string(output), tc.want) {
				t.Fatalf("render failed=%v want=%v; %s", err != nil, tc.fail, output)
			}
		})
	}
}
