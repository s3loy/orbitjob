//go:build integration

package postgres

import (
	"context"
	"testing"

	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/platform/postgrestest"
)

// testULID builds a 26-character identifier shaped like the ULIDs the schema
// stores, without pulling in the generator for a test fixture.
func testULID(prefix byte) string {
	var b [26]byte
	b[0] = prefix
	for i := 1; i < 26; i++ {
		b[i] = '0'
	}
	return string(b[:])
}

func TestPolicyRepositoryEffectiveDocuments(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewPolicyRepository(db)
	ctx := context.Background()

	tenantID := testULID('T')
	keyID := testULID('K')
	allowID := testULID('A')
	denyID := testULID('D')

	if _, err := db.ExecContext(ctx, `
		INSERT INTO tenants (id, slug, name, status) VALUES ($1, $2, $3, 'active')
	`, tenantID, "slug-"+tenantID, "Tenant "+tenantID); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO api_keys (id, tenant_id, kind, key_hash, key_prefix)
		VALUES ($1, $2, 'tenant', 'hash', 'otj_testpref')
	`, keyID, tenantID); err != nil {
		t.Fatalf("insert key: %v", err)
	}

	allowDoc := `{"version":"1","statement":[{"effect":"Allow","action":["job:Get","job:Delete"],"resource":["orbitjob:self:*:job/*"]}]}`
	denyDoc := `{"version":"1","statement":[{"effect":"Deny","action":["job:Delete"],"resource":["orbitjob:self:*:job/*"]}]}`
	for id, doc := range map[string]string{allowID: allowDoc, denyID: denyDoc} {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO policies (id, tenant_id, name, document) VALUES ($1, $2, $3, $4::jsonb)
		`, id, tenantID, "policy-"+id, doc); err != nil {
			t.Fatalf("insert policy %s: %v", id, err)
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO key_policies (key_id, policy_id) VALUES ($1, $2)
		`, keyID, id); err != nil {
			t.Fatalf("bind policy %s: %v", id, err)
		}
	}

	docs, err := repo.EffectiveDocuments(ctx, tenantID, keyID)
	if err != nil {
		t.Fatalf("EffectiveDocuments: %v", err)
	}

	req := policy.Request{Action: "job:Get", Resource: policy.ARN{Tenant: tenantID, Group: "ci", Type: "job", ID: "1"}}
	if got := policy.Evaluate(docs, req); got != policy.DecisionAllow {
		t.Fatalf("job:Get should be allowed, got %v", got)
	}
	// The deny binds alongside the allow and must win.
	req = policy.Request{Action: "job:Delete", Resource: policy.ARN{Tenant: tenantID, Group: "ci", Type: "job", ID: "1"}}
	if got := policy.Evaluate(docs, req); got != policy.DecisionDeny {
		t.Fatalf("the deny policy must win, got %v", got)
	}
}

func TestPolicyRepositoryBoundaryNarrows(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewPolicyRepository(db)
	ctx := context.Background()

	tenantID := testULID('T')
	keyID := testULID('K')
	allowID := testULID('A')
	boundaryID := testULID('B')

	if _, err := db.ExecContext(ctx, `
		INSERT INTO tenants (id, slug, name, status) VALUES ($1, $2, $3, 'active')
	`, tenantID, "slug-"+tenantID, "Tenant "+tenantID); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	allowDoc := `{"version":"1","statement":[{"effect":"Allow","action":["job:Get","job:Delete"],"resource":["orbitjob:self:*:job/*"]}]}`
	boundaryDoc := `{"version":"1","statement":[{"effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}]}`
	if _, err := db.ExecContext(ctx, `
		INSERT INTO policies (id, tenant_id, name, document) VALUES ($1, $2, $3, $4::jsonb)
	`, allowID, tenantID, "allow", allowDoc); err != nil {
		t.Fatalf("insert allow policy: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO policies (id, tenant_id, name, document) VALUES ($1, $2, $3, $4::jsonb)
	`, boundaryID, tenantID, "boundary", boundaryDoc); err != nil {
		t.Fatalf("insert boundary policy: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO api_keys (id, tenant_id, kind, key_hash, key_prefix, boundary_policy_id)
		VALUES ($1, $2, 'tenant', 'hash', 'otj_testpref', $3)
	`, keyID, tenantID, boundaryID); err != nil {
		t.Fatalf("insert key: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO key_policies (key_id, policy_id) VALUES ($1, $2)
	`, keyID, allowID); err != nil {
		t.Fatalf("bind allow policy: %v", err)
	}

	docs, err := repo.EffectiveDocuments(ctx, tenantID, keyID)
	if err != nil {
		t.Fatalf("EffectiveDocuments: %v", err)
	}
	req := policy.Request{Action: "job:Delete", Resource: policy.ARN{Tenant: tenantID, Group: "ci", Type: "job", ID: "1"}}
	if got := policy.Evaluate(docs, req); got != policy.DecisionDeny {
		t.Fatalf("the boundary does not allow job:Delete, got %v", got)
	}
	req = policy.Request{Action: "job:Get", Resource: policy.ARN{Tenant: tenantID, Group: "ci", Type: "job", ID: "1"}}
	if got := policy.Evaluate(docs, req); got != policy.DecisionAllow {
		t.Fatalf("job:Get survives the boundary, got %v", got)
	}
}

func TestPolicyRepositoryGetPlatformPreset(t *testing.T) {
	db := postgrestest.Open(t)
	repo := NewPolicyRepository(db)
	ctx := context.Background()

	// The schema harness truncated the presets away with everything else;
	// restore them before reading one back.
	if err := postgrestest.SeedPresetPolicies(ctx, db); err != nil {
		t.Fatalf("seed platform preset policies: %v", err)
	}

	rec, err := repo.Get(ctx, "any-tenant", "00000000000000000000000012")
	if err != nil {
		t.Fatalf("Get platform preset: %v", err)
	}
	if !rec.IsPlatformPreset() {
		t.Fatalf("expected a platform preset, got tenant %q", rec.TenantID)
	}
	if rec.Name != "ReadOnlyAccess" {
		t.Fatalf("unexpected preset %q", rec.Name)
	}
	// A preset carries the self token; reading it must resolve it for the caller.
	req := policy.Request{Action: "job:Get", Resource: policy.ARN{Tenant: "any-tenant", Group: "ci", Type: "job", ID: "1"}}
	if got := policy.Evaluate([]policy.Document{rec.Document}, req); got != policy.DecisionAllow {
		t.Fatalf("the preset should allow job:Get after self resolution, got %v", got)
	}
	req = policy.Request{Action: "job:Delete", Resource: policy.ARN{Tenant: "any-tenant", Group: "ci", Type: "job", ID: "1"}}
	if got := policy.Evaluate([]policy.Document{rec.Document}, req); got != policy.DecisionDeny {
		t.Fatalf("ReadOnlyAccess must not allow job:Delete, got %v", got)
	}
}
