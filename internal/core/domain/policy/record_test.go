package policy

import (
	"testing"

	"orbitjob/internal/platform/scan"
)

func TestIsPlatformPreset(t *testing.T) {
	if !(Record{TenantID: ""}).IsPlatformPreset() {
		t.Fatal("a policy with no owning tenant is a platform preset")
	}
	if (Record{TenantID: scan.GenerateID()}).IsPlatformPreset() {
		t.Fatal("a tenant-owned policy is not a platform preset")
	}
}
