package audit

import "testing"

// TestEventAndResourceLiterals pins the vocabulary written into audit_events.
// Rows already in the database carry these literals and compliance queries
// filter on them, so changing a constant's value would split the trail in
// half: new rows invisible to old queries and old rows invisible to new ones.
// There is deliberately no "bind" event type: the key-creation event is the
// binding record, and its diff carries the granted policy ids.
func TestEventAndResourceLiterals(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"create event", EventCreate, "create"},
		{"delete event", EventDelete, "delete"},
		{"revoke event", EventRevoke, "revoke"},
		{"policy resource", ResourcePolicy, "policy"},
		{"apikey resource", ResourceAPIKey, "apikey"},
		{"resource group resource", ResourceGroup, "resource_group"},
	}
	seen := map[string]string{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got == "" {
				t.Fatal("empty literal would make rows unqueryable")
			}
			if tt.got != tt.want {
				t.Fatalf("literal = %q, want %q", tt.got, tt.want)
			}
			if other, dup := seen[tt.got]; dup {
				t.Fatalf("literal %q is shared between %s and %s; event and resource categories must stay distinguishable", tt.got, other, tt.name)
			}
			seen[tt.got] = tt.name
		})
	}
}
