package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"orbitjob/internal/core/domain/policy"
)

func TestCredentialSQLQuotesInputAndNeverStoresPlaintext(t *testing.T) {
	if got := quoteSQL("fixture'; DELETE FROM tenants; --"); got != "'fixture''; DELETE FROM tenants; --'" {
		t.Fatal("SQL literal did not escape its quote")
	}
	b := credentialBuilder{state: map[string]any{}, owner: "console-installed-123"}
	if err := b.key("key", "00000000000000000000000001", "", "", []string{administratorPolicy}, ""); err != nil {
		t.Fatal(err)
	}
	secret, ok := b.state["key"].(string)
	if !ok || !strings.HasPrefix(secret, "otj_") || len(secret) != 36 {
		t.Fatal("credential format is invalid")
	}
	if strings.Contains(b.sql.String(), secret) || !strings.Contains(b.sql.String(), "$2a$") {
		t.Fatal("SQL must contain only the bcrypt hash and short prefix")
	}
	if len(b.keys) != 1 || b.keys[0] != b.state["keyId"] || !strings.Contains(b.sql.String(), quoteSQL(b.owner)) {
		t.Fatal("credential ownership is not recorded")
	}
	profile := b.state["accountsByKey"].(map[string]humanCredential)[secret]
	if len(profile.Password) < 15 || profile.UserID == b.state["keyId"] || profile.Username == "" {
		t.Fatal("browser account must have a separate identity and password")
	}
	if strings.Contains(b.sql.String(), profile.Password) || !strings.Contains(b.sql.String(), "$argon2id$") {
		t.Fatal("browser password must be hashed before SQL provisioning")
	}
}

func TestCredentialRevocationRejectsUnownedOrPublicManifestBeforeKubectl(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	for _, state := range []map[string]any{
		{"prefix": "console-installed-123' OR true --", "context": fixtureContext, "namespace": fixtureNamespace},
		{"prefix": "console-installed-123", "context": "kind-other", "namespace": fixtureNamespace},
		{"prefix": "console-installed-123", "context": fixtureContext, "namespace": "other"},
	} {
		if err := writePrivateJSON(path, state); err != nil {
			t.Fatal(err)
		}
		if err := revokeCredentials(context.Background(), options{manifest: path}); err == nil || !strings.Contains(err.Error(), "not owned") {
			t.Fatal("unowned manifest reached external mutation")
		}
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if err := revokeCredentials(context.Background(), options{manifest: path}); err == nil || !strings.Contains(err.Error(), "private regular") {
		t.Fatal("public credential manifest was accepted")
	}
}

func TestFailedCredentialRetirementDoesNotClaimSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := writePrivateJSON(path, map[string]any{"prefix": "console-installed-123", "context": fixtureContext,
		"namespace": fixtureNamespace, "retired": false}); err != nil {
		t.Fatal(err)
	}
	want := errors.New("retirement transaction failed")
	err := retireCredentials(context.Background(), options{manifest: path}, func(context.Context, options, string) error { return want })
	if !errors.Is(err, want) || readCredentialState(t, path)["retired"] != false {
		t.Fatal("failed retirement must preserve a retryable manifest")
	}
}

func readCredentialState(t *testing.T, path string) map[string]any {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("credential manifest must exist with mode 0600")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestCredentialSetKeepsTenantAuthorityAndGlobalSetupSeparate(t *testing.T) {
	o := options{tenant: "00000000000000000000000001", manifest: filepath.Join(t.TempDir(), "credentials.json")}
	var statements string
	err := createCredentialSet(context.Background(), o, func(_ context.Context, _ options, input string) error {
		state := readCredentialState(t, o.manifest)
		if state["authPrepared"] != false || state["prepared"] != false {
			t.Fatal("credential manifest claimed readiness before its transaction")
		}
		statements = input
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	state := readCredentialState(t, o.manifest)
	if state["authPrepared"] != true || state["prepared"] != false || state["retired"] != false || state["context"] != fixtureContext || state["namespace"] != fixtureNamespace {
		t.Fatal("credential state does not distinguish auth provisioning from workload preparation")
	}
	owner := state["prefix"].(string)
	if !regexp.MustCompile(`^console-installed-[0-9]+$`).MatchString(owner) {
		t.Fatal("credential owner prefix is invalid")
	}
	if !strings.HasPrefix(statements, "BEGIN;\n") || !strings.HasSuffix(statements, "COMMIT;\n") {
		t.Fatal("credential provisioning must remain one transaction")
	}
	keyNames := []string{"key", "foreignKey", "groupKey", "seedKey", "readKey", "getOnlyKey", "listOnlyKey", "workflowKey", "denyKey", "boundaryKey", "createOnlyKey", "platformKey", "unboundPlatformKey", "selfPlatformKey", "expiredPlatformKey", "revokedPlatformKey", "invalidBoundaryKey"}
	ids := map[string]bool{}
	secrets := map[string]bool{}
	for _, name := range keyNames {
		id, idOK := state[name+"Id"].(string)
		secret, secretOK := state[name].(string)
		if !idOK || len(id) != 26 || ids[id] || !secretOK || len(secret) != 36 || !strings.HasPrefix(secret, "otj_") || secrets[secret] {
			t.Fatalf("%s must have a distinct valid key and ID", name)
		}
		if strings.Contains(statements, secret) {
			t.Fatal("credential transaction contains plaintext credentials")
		}
		ids[id], secrets[secret] = true, true
	}
	if len(state["keys"].([]any)) != len(ids) {
		t.Fatal("ownership list must contain every generated key exactly once")
	}
	for _, value := range state["keys"].([]any) {
		if !ids[value.(string)] {
			t.Fatal("ownership list contains an unknown or duplicate key")
		}
		delete(ids, value.(string))
	}
	for _, alias := range []string{"fullKey", "historyKey", "logsKey"} {
		if state[alias] != state["key"] {
			t.Fatalf("%s must use the ordinary tenant credential", alias)
		}
	}
	// Evaluate the actual documents emitted for PostgreSQL, not reconstructed
	// test grants. Their owner columns must agree with each key's tenant.
	documents := map[string]policy.Document{}
	owners := map[string]string{}
	policyRows := regexp.MustCompile(`INSERT INTO policies\(id,tenant_id,name,description,document\) VALUES\('([^']+)',(NULL|'[^']+'),'[^']+','[^']+','(.*)'::jsonb\);`)
	for _, row := range policyRows.FindAllStringSubmatch(statements, -1) {
		doc, err := policy.ParseDocument([]byte(strings.ReplaceAll(row[3], "''", "'")))
		if err != nil {
			t.Fatal(err)
		}
		documents[row[1]], owners[row[1]] = doc, strings.Trim(row[2], "'")
	}
	bindings := map[string][]string{}
	boundRows := regexp.MustCompile(`INSERT INTO key_policies\(key_id,policy_id,bound_by\) VALUES\('([^']+)','([^']+)','[^']+'\);`)
	for _, row := range boundRows.FindAllStringSubmatch(statements, -1) {
		bindings[row[1]] = append(bindings[row[1]], row[2])
	}
	profiles := state["accountsByKey"].(map[string]any)
	humanBindings := map[string][]string{}
	memberRows := regexp.MustCompile(`INSERT INTO account_membership_policies\(tenant_id,user_id,policy_id\) VALUES\('[^']+','([^']+)','([^']+)'\);`)
	for _, row := range memberRows.FindAllStringSubmatch(statements, -1) {
		humanBindings[row[1]] = append(humanBindings[row[1]], row[2])
	}
	for _, name := range keyNames {
		profile, exists := profiles[state[name].(string)]
		apiOnly := name == "groupKey" || strings.Contains(strings.ToLower(name), "platform") || name == "invalidBoundaryKey"
		if apiOnly {
			if exists {
				t.Fatalf("%s must retain its API-only authentication semantics", name)
			}
			continue
		}
		if !exists {
			t.Fatalf("%s has no matching browser account", name)
		}
		human := profile.(map[string]any)
		userID, password := human["userId"].(string), human["password"].(string)
		if userID == state[name+"Id"] || strings.Contains(statements, password) || len(password) < 15 {
			t.Fatalf("%s browser identity or password is unsafe", name)
		}
		if !slices.Equal(humanBindings[userID], bindings[state[name+"Id"].(string)]) {
			t.Fatalf("%s browser account widened or lost its fixture grants", name)
		}
		if name == "boundaryKey" {
			// Read the key's boundary from its emitted row instead of relying on
			// the order in which the helper creates policy fixtures.
			keyRow := regexp.MustCompile(`INSERT INTO api_keys[^\n]+VALUES\('` + state[name+"Id"].(string) + `','[^']+','tenant','[^']+',NULL,'([^']+)'`).FindStringSubmatch(statements)
			if len(keyRow) != 2 || !strings.Contains(statements, quoteSQL(userID)+",'viewer',"+quoteSQL(keyRow[1])) {
				t.Fatal("browser fixture lost its exact permission boundary")
			}
		}
	}
	main := profiles[state["key"].(string)].(map[string]any)
	if state["username"] != main["username"] || state["password"] != main["password"] || state["userId"] != main["userId"] {
		t.Fatal("primary browser login does not refer to the ordinary tenant profile")
	}
	baseline, err := os.ReadFile("../../../db/migrations/0001_baseline.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	preset := regexp.MustCompile(`(?s)\('` + administratorPolicy + `', NULL, 'AdministratorAccess',\s*'[^']*',\s*'([^']+)'\)`).FindSubmatch(baseline)
	if len(preset) != 2 {
		t.Fatal("administrator policy must resolve to the migration-owned preset")
	}
	global, err := policy.ParseDocument(preset[1])
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(bindings[state["seedKeyId"].(string)], []string{administratorPolicy}) {
		t.Fatal("only the separate setup credential should carry the global preset unbounded")
	}
	for _, name := range []string{"key", "foreignKey", "groupKey"} {
		tenant, other := o.tenant, state["foreignTenantId"].(string)
		if name == "foreignKey" {
			tenant, other = other, tenant
		}
		bound := bindings[state[name+"Id"].(string)]
		if len(bound) != 1 || bound[0] == administratorPolicy || owners[bound[0]] != tenant {
			t.Fatalf("%s must bind its own tenant policy", name)
		}
		doc := documents[bound[0]]
		if err := policy.ValidateTenantDocument(doc); err != nil {
			t.Fatalf("%s policy exceeds tenant policy rules: %v", name, err)
		}
		docs := []policy.Document{policy.ResolveSelf(doc, tenant)}
		for _, action := range policy.KnownActions {
			resource := policy.ARN{Tenant: tenant, Group: "group", Type: strings.SplitN(action, ":", 2)[0], ID: "target"}
			if policy.Evaluate(docs, policy.Request{Action: action, Resource: resource}) != policy.DecisionAllow {
				t.Fatalf("%s lost its legitimate own-tenant action %s", name, action)
			}
			resource.Tenant = other
			if policy.Evaluate(docs, policy.Request{Action: action, Resource: resource}) != policy.DecisionDeny {
				t.Fatalf("%s permits foreign-tenant action %s", name, action)
			}
		}
		if !errors.Is(policy.CheckGrantable(docs, []policy.Document{global}), policy.ErrPrivilegeEscalation) {
			t.Fatalf("%s can delegate a global administrator", name)
		}
		if err := policy.CheckGrantable([]policy.Document{global}, docs); err != nil {
			t.Fatal("setup authority cannot grant the tenant fixture policy")
		}
		group := "NULL"
		if name == "groupKey" {
			group = quoteSQL(state["groupId"].(string))
		}
		keyPrefix := "VALUES(" + quoteSQL(state[name+"Id"].(string)) + "," + quoteSQL(tenant) + ",'tenant'," + quoteSQL(owner+"-"+name) + "," + group + ",NULL,"
		if !strings.Contains(statements, keyPrefix) {
			t.Fatalf("%s has the wrong stored tenant, group, or boundary", name)
		}
	}
}

func TestFailedCredentialTransactionRetainsUnpreparedPrivateManifest(t *testing.T) {
	o := options{tenant: "00000000000000000000000001", manifest: filepath.Join(t.TempDir(), "credentials.json")}
	want := errors.New("transaction failed")
	err := createCredentialSet(context.Background(), o, func(context.Context, options, string) error { return want })
	if !errors.Is(err, want) {
		t.Fatal("credential transaction failure was not propagated")
	}
	state := readCredentialState(t, o.manifest)
	if state["authPrepared"] != false || state["prepared"] != false || state["retired"] != false {
		t.Fatal("failed transaction was presented as prepared or retired")
	}
}
