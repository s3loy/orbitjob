package policy

import "testing"

func TestIsPlatformPreset(t *testing.T) {
	if !(Record{TenantID: ""}).IsPlatformPreset() {
		t.Fatal("a policy with no owning tenant is a platform preset")
	}
	if (Record{TenantID: "T1"}).IsPlatformPreset() {
		t.Fatal("a tenant-owned policy is not a platform preset")
	}
}
