//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/lib/pq"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/platform/postgrestest"
	"orbitjob/internal/platform/scan"
	"testing"
)

func TestPlatformPolicyDocuments(t *testing.T) {
	db := postgrestest.Open(t)
	admin := queryAdminConnection(t, db)
	repo := NewPolicyRepository(admin)
	ctx := context.Background()
	if err := postgrestest.SeedPresetPolicies(ctx, db); err != nil {
		t.Fatal(err)
	}
	key, tenant, foreign := scan.GenerateID(), scan.GenerateID(), scan.GenerateID()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO tenants(id,slug,name,status)VALUES($1,'platform-test','Platform','active')`, tenant)
	exec(`INSERT INTO api_keys(id,kind,key_hash,key_prefix)VALUES($1,'platform','not-a-secret','platform')`, key)
	exec(`INSERT INTO policies(id,tenant_id,name,document)VALUES($1,$2,'Foreign',$3::jsonb)`, foreign, tenant, deleteGrantDoc)
	exec(`INSERT INTO key_policies(key_id,policy_id)VALUES($1,'00000000000000000000000010')`, key)
	request := policy.Request{Action: "job:Get", Resource: policy.ARN{Tenant: "target", Group: "group", Type: "job", ID: "id"}}
	check := func(want policy.Decision) {
		t.Helper()
		docs, err := repo.EffectivePlatformDocuments(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		if got := policy.Evaluate(docs, request); got != want {
			t.Fatalf("decision %s want %s", got, want)
		}
	}
	check(policy.DecisionAllow)
	// Explicit Deny in a second preset overrides administrator grants.
	deny := scan.GenerateID()
	exec(`INSERT INTO policies(id,name,document)VALUES($1,'Deny preset',$2::jsonb)`, deny, `{"version":"1","statement":[{"effect":"Deny","action":["job:Get"],"resource":["orbitjob:*:*:job/*"]}]}`)
	exec(`INSERT INTO key_policies(key_id,policy_id)VALUES($1,$2)`, key, deny)
	check(policy.DecisionDeny)
	exec(`DELETE FROM key_policies WHERE key_id=$1 AND policy_id=$2`, key, deny)
	// A valid global boundary narrows the administrator preset to exactly Get.
	boundary := scan.GenerateID()
	exec(`INSERT INTO policies(id,name,document)VALUES($1,'Get boundary',$2::jsonb)`, boundary, `{"version":"1","statement":[{"effect":"Allow","action":["job:Get"],"resource":["orbitjob:*:*:job/*"]}]}`)
	exec(`UPDATE api_keys SET boundary_policy_id=$2 WHERE id=$1`, key, boundary)
	check(policy.DecisionAllow)
	docs, err := repo.EffectivePlatformDocuments(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	listRequest := request
	listRequest.Action = "job:List"
	if policy.Evaluate(docs, listRequest) != policy.DecisionDeny {
		t.Fatal("boundary did not narrow grant")
	}
	exec(`UPDATE api_keys SET boundary_policy_id=NULL WHERE id=$1`, key)
	// Ordinary tenant loader never extracts a platform key, even with its ID.
	docs, err = repo.EffectiveDocuments(ctx, tenant, key)
	if err != nil || len(docs) != 0 {
		t.Fatalf("tenant path: docs=%d err=%v", len(docs), err)
	}
	exec(`UPDATE api_keys SET boundary_policy_id='00000000000000000000000011' WHERE id=$1`, key)
	check(policy.DecisionDeny) // TenantAdmin self remains self, never selected target.
	exec(`UPDATE api_keys SET boundary_policy_id=$2 WHERE id=$1`, key, foreign)
	if _, err := repo.EffectivePlatformDocuments(ctx, key); err == nil {
		t.Fatal("tenant-owned boundary silently removed")
	}
	// Simulate a corrupted missing reference in this isolated superuser fixture.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`SET LOCAL session_replication_role=replica`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`UPDATE api_keys SET boundary_policy_id=$2 WHERE id=$1`, key, scan.GenerateID()); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.EffectivePlatformDocuments(ctx, key); err == nil {
		t.Fatal("missing configured boundary silently removed")
	}
	exec(`UPDATE api_keys SET boundary_policy_id=NULL WHERE id=$1`, key)
	exec(`DELETE FROM key_policies WHERE key_id=$1`, key)
	check(policy.DecisionDeny)
	exec(`INSERT INTO key_policies(key_id,policy_id)VALUES($1,$2)`, key, foreign)
	check(policy.DecisionDeny) // No tenant-owned documents can be extracted.
	exec(`INSERT INTO key_policies(key_id,policy_id)VALUES($1,'00000000000000000000000010')`, key)
	for _, state := range []string{"revoked_at=now()", "expires_at=now()-interval '1 second'", "kind='tenant',tenant_id='" + tenant + "'"} {
		exec(`UPDATE api_keys SET `+state+` WHERE id=$1`, key)
		check(policy.DecisionDeny)
		exec(`UPDATE api_keys SET revoked_at=NULL,expires_at=NULL,kind='platform',tenant_id=NULL WHERE id=$1`, key)
	}
	if docs, err := repo.EffectivePlatformDocuments(ctx, "missing"); err != nil || len(docs) != 0 {
		t.Fatalf("missing: %v %v", docs, err)
	}
	var schema string
	if err := db.QueryRow(`SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	function := schema + ".orbitjob_platform_key_documents(text)"
	for _, role := range []string{"orbitjob_admin", "orbitjob_runtime", "orbitjob_operator"} {
		var allowed bool
		if err := db.QueryRow(`SELECT has_function_privilege($1,$2,'EXECUTE')`, role, function).Scan(&allowed); err != nil {
			t.Fatal(err)
		}
		if allowed != (role == "orbitjob_admin") {
			t.Fatalf("%s execute=%v", role, allowed)
		}
	}
	var publicGrant bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_proc p,LATERAL aclexplode(p.proacl) a WHERE p.oid=$1::regprocedure AND a.grantee=0 AND a.privilege_type='EXECUTE')`, function).Scan(&publicGrant); err != nil || publicGrant {
		t.Fatalf("PUBLIC execute=%v err=%v", publicGrant, err)
	}
	// Hostile caller path cannot redirect the helper's schema-qualified reads.
	conn, err := admin.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SET search_path=pg_temp,public`); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := conn.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM %s.orbitjob_platform_key_documents($1)`, schema), key).Scan(&count); err != nil || count != 1 {
		t.Fatalf("hostile path count=%d err=%v", count, err)
	}
}

func TestPlatformPolicyReaderExecutionBoundary(t *testing.T) {
	db := postgrestest.Open(t)
	var schema string
	if err := db.QueryRow(`SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	function := schema + ".orbitjob_platform_key_documents(text)"
	for _, role := range []string{"orbitjob_runtime", "orbitjob_operator"} {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`SET LOCAL ROLE ` + role); err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(fmt.Sprintf(`SELECT * FROM %s.orbitjob_platform_key_documents('missing')`, schema))
		_ = tx.Rollback()
		var pqErr *pq.Error
		if !errors.As(err, &pqErr) || pqErr.Code != "42501" {
			t.Fatalf("%s expected execution denial42501, got %v", role, err)
		}
	}
	var owner, path string
	var definer bool
	if err := db.QueryRow(`SELECT pg_get_userbyid(proowner),prosecdef,array_to_string(proconfig,',') FROM pg_proc WHERE oid=$1::regprocedure`, function).Scan(&owner, &definer, &path); err != nil {
		t.Fatal(err)
	}
	if owner != "orbitjob_table_owner" || !definer || path != "search_path=pg_catalog, "+schema {
		t.Fatalf("unsafe helper owner=%s definer=%v config=%s", owner, definer, path)
	}
}
