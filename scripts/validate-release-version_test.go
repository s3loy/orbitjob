package main

import (
	"os/exec"
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
		{name: "build metadata", value: "v1.2.3+build.01", valid: true},
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
			err := exec.Command("bash", "validate-release-version.sh", tt.value).Run()
			if tt.valid && err != nil {
				t.Fatalf("valid version %q rejected: %v", tt.value, err)
			}
			if !tt.valid && err == nil {
				t.Fatalf("invalid version %q accepted", tt.value)
			}
		})
	}
}
