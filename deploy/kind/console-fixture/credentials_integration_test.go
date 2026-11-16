//go:build integration

package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"orbitjob/internal/admin/app/account"
	adminpostgres "orbitjob/internal/admin/store/postgres"
	"orbitjob/internal/core/domain/apikey"
	"orbitjob/internal/core/domain/audit"
	"orbitjob/internal/platform/postgrestest"
	"orbitjob/internal/platform/scan"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

func TestCredentialRetirementRecoversAuditedCreatorsWithoutJournals(t *testing.T) {
	db := postgrestest.Open(t)
	ctx := context.Background()
	owner := "console-installed-123456789"
	tenant, foreign := scan.GenerateID(), scan.GenerateID()
	for _, id := range []string{tenant, foreign} {
		if _, err := db.ExecContext(ctx, "INSERT INTO tenants(id,name,slug) VALUES($1,$2,$3)", id, id, id); err != nil {
			t.Fatal(err)
		}
	}
	user, unrelatedUser, seed := scan.GenerateID(), scan.GenerateID(), scan.GenerateID()
	passwordHash, err := account.HashPassword("fixture-password-local-only")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ id, username string }{{user, owner + "-key"}, {unrelatedUser, "unrelated-account"}} {
		if _, err := db.ExecContext(ctx, "INSERT INTO account_users(id,username,display_name,password_hash) VALUES($1,$2,$2,$3)", row.id, row.username, passwordHash); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO api_keys(id,tenant_id,kind,name,key_hash,key_prefix,created_by)
	 VALUES($1,$2,'tenant','seed','unused-test-key-hash','otj_fixture',$3)`, seed, tenant, owner); err != nil {
		t.Fatal(err)
	}
	repository := adminpostgres.NewAPIKeyRepository(db)
	create := func(actor, kind, target string) string {
		t.Helper()
		id := scan.GenerateID()
		// Use the production persistence path, whose created_by is a tenant ID,
		// while the transaction records the authenticated actor separately.
		input := apikey.PersistInput{ID: id, TenantID: target, Name: "created-during-test", KeyHash: "unused-test-key-hash", KeyPrefix: "otj_fixture", BoundBy: target}
		event := audit.Event{TenantID: target, ActorID: actor, EventType: audit.EventCreate, ResourceType: audit.ResourceAPIKey, ResourceID: id}
		if err := repository.Create(audit.WithActor(ctx, actor, kind), input, event); err != nil {
			t.Fatal(err)
		}
		return id
	}
	child := create(seed, "api_key", tenant)
	grandchild := create(child, "api_key", tenant)
	humanChild := create(user, "user", foreign)
	unrelated := create(unrelatedUser, "user", tenant)
	// A mismatched-tenant audit row cannot claim an unrelated persisted key.
	if _, err := db.ExecContext(ctx, `INSERT INTO audit_events(tenant_id,actor_type,actor_id,event_type,resource_type,resource_id)
	 VALUES($1,'user',$2,'create','apikey',$3)`, foreign, user, unrelated); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "credentials.json")
	state := map[string]any{"prefix": owner, "context": fixtureContext, "namespace": fixtureNamespace,
		"userIds": []string{user}, "retired": false}
	if err := writePrivateJSON(path, state); err != nil {
		t.Fatal(err)
	}
	if err := retireCredentials(ctx, options{manifest: path}, func(ctx context.Context, _ options, input string) error {
		_, err := db.ExecContext(ctx, input)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{seed, child, grandchild, humanChild, unrelated} {
		var revoked sql.NullTime
		if err := db.QueryRowContext(ctx, "SELECT revoked_at FROM api_keys WHERE id=$1", id).Scan(&revoked); err != nil {
			t.Fatal(err)
		}
		if revoked.Valid != (id != unrelated) {
			t.Errorf("key %s revoked=%v; unrelated=%v", id, revoked.Valid, id == unrelated)
		}
	}
	for _, id := range []string{user, unrelatedUser} {
		var status string
		if err := db.QueryRowContext(ctx, "SELECT status FROM account_users WHERE id=$1", id).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if (status == "disabled") != (id == user) {
			t.Errorf("account %s has unexpected status %s", id, status)
		}
	}
	if readCredentialState(t, path)["retired"] != true {
		t.Fatal("successful database retirement was not recorded")
	}
}
