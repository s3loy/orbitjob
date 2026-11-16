package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateReleaseVersion(t *testing.T) {
	tests := []struct {
		name  string
		value string
		valid bool
	}{
		{name: "release", value: "v0.2.1", valid: true},
		{name: "numeric prerelease zero", value: "v1.2.3-0", valid: true},
		{name: "alphanumeric prerelease", value: "v1.2.3-rc.1", valid: true},
		{name: "alphanumeric leading zero", value: "v1.2.3-alpha-01", valid: true},
		{name: "build metadata", value: "v1.2.3+build.01", valid: false},
		{name: "missing prefix", value: "1.2.3", valid: false},
		{name: "invalid version", value: "vfoo", valid: false},
		{name: "major leading zero", value: "v01.2.3", valid: false},
		{name: "minor leading zero", value: "v1.02.3", valid: false},
		{name: "patch leading zero", value: "v1.2.03", valid: false},
		{name: "numeric prerelease leading zero", value: "v1.2.3-01", valid: false},
		{name: "nested numeric prerelease leading zero", value: "v1.2.3-rc.01", valid: false},
		{name: "empty prerelease", value: "v1.2.3-", valid: false},
		{name: "empty identifier", value: "v1.2.3-rc..1", valid: false},
		{name: "shell metacharacter", value: "v1.2.3;echo-owned", valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chart := releaseChart(t, "version: "+strings.TrimPrefix(tt.value, "v")+"\nappVersion: \""+strings.TrimPrefix(tt.value, "v")+"\"\n")
			err := exec.Command("bash", "validate-release-version.sh", tt.value, chart).Run()
			if tt.valid && err != nil {
				t.Fatalf("valid version %q rejected: %v", tt.value, err)
			}
			if !tt.valid && err == nil {
				t.Fatalf("invalid version %q accepted", tt.value)
			}
		})
	}
}

func TestValidateReleaseVersionRequiresMatchingChart(t *testing.T) {
	for _, tt := range []struct {
		name, chart string
		valid       bool
	}{
		{"matching scalars", "version: 1.2.3\nappVersion: \"1.2.3\"\n", true},
		{"matching quoted scalars", "version: '1.2.3' # chart\nappVersion: \"1.2.3\" # image\n", true},
		{"stale chart", "version: 1.2.2\nappVersion: \"1.2.3\"\n", false},
		{"stale image", "version: 1.2.3\nappVersion: \"1.2.2\"\n", false},
		{"missing chart version", "appVersion: \"1.2.3\"\n", false},
		{"missing image version", "version: 1.2.3\n", false},
		{"duplicate version", "version: 1.2.3\nversion: 1.2.3\nappVersion: 1.2.3\n", false},
		{"duplicate appVersion", "version: 1.2.3\nappVersion: 1.2.3\nappVersion: 1.2.3\n", false},
		{"mismatched quote", "version: 1.2.3\nappVersion: '1.2.3\"\n", false},
		{"nested fields", "metadata:\n  version: 1.2.3\n  appVersion: 1.2.3\n", false},
		{"missing file", "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			chart := releaseChart(t, tt.chart)
			if tt.chart == "" {
				if err := os.Remove(chart); err != nil {
					t.Fatal(err)
				}
			}
			out, err := exec.Command("bash", "validate-release-version.sh", "v1.2.3", chart).CombinedOutput()
			if (err == nil) != tt.valid {
				t.Fatalf("valid=%v, err=%v: %s", tt.valid, err, out)
			}
		})
	}
}

func releaseChart(t *testing.T, contents string) string {
	t.Helper()
	chart := filepath.Join(t.TempDir(), "Chart.yaml")
	if err := os.WriteFile(chart, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return chart
}
