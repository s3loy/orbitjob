package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"orbitjob/internal/admin/app/account"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/platform/scan"
)

const administratorPolicy = "00000000000000000000000010"

// Credential provisioning deliberately uses the owner only inside the guarded
// disposable installation. Invalid platform boundaries cannot be produced by
// the public API and are required to verify the authentication failure path.
func seedCredentials(ctx context.Context, env *environment, o options) error {
	if _, err := os.Lstat(o.manifest); !errors.Is(err, os.ErrNotExist) {
		return errors.New("credential manifest already exists; retain it for run or cleanup")
	}
	if err := withTenant(ctx, env.db, o.tenant, func(tx *sql.Tx) error {
		var tenant string
		return tx.QueryRowContext(ctx, "SELECT id FROM tenants WHERE id=$1", o.tenant).Scan(&tenant)
	}); err != nil {
		return errors.New("credentials requires an existing installation tenant")
	}
	return createCredentialSet(ctx, o, ownerSQL)
}

type credentialBuilder struct {
	state    map[string]any
	sql      strings.Builder
	owner    string
	tenant   string
	foreign  string
	group    string
	policies []string
	keys     []string
	users    []string
}

type humanCredential struct {
	Username string `json:"username"`
	Password string `json:"password"`
	UserID   string `json:"userId"`
}

func quoteSQL(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }

func (b *credentialBuilder) policy(name string, tenant string, document any) string {
	id := scan.GenerateID()
	body, _ := json.Marshal(document)
	target := "NULL"
	if tenant != "" {
		target = quoteSQL(tenant)
	}
	fmt.Fprintf(&b.sql, "INSERT INTO policies(id,tenant_id,name,description,document) VALUES(%s,%s,%s,%s,%s::jsonb);\n",
		quoteSQL(id), target, quoteSQL(b.owner+"-"+name), quoteSQL(b.owner), quoteSQL(string(body)))
	b.policies = append(b.policies, id)
	return id
}

func grant(actions []string, resources ...string) map[string]any {
	return map[string]any{"effect": "Allow", "action": actions, "resource": resources}
}

func document(statements ...map[string]any) map[string]any {
	return map[string]any{"version": "1", "statement": statements}
}

func (b *credentialBuilder) key(name, tenant, group, boundary string, policies []string, lifecycle string) error {
	secret := make([]byte, 24)
	if _, err := rand.Read(secret); err != nil {
		return err
	}
	plaintext := "otj_" + base64.RawURLEncoding.EncodeToString(secret)
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	id := scan.GenerateID()
	target, kind := "NULL", "platform"
	if tenant != "" {
		target, kind = quoteSQL(tenant), "tenant"
	}
	nullable := func(v string) string {
		if v == "" {
			return "NULL"
		}
		return quoteSQL(v)
	}
	expires, revoked := "NULL", "NULL"
	if lifecycle == "expired" {
		expires = "CURRENT_TIMESTAMP - interval '1 hour'"
	}
	if lifecycle == "revoked" {
		revoked = "CURRENT_TIMESTAMP"
	}
	fmt.Fprintf(&b.sql, "INSERT INTO api_keys(id,tenant_id,kind,name,resource_group_id,boundary_policy_id,key_hash,key_prefix,created_by,expires_at,revoked_at) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s);\n",
		quoteSQL(id), target, quoteSQL(kind), quoteSQL(b.owner+"-"+name), nullable(group), nullable(boundary), quoteSQL(string(hash)), quoteSQL(plaintext[:12]), quoteSQL(b.owner), expires, revoked)
	for _, policy := range policies {
		fmt.Fprintf(&b.sql, "INSERT INTO key_policies(key_id,policy_id,bound_by) VALUES(%s,%s,%s);\n", quoteSQL(id), quoteSQL(policy), quoteSQL(b.owner))
	}
	b.keys = append(b.keys, id)
	b.state[name], b.state[name+"Id"] = plaintext, id
	if tenant != "" && group == "" && lifecycle == "" {
		if err := b.human(name, tenant, boundary, policies, plaintext); err != nil {
			return err
		}
	}
	return nil
}

// human provisions the same explicit tenant grants for browser qualification.
// Group and platform credentials remain API-only fixtures with their own scope.
func (b *credentialBuilder) human(name, tenant, boundary string, policies []string, key string) error {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return err
	}
	password := base64.RawURLEncoding.EncodeToString(secret)
	hash, err := account.HashPassword(password)
	if err != nil {
		return err
	}
	credential := humanCredential{Username: strings.ToLower(b.owner + "-" + name), Password: password, UserID: scan.GenerateID()}
	role := account.RoleViewer
	if name == "key" || name == "seedKey" {
		role = account.RoleAdmin
	}
	fmt.Fprintf(&b.sql, "INSERT INTO account_users(id,username,display_name,password_hash,installation_admin) VALUES(%s,%s,%s,%s,%t);\n",
		quoteSQL(credential.UserID), quoteSQL(credential.Username), quoteSQL("Console "+name), quoteSQL(hash), name == "seedKey")
	boundarySQL := "NULL"
	if boundary != "" {
		boundarySQL = quoteSQL(boundary)
	}
	fmt.Fprintf(&b.sql, "INSERT INTO account_memberships(tenant_id,user_id,role,boundary_policy_id) VALUES(%s,%s,%s,%s);\n",
		quoteSQL(tenant), quoteSQL(credential.UserID), quoteSQL(role), boundarySQL)
	for _, policyID := range policies {
		fmt.Fprintf(&b.sql, "INSERT INTO account_membership_policies(tenant_id,user_id,policy_id) VALUES(%s,%s,%s);\n",
			quoteSQL(tenant), quoteSQL(credential.UserID), quoteSQL(policyID))
	}
	profiles, ok := b.state["accountsByKey"].(map[string]humanCredential)
	if !ok {
		profiles = make(map[string]humanCredential)
		b.state["accountsByKey"] = profiles
	}
	profiles[key] = credential
	b.users = append(b.users, credential.UserID)
	if name == "key" {
		b.state["username"], b.state["password"], b.state["userId"] = credential.Username, credential.Password, credential.UserID
	}
	return nil
}

func createCredentialSet(ctx context.Context, o options, execute func(context.Context, options, string) error) error {
	if len(o.tenant) != 26 {
		return errors.New("credentials requires an existing --tenant ID")
	}
	b := credentialBuilder{state: map[string]any{}, owner: "console-installed-" + fmt.Sprint(time.Now().UnixMilli()), tenant: o.tenant, foreign: scan.GenerateID(), group: scan.GenerateID()}
	b.sql.WriteString("BEGIN;\n")
	b.sql.WriteString("DO $$ BEGIN IF EXISTS(SELECT 1 FROM account_users) OR EXISTS(SELECT 1 FROM account_installation WHERE claimed_at IS NOT NULL) THEN RAISE EXCEPTION 'browser fixtures require an unclaimed dedicated installation'; END IF; END $$;\n")
	fmt.Fprintf(&b.sql, "INSERT INTO tenants(id,name,slug,metadata) VALUES(%s,%s,%s,jsonb_build_object('console_fixture',%s));\n", quoteSQL(b.foreign), quoteSQL(b.owner+"-foreign"), quoteSQL(b.owner+"-foreign"), quoteSQL(b.owner))
	fmt.Fprintf(&b.sql, "INSERT INTO resource_groups(id,tenant_id,name,slug) VALUES(%s,%s,%s,%s);\n", quoteSQL(b.group), quoteSQL(b.tenant), quoteSQL(b.owner+"-group"), quoteSQL(b.owner+"-group"))
	full := b.policy("tenant-full", b.tenant, document(grant(policy.KnownActions, "orbitjob:self:*:*/*")))
	foreignFull := b.policy("foreign-full", b.foreign, document(grant(policy.KnownActions, "orbitjob:self:*:*/*")))
	readActions := []string{"job:Get", "job:List", "instance:Get", "instance:List", "function:Get", "function:List", "workflow:Get", "workflow:List", "tenant:Get", "tenant:List", "group:List", "policy:Get", "policy:List", "apikey:List"}
	read := b.policy("read", b.tenant, document(grant(readActions, "orbitjob:self:*:*/*")))
	getOnly := b.policy("get", b.tenant, document(grant([]string{"function:Get", "workflow:Get"}, "orbitjob:self:*:*/*")))
	listOnly := b.policy("list", b.tenant, document(grant([]string{"function:List", "workflow:List", "policy:List"}, "orbitjob:self:*:*/*")))
	workflow := b.policy("workflow", b.tenant, document(grant([]string{"workflow:Get", "workflow:List", "workflow:Trigger"}, "orbitjob:self:*:workflow/*")))
	deny := b.policy("deny", b.tenant, document(grant(policy.KnownActions, "orbitjob:self:*:*/*"), map[string]any{"effect": "Deny", "action": []string{"job:List", "function:Invoke", "workflow:Trigger"}, "resource": []string{"orbitjob:self:*:*/*"}}))
	boundary := b.policy("boundary", b.tenant, document(grant([]string{"job:List"}, "orbitjob:self:*:job/*")))
	createOnly := b.policy("create", b.tenant, document(grant([]string{"group:Create", "policy:Create", "job:Get"}, "orbitjob:self:*:*/*")))
	platform := b.policy("platform", "", document(grant([]string{"job:List"}, "orbitjob:"+b.tenant+":*:job/*"), grant([]string{"tenant:Create", "tenant:Get", "tenant:List"}, "orbitjob:*:*:tenant/*")))
	selfBoundary := b.policy("platform-self", "", document(grant([]string{"*"}, "orbitjob:self:*:*/*")))
	for _, sample := range []struct {
		name, tenant, group, boundary, lifecycle string
		policies                                 []string
	}{
		{"key", b.tenant, "", "", "", []string{full}},
		{"foreignKey", b.foreign, "", "", "", []string{foreignFull}},
		{"groupKey", b.tenant, b.group, "", "", []string{full}},
		{"seedKey", b.tenant, "", "", "", []string{administratorPolicy}},
		{"readKey", b.tenant, "", "", "", []string{read}},
		{"getOnlyKey", b.tenant, "", "", "", []string{getOnly}},
		{"listOnlyKey", b.tenant, "", "", "", []string{listOnly}},
		{"workflowKey", b.tenant, "", "", "", []string{workflow}},
		{"denyKey", b.tenant, "", "", "", []string{deny}},
		{"boundaryKey", b.tenant, "", boundary, "", []string{administratorPolicy}},
		{"createOnlyKey", b.tenant, "", "", "", []string{createOnly}},
		{"platformKey", "", "", "", "", []string{platform}},
		{"unboundPlatformKey", "", "", "", "", nil},
		{"selfPlatformKey", "", "", selfBoundary, "", []string{platform}},
		{"expiredPlatformKey", "", "", "", "expired", []string{platform}},
		{"revokedPlatformKey", "", "", "", "revoked", []string{platform}},
		{"invalidBoundaryKey", "", "", read, "", []string{platform}},
	} {
		if err := b.key(sample.name, sample.tenant, sample.group, sample.boundary, sample.policies, sample.lifecycle); err != nil {
			return err
		}
	}
	b.sql.WriteString("UPDATE account_installation SET claimed_at=clock_timestamp() WHERE singleton AND claimed_at IS NULL;\n")
	fmt.Fprintf(&b.sql, "INSERT INTO installation_audit_events(event_type,resource_id,details) VALUES('fixture.accounts_prepared',%s,jsonb_build_object('scope','dedicated-installed-verification'));\n", quoteSQL(b.owner))
	b.sql.WriteString("COMMIT;\n")
	b.state["context"], b.state["namespace"], b.state["prefix"] = fixtureContext, fixtureNamespace, b.owner
	b.state["tenantId"], b.state["foreignTenantId"], b.state["groupId"] = b.tenant, b.foreign, b.group
	b.state["keys"], b.state["ownedPolicies"] = b.keys, b.policies
	b.state["userIds"] = b.users
	b.state["policies"] = append([]string{administratorPolicy}, b.policies...)
	for _, alias := range []string{"fullKey", "historyKey", "logsKey"} {
		b.state[alias] = b.state["key"]
	}
	b.state["prepared"], b.state["authPrepared"], b.state["retired"] = false, false, false
	if err := writePrivateJSON(o.manifest, b.state); err != nil {
		return err
	}
	if err := execute(ctx, o, b.sql.String()); err != nil {
		return err
	}
	b.state["authPrepared"] = true
	return writePrivateJSON(o.manifest, b.state)
}

func ownerSQL(ctx context.Context, o options, input string) error {
	cmd := exec.CommandContext(ctx, "kubectl", "--context", o.context, "-n", o.namespace,
		"exec", "-i", "deploy/orbitjob-postgres", "--", "sh", "-c", `psql -v ON_ERROR_STOP=1 -Atq -U "$POSTGRES_USER" -d "$POSTGRES_DB"`)
	cmd.Stdin = strings.NewReader(input)
	if err := cmd.Run(); err != nil {
		return errors.New("guarded fixture credential transaction failed")
	}
	return nil
}

func revokeCredentials(ctx context.Context, o options) error {
	return retireCredentials(ctx, o, ownerSQL)
}

func retireCredentials(ctx context.Context, o options, execute func(context.Context, options, string) error) error {
	info, err := os.Lstat(o.manifest)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return errors.New("credential manifest must be a private regular file")
	}
	data, err := os.ReadFile(o.manifest)
	if err != nil {
		return err
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		return errors.New("invalid credential manifest")
	}
	owner, _ := state["prefix"].(string)
	if !regexp.MustCompile(`^console-installed-[0-9]+$`).MatchString(owner) || state["context"] != fixtureContext || state["namespace"] != fixtureNamespace {
		return errors.New("credential manifest is not owned by this fixture installation")
	}
	userPredicate := "FALSE"
	if ids, ok := state["userIds"].([]any); ok && len(ids) > 0 {
		owned := make([]string, 0, len(ids))
		for _, raw := range ids {
			id, ok := raw.(string)
			if !ok || !regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`).MatchString(id) {
				return errors.New("invalid owned account identity")
			}
			owned = append(owned, quoteSQL(id))
		}
		userPredicate = "id IN (" + strings.Join(owned, ",") + ") AND username LIKE " + quoteSQL(owner+"-%")
	}
	// Public key creation records the tenant in created_by. Its transactional
	// audit event identifies the authenticated creator, including human users.
	// Follow those events so an interrupted browser write needs no local journal.
	ownership := `WITH RECURSIVE owned_actors(kind,id) AS (
 SELECT 'api_key'::text,id::text FROM api_keys WHERE created_by=` + quoteSQL(owner) + `
 UNION SELECT 'user'::text,id::text FROM account_users WHERE ` + userPredicate + `
 UNION SELECT 'api_key'::text,k.id::text FROM owned_actors parent
 JOIN audit_events e ON e.actor_type=parent.kind AND e.actor_id::text=parent.id
 JOIN api_keys k ON k.id::text=e.resource_id::text AND k.tenant_id=e.tenant_id
 WHERE e.event_type='create' AND e.resource_type='apikey'
) `
	input := "BEGIN;\n" +
		"DELETE FROM account_sessions WHERE user_id IN (SELECT id FROM account_users WHERE " + userPredicate + ");\n" +
		"UPDATE account_users SET status='disabled' WHERE " + userPredicate + ";\n" +
		"UPDATE account_capabilities SET consumed_at=COALESCE(consumed_at,CURRENT_TIMESTAMP) WHERE user_id IN (SELECT id FROM account_users WHERE " + userPredicate + ") OR issued_by IN (SELECT id FROM account_users WHERE " + userPredicate + ");\n" +
		ownership + "UPDATE api_keys SET revoked_at=COALESCE(revoked_at,CURRENT_TIMESTAMP) WHERE id::text IN (SELECT id FROM owned_actors WHERE kind='api_key');\n" +
		"DO $retirement$ BEGIN IF EXISTS (" + ownership + "SELECT 1 FROM api_keys k JOIN owned_actors a ON a.kind='api_key' AND a.id=k.id::text WHERE k.revoked_at IS NULL) THEN RAISE EXCEPTION 'fixture credential retirement is incomplete'; END IF; END $retirement$;\nCOMMIT;"
	if err := execute(ctx, o, input); err != nil {
		return err
	}
	state["retired"] = true
	return writePrivateJSON(o.manifest, state)
}
