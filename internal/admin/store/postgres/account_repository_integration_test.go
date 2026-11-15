//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"orbitjob/internal/admin/app/account"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/platform/postgrestest"
	"orbitjob/internal/platform/scan"
)

const accountTestPassword = "A test passphrase for OrbitJob 42"

func accountFixture(t *testing.T) (*sql.DB, *account.Service) {
	t.Helper()
	db := postgrestest.Open(t)
	if err := postgrestest.SeedPresetPolicies(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tenants(id,slug,name)VALUES('00000000000000000000000001','default','Default')`); err != nil {
		t.Fatal(err)
	}
	return db, account.NewService(NewAccountRepository(queryAdminConnection(t, db)))
}
func accountClaim(t *testing.T, s *account.Service, username string) (account.User, account.Session, string) {
	t.Helper()
	ctx := context.Background()
	cap, err := s.IssueInstallationCapability(ctx, "setup", "", 15*time.Minute)
	if err != nil {
		t.Fatalf("account claim: %v", err)
	}
	user, err := s.Setup(ctx, cap.Token, username, accountTestPassword, "Admin")
	if err != nil {
		t.Fatalf("account setup: %v", err)
	}
	session, token, err := s.Login(ctx, username, accountTestPassword, false)
	if err != nil {
		t.Fatalf("account login: %v", err)
	}
	return user, session, token
}
func TestAccountClaimConcurrencyAndClosedSetup(t *testing.T) {
	db, s := accountFixture(t)
	ctx := context.Background()
	initialized, err := s.Initialized(ctx)
	if err != nil || initialized {
		t.Fatalf("initialized=%v err=%v", initialized, err)
	}
	if _, err = s.Setup(ctx, strings.Repeat("x", 43), "stranger", accountTestPassword, ""); !errors.Is(err, account.ErrInvalid) {
		t.Fatalf("unguarded setup: %v", err)
	}
	cap, err := s.IssueInstallationCapability(ctx, "setup", "", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, name := range []string{"claimant-one", "claimant-two"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			_, e := s.Setup(ctx, cap.Token, name, accountTestPassword, "")
			errs <- e
		}(name)
	}
	wg.Wait()
	close(errs)
	successes := 0
	for e := range errs {
		if e == nil {
			successes++
		} else if !errors.Is(e, account.ErrInvalid) {
			t.Fatal(e)
		}
	}
	if successes != 1 {
		t.Fatalf("claims=%d", successes)
	}
	for table, want := range map[string]int{"account_users": 1, "account_memberships": 1, "account_membership_policies": 1, "installation_audit_events": 2, "audit_events": 1} {
		var got int
		if err = db.QueryRow("SELECT count(*) FROM " + table).Scan(&got); err != nil || got != want {
			t.Fatalf("%s=%d want=%d err=%v", table, got, want, err)
		}
	}
	if _, err = s.IssueInstallationCapability(ctx, "setup", "", time.Minute); !errors.Is(err, account.ErrConflict) {
		t.Fatalf("reopened: %v", err)
	}
	initialized, err = s.Initialized(ctx)
	if err != nil || !initialized {
		t.Fatalf("initialized=%v err=%v", initialized, err)
	}
	// Explicit recovery does not clear the durable claimed marker and restores
	// the selected identity rather than turning the installation public.
	recovery, err := s.IssueInstallationCapability(ctx, "recovery", "recovered", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Setup(ctx, recovery.Token, "mismatched", accountTestPassword, ""); !errors.Is(err, account.ErrInvalid) {
		t.Fatalf("bound recovery: %v", err)
	}
	if _, err = s.Setup(ctx, recovery.Token, "recovered", accountTestPassword, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Setup(ctx, recovery.Token, "recovered", accountTestPassword, ""); !errors.Is(err, account.ErrInvalid) {
		t.Fatalf("replay: %v", err)
	}
}
func TestAccountSessionsResetAndExpiry(t *testing.T) {
	db, s := accountFixture(t)
	user, session, token := accountClaim(t, s, "administrator")
	ctx := context.Background()
	if _, _, err := s.Login(ctx, "administrator", "incorrect password", false); !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatal(err)
	}
	persisted, err := s.Authenticate(ctx, token)
	if err != nil || persisted.User.ID != user.ID || persisted.CSRFToken == "" {
		t.Fatalf("authentication %v %v", persisted, err)
	}
	originalExpiry := persisted.ExpiresAt
	time.Sleep(5 * time.Millisecond)
	persisted, err = s.Authenticate(ctx, token)
	if err != nil || !persisted.ExpiresAt.Equal(originalExpiry) {
		t.Fatal("session slid")
	}
	if d := session.ExpiresAt.Sub(session.CreatedAt); d > 8*time.Hour || d < 7*time.Hour {
		t.Fatalf("session duration=%s", d)
	}
	remembered, rememberToken, err := s.Login(ctx, "administrator", accountTestPassword, true)
	if err != nil {
		t.Fatal(err)
	}
	if d := remembered.ExpiresAt.Sub(remembered.CreatedAt); d > 7*24*time.Hour || d < 6*24*time.Hour {
		t.Fatalf("remember duration=%s", d)
	}
	if err = s.RevokeSession(ctx, session.ID, remembered.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(ctx, rememberToken); !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatalf("revoked=%v", err)
	}
	if _, err = db.Exec(`UPDATE account_sessions SET recent_auth_at=now()-interval '16 minutes' WHERE id=$1`, session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Users(ctx, session.ID); !errors.Is(err, account.ErrRecentAuth) {
		t.Fatalf("stale recent auth=%v", err)
	}
	if err = s.Reauthenticate(ctx, session.ID, accountTestPassword); err != nil {
		t.Fatal(err)
	}
	reset, err := s.IssuePasswordReset(ctx, session.ID, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ResetPassword(ctx, reset.Token, "A changed passphrase for OrbitJob 73"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(ctx, token); !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatalf("password reset left session=%v", err)
	}
	if err = s.ResetPassword(ctx, reset.Token, accountTestPassword); !errors.Is(err, account.ErrInvalid) {
		t.Fatalf("reset replay=%v", err)
	}
	_, _, err = s.Login(ctx, "administrator", accountTestPassword, false)
	if !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatalf("old password accepted=%v", err)
	}
	session, token, err = s.Login(ctx, "administrator", "A changed passphrase for OrbitJob 73", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE account_sessions SET expires_at=now()-interval '1 second' WHERE id=$1`, session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(ctx, token); !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatalf("expired=%v", err)
	}
	if n, err := s.Cleanup(ctx, 1); err != nil || n != 1 {
		t.Fatalf("bounded cleanup=%d %v", n, err)
	}
}

func TestAccountSessionRevocationAuditsOnlyOwnedExistingSessions(t *testing.T) {
	db, s := accountFixture(t)
	_, actor, _ := accountClaim(t, s, "session-audit-admin")
	ctx := context.Background()
	owned, ownedToken, err := s.Login(ctx, "session-audit-admin", accountTestPassword, false)
	if err != nil {
		t.Fatal(err)
	}
	invitation, err := s.Invite(ctx, actor.ID, account.Grant{TenantID: "00000000000000000000000001", Role: account.RoleViewer, Username: "other-session-owner"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.AcceptInvitation(ctx, "", invitation.Token, "other-session-owner", accountTestPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	foreign, foreignToken, err := s.Login(ctx, other.Username, accountTestPassword, false)
	if err != nil {
		t.Fatal(err)
	}
	countAudits := func() int {
		t.Helper()
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM installation_audit_events WHERE event_type='account.session_revoked'`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	for _, tc := range []struct{ name, id string }{{"unknown", scan.GenerateID()}, {"another account", foreign.ID}} {
		if err = s.RevokeSession(ctx, actor.ID, tc.id); err != nil {
			t.Fatalf("%s target broke idempotent revocation: %v", tc.name, err)
		}
		if got := countAudits(); got != 0 {
			t.Errorf("%s target emitted success audit without deleting an owned session: count=%d want=0", tc.name, got)
		}
	}
	if _, err = s.Authenticate(ctx, foreignToken); err != nil {
		t.Fatalf("revoking another account's session changed it: %v", err)
	}
	if err = s.RevokeSession(ctx, actor.ID, owned.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(ctx, ownedToken); !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatalf("owned session remained active: %v", err)
	}
	if got := countAudits(); got != 1 {
		t.Errorf("owned session deletion audit count=%d want=1", got)
	}
	if err = s.RevokeSession(ctx, actor.ID, owned.ID); err != nil {
		t.Fatalf("retry broke idempotent revocation: %v", err)
	}
	if got := countAudits(); got != 1 {
		t.Errorf("retry emitted another success audit: count=%d want=1", got)
	}
	before := countAudits()
	if _, err = db.Exec(`UPDATE account_sessions SET recent_auth_at=now()-interval '16 minutes' WHERE id=$1`, actor.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.RevokeSession(ctx, actor.ID, foreign.ID); !errors.Is(err, account.ErrRecentAuth) {
		t.Fatalf("stale authentication can revoke sessions: %v", err)
	}
	if got := countAudits(); got != before {
		t.Fatal("rejected revocation emitted a success audit")
	}
}

func TestAccountInvitationIsolationAndLastAdmin(t *testing.T) {
	db, s := accountFixture(t)
	first, session, token := accountClaim(t, s, "first-admin")
	ctx := context.Background()
	workspace, err := s.CreateWorkspace(ctx, session.ID, "Other", "other")
	if err != nil {
		t.Fatal(err)
	}
	invitation, err := s.Invite(ctx, session.ID, account.Grant{TenantID: workspace.ID, Role: account.RoleViewer, Username: "invited-user"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AcceptInvitation(ctx, "", invitation.Token, "wrong-user", accountTestPassword, ""); !errors.Is(err, account.ErrForbidden) {
		t.Fatalf("username mismatch=%v", err)
	}
	invited, err := s.AcceptInvitation(ctx, "", invitation.Token, "invited-user", accountTestPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AcceptInvitation(ctx, "", invitation.Token, "invited-user", accountTestPassword, ""); !errors.Is(err, account.ErrInvalid) {
		t.Fatalf("invite replay=%v", err)
	}
	spaces, err := s.Workspaces(ctx, invited.ID)
	if err != nil || len(spaces) != 1 || spaces[0].ID != workspace.ID {
		t.Fatalf("workspace leak=%v %v", spaces, err)
	}
	if _, err = s.Membership(ctx, invited.ID, "00000000000000000000000001"); !errors.Is(err, account.ErrForbidden) {
		t.Fatalf("cross-workspace=%v", err)
	}
	docs, err := s.EffectiveDocuments(ctx, invited.ID, workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		tenant, action string
		want           policy.Decision
	}{{workspace.ID, "job:Get", policy.DecisionAllow}, {workspace.ID, "job:Trigger", policy.DecisionDeny}, {"00000000000000000000000001", "job:Get", policy.DecisionDeny}} {
		got := policy.Evaluate(docs, policy.Request{Action: tc.action, Resource: policy.ARN{Tenant: tc.tenant, Group: "*", Type: "job", ID: "test"}})
		if got != tc.want {
			t.Fatalf("%+v: %v", tc, got)
		}
	}
	// Existing account invitation requires that account's authenticated identity.
	invitation, err = s.Invite(ctx, session.ID, account.Grant{TenantID: "00000000000000000000000001", Role: account.RoleViewer, Username: invited.Username})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AcceptInvitation(ctx, session.ID, invitation.Token, invited.Username, "", ""); !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatalf("hijack accepted=%v", err)
	}
	otherSession, otherToken, err := s.Login(ctx, invited.Username, accountTestPassword, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AcceptInvitation(ctx, otherSession.ID, invitation.Token, invited.Username, "", ""); err != nil {
		t.Fatal(err)
	}
	disabled := "disabled"
	if err = s.UpdateUser(ctx, session.ID, first.ID, account.UserChange{Status: &disabled}); !errors.Is(err, account.ErrLastAdmin) {
		t.Fatalf("last disable=%v", err)
	}
	yes := true
	if err = s.UpdateUser(ctx, session.ID, invited.ID, account.UserChange{InstallationAdmin: &yes}); err != nil {
		t.Fatal(err)
	}
	no := false
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, entry := range []struct{ sid, id string }{{session.ID, first.ID}, {otherSession.ID, invited.ID}} {
		wg.Add(1)
		go func(sid, id string) {
			defer wg.Done()
			results <- s.UpdateUser(ctx, sid, id, account.UserChange{InstallationAdmin: &no})
		}(entry.sid, entry.id)
	}
	wg.Wait()
	close(results)
	success, refused := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else if errors.Is(e, account.ErrLastAdmin) {
			refused++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || refused != 1 {
		t.Fatalf("demotions success=%d refused=%d", success, refused)
	}
	var adminCount int
	if err = db.QueryRow(`SELECT count(*) FROM account_users WHERE installation_admin AND status='active'`).Scan(&adminCount); err != nil || adminCount != 1 {
		t.Fatalf("admins=%d %v", adminCount, err)
	}
	// Disable invalidates every session of the target; no grant becomes global.
	var survivor string
	if err = db.QueryRow(`SELECT id FROM account_users WHERE installation_admin`).Scan(&survivor); err != nil {
		t.Fatal(err)
	}
	sid, target, targetToken := session.ID, invited.ID, otherToken
	if survivor == invited.ID {
		sid, target, targetToken = otherSession.ID, first.ID, token
	}
	if err = s.UpdateUser(ctx, sid, target, account.UserChange{Status: &disabled}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(ctx, targetToken); !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatalf("disabled session=%v", err)
	}
}

func TestAccountExistingInvitationUsesSessionIdentity(t *testing.T) {
	db, s := accountFixture(t)
	_, admin, _ := accountClaim(t, s, "invitation-admin")
	ctx := context.Background()
	initial, err := s.Invite(ctx, admin.ID, account.Grant{TenantID: "00000000000000000000000001", Role: account.RoleViewer, Username: "existing-member"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := s.AcceptInvitation(ctx, "", initial.Token, "existing-member", accountTestPassword, "Existing Member")
	if err != nil {
		t.Fatal(err)
	}
	session, token, err := s.Login(ctx, user.Username, accountTestPassword, false)
	if err != nil {
		t.Fatal(err)
	}
	var originalHash string
	if err = db.QueryRow(`SELECT password_hash FROM account_users WHERE id=$1`, user.ID).Scan(&originalHash); err != nil {
		t.Fatal(err)
	}
	for _, bound := range []bool{true, false} {
		name := "unbound"
		username := ""
		if bound {
			name, username = "bound", user.Username
		}
		t.Run(name, func(t *testing.T) {
			workspace, err := s.CreateWorkspace(ctx, admin.ID, "Invitation "+name, "invitation-"+name)
			if err != nil {
				t.Fatal(err)
			}
			invitation, err := s.Invite(ctx, admin.ID, account.Grant{TenantID: workspace.ID, Role: account.RoleViewer, Username: username})
			if err != nil {
				t.Fatal(err)
			}
			for _, sid := range []string{"", scan.GenerateID()} {
				if _, err = s.AcceptInvitation(ctx, sid, invitation.Token, user.Username, "", ""); !errors.Is(err, account.ErrUnauthenticated) {
					t.Fatalf("unauthenticated existing account accepted: %v", err)
				}
			}
			if _, err = s.AcceptInvitation(ctx, session.ID, invitation.Token, "another-user", "", ""); !errors.Is(err, account.ErrUnauthenticated) {
				t.Fatalf("body replaced session identity: %v", err)
			}
			if bound {
				if _, err = s.AcceptInvitation(ctx, admin.ID, invitation.Token, "", "", ""); !errors.Is(err, account.ErrForbidden) {
					t.Fatalf("bound invitation accepted another identity: %v", err)
				}
			}
			expired, _, err := s.Login(ctx, user.Username, accountTestPassword, false)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(`UPDATE account_sessions SET expires_at=now()-interval '1 second' WHERE id=$1`, expired.ID); err != nil {
				t.Fatal(err)
			}
			if _, err = s.AcceptInvitation(ctx, expired.ID, invitation.Token, "", "", ""); !errors.Is(err, account.ErrUnauthenticated) {
				t.Fatalf("expired session accepted: %v", err)
			}
			accepted, err := s.AcceptInvitation(ctx, session.ID, invitation.Token, "", "", "")
			if err != nil || accepted.ID != user.ID || accepted.DisplayName != user.DisplayName {
				t.Fatalf("token-only acceptance user=%+v err=%v", accepted, err)
			}
			member, err := s.Membership(ctx, user.ID, workspace.ID)
			if err != nil || member.Role != account.RoleViewer {
				t.Fatalf("membership=%+v err=%v", member, err)
			}
			if _, err = s.AcceptInvitation(ctx, session.ID, invitation.Token, "", "", ""); !errors.Is(err, account.ErrInvalid) {
				t.Fatalf("invitation replay accepted: %v", err)
			}
			var auditCount int
			if err = db.QueryRow(`SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND actor_id=$2 AND event_type='membership.created'`, workspace.ID, user.ID).Scan(&auditCount); err != nil || auditCount != 1 {
				t.Fatalf("membership audit count=%d err=%v", auditCount, err)
			}
		})
	}
	var finalHash string
	if err = db.QueryRow(`SELECT password_hash FROM account_users WHERE id=$1`, user.ID).Scan(&finalHash); err != nil || finalHash != originalHash {
		t.Fatal("accepting invitations changed an existing account password")
	}
	if authenticated, err := s.Authenticate(ctx, token); err != nil || authenticated.User.ID != user.ID {
		t.Fatal("accepting invitations invalidated the existing session")
	}
	var userCount int
	if err = db.QueryRow(`SELECT count(*) FROM account_users`).Scan(&userCount); err != nil || userCount != 2 {
		t.Fatalf("existing account duplicated: users=%d err=%v", userCount, err)
	}
}

func TestAccountSQLPrivilegesAndAPIKeyCompatibility(t *testing.T) {
	db, s := accountFixture(t)
	_, _, _ = accountClaim(t, s, "administrator")
	ctx := context.Background()
	admin := queryAdminConnection(t, db)
	for _, table := range []string{"account_users", "account_sessions", "account_capabilities", "account_installation", "account_memberships", "installation_audit_events"} {
		if _, err := admin.Exec(`SELECT * FROM ` + table); err == nil {
			t.Fatalf("direct account read granted: %s", table)
		}
	}
	var schema string
	if err := db.QueryRow(`SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"orbitjob_runtime", "orbitjob_operator", "orbitjob_admin"} {
		var allowed bool
		if err := db.QueryRow(`SELECT has_function_privilege($1,$2,'EXECUTE')`, role, schema+".orbitjob_identity(text,jsonb)").Scan(&allowed); err != nil || allowed != (role == "orbitjob_admin") {
			t.Fatalf("%s=%v %v", role, allowed, err)
		}
	}
	id := scan.GenerateID()
	hash := strings.Repeat("a", 60)
	if _, err := db.Exec(`INSERT INTO api_keys(id,kind,tenant_id,key_hash,key_prefix)VALUES($1,'tenant','00000000000000000000000001',$2,'test');`, id, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO key_policies(key_id,policy_id)VALUES($1,'00000000000000000000000011')`, id); err != nil {
		t.Fatal(err)
	}
	docs, err := NewPolicyRepository(admin).EffectiveDocuments(ctx, "00000000000000000000000001", id)
	if err != nil || len(docs) == 0 {
		t.Fatalf("legacy API key changed: %v %v", docs, err)
	}
	// Audit rows expose neither password hashes nor capability/session secrets.
	rows, err := db.Query(`SELECT row_to_json(e)::text FROM installation_audit_events e`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(raw, "argon2") || strings.Contains(raw, "token_hash") {
			t.Fatal("secret in audit")
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(fmt.Sprintf(`SET ROLE orbitjob_runtime`)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`SELECT orbitjob_identity('initialized','{}')`); err == nil {
		t.Fatal("runtime identity access")
	}
	_, _ = db.Exec(`RESET ROLE`)
}

func TestAccountWorkspaceAdminCannotResetSharedGlobalIdentity(t *testing.T) {
	db, s := accountFixture(t)
	_, admin, _ := accountClaim(t, s, "reset-admin")
	ctx := context.Background()
	first, err := s.CreateWorkspace(ctx, admin.ID, "First reset workspace", "first-reset-workspace")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateWorkspace(ctx, admin.ID, "Second reset workspace", "second-reset-workspace")
	if err != nil {
		t.Fatal(err)
	}
	createMember := func(username, role string) (account.User, account.Session, string) {
		t.Helper()
		invitation, err := s.Invite(ctx, admin.ID, account.Grant{TenantID: first.ID, Role: role, Username: username})
		if err != nil {
			t.Fatal(err)
		}
		user, err := s.AcceptInvitation(ctx, "", invitation.Token, username, accountTestPassword, "")
		if err != nil {
			t.Fatal(err)
		}
		session, token, err := s.Login(ctx, username, accountTestPassword, false)
		if err != nil {
			t.Fatal(err)
		}
		return user, session, token
	}
	_, workspaceAdmin, _ := createMember("workspace-reset-admin", account.RoleAdmin)
	shared, sharedSession, sharedToken := createMember("shared-reset-member", account.RoleViewer)
	invitation, err := s.Invite(ctx, admin.ID, account.Grant{TenantID: second.ID, Role: account.RoleViewer, Username: shared.Username})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AcceptInvitation(ctx, sharedSession.ID, invitation.Token, "", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = s.IssuePasswordReset(ctx, workspaceAdmin.ID, shared.ID, first.ID); !errors.Is(err, account.ErrForbidden) {
		t.Fatalf("workspace admin can reset a shared global account: %v", err)
	}
	var resetCount int
	if err = db.QueryRow(`SELECT count(*) FROM account_capabilities WHERE kind='reset' AND user_id=$1`, shared.ID).Scan(&resetCount); err != nil || resetCount != 0 {
		t.Fatalf("rejected reset created capability: count=%d err=%v", resetCount, err)
	}
	if _, err = s.Authenticate(ctx, sharedToken); err != nil {
		t.Fatalf("rejected reset affected shared account: %v", err)
	}
	reset, err := s.IssuePasswordReset(ctx, admin.ID, shared.ID, first.ID)
	if err != nil {
		t.Fatalf("installation admin reset rejected: %v", err)
	}
	if err = s.ResetPassword(ctx, reset.Token, "A changed shared account passphrase 42"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(ctx, sharedToken); !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatalf("global reset left shared session active: %v", err)
	}
	if _, _, err = s.Login(ctx, shared.Username, "A changed shared account passphrase 42", false); err != nil {
		t.Fatal(err)
	}
}

func TestAccountOwnerPoliciesDenyBoundariesAndExpiredCapabilities(t *testing.T) {
	db, s := accountFixture(t)
	first, session, _ := accountClaim(t, s, "owner-admin")
	ctx := context.Background()
	docs, err := s.EffectiveDocuments(ctx, first.ID, "00000000000000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range policy.KnownActions {
		req := policy.Request{Action: action, Resource: policy.ARN{Tenant: "00000000000000000000000001", Group: "*", Type: "instance", ID: "sample"}}
		if policy.Evaluate(docs, req) != policy.DecisionAllow {
			t.Fatalf("initial owner cannot %s", action)
		}
		req.Resource.Tenant = scan.GenerateID()
		if policy.Evaluate(docs, req) != policy.DecisionDeny {
			t.Fatalf("initial owner crossed workspace for %s", action)
		}
	}
	workspace, err := s.CreateWorkspace(ctx, session.ID, "Second", "second")
	if err != nil {
		t.Fatal(err)
	}
	cap, err := s.Invite(ctx, session.ID, account.Grant{TenantID: workspace.ID, Role: account.RoleAdmin, Username: "limited-admin"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := s.AcceptInvitation(ctx, "", cap.Token, "limited-admin", accountTestPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	limited, _, err := s.Login(ctx, user.Username, accountTestPassword, false)
	if err != nil {
		t.Fatal(err)
	}
	// Workspace admins cannot issue credentials that control a global identity.
	if _, err = s.IssuePasswordReset(ctx, limited.ID, first.ID, workspace.ID); !errors.Is(err, account.ErrForbidden) {
		t.Fatalf("workspace admin reset global identity: %v", err)
	}
	// A disabled membership becomes inaccessible while its session remains valid.
	disabled := "disabled"
	if err = s.UpdateMember(ctx, session.ID, user.ID, workspace.ID, account.MemberChange{Status: &disabled}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.EffectiveDocuments(ctx, user.ID, workspace.ID); !errors.Is(err, account.ErrForbidden) {
		t.Fatalf("disabled membership documents=%v", err)
	}
	active := "active"
	role := account.RoleViewer
	if err = s.UpdateMember(ctx, session.ID, user.ID, workspace.ID, account.MemberChange{Status: &active, Role: &role}); err != nil {
		t.Fatal(err)
	}
	expired, err := s.Invite(ctx, session.ID, account.Grant{TenantID: workspace.ID, Role: account.RoleViewer, Username: "too-late"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE account_capabilities SET expires_at=now()-interval '1 second' WHERE id=$1`, expired.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AcceptInvitation(ctx, "", expired.Token, "too-late", accountTestPassword, ""); !errors.Is(err, account.ErrInvalid) {
		t.Fatalf("expired invitation=%v", err)
	}
	reset, err := s.IssuePasswordReset(ctx, session.ID, user.ID, workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE account_capabilities SET expires_at=now()-interval '1 second' WHERE id=$1`, reset.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.ResetPassword(ctx, reset.Token, "Another changed password for tests"); !errors.Is(err, account.ErrInvalid) {
		t.Fatalf("expired reset=%v", err)
	}
	// Independent Deny and a boundary both survive the membership loader.
	denyID, boundaryID := scan.GenerateID(), scan.GenerateID()
	if _, err = db.Exec(`INSERT INTO policies(id,tenant_id,name,document)VALUES($1,$3,'DenyGet','{"version":"1","statement":[{"effect":"Deny","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}]}'),($2,$3,'OnlyGet','{"version":"1","statement":[{"effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}]}')`, denyID, boundaryID, workspace.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO account_membership_policies(tenant_id,user_id,policy_id)VALUES($1,$2,$3)`, workspace.ID, user.ID, denyID); err != nil {
		t.Fatal(err)
	}
	docs, err = s.EffectiveDocuments(ctx, user.ID, workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := policy.Request{Action: "job:Get", Resource: policy.ARN{Tenant: workspace.ID, Group: "*", Type: "job", ID: "sample"}}
	if policy.Evaluate(docs, request) != policy.DecisionDeny {
		t.Fatal("cross-document deny lost")
	}
	if _, err = db.Exec(`DELETE FROM account_membership_policies WHERE policy_id=$1`, denyID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE account_memberships SET boundary_policy_id=$3 WHERE tenant_id=$1 AND user_id=$2`, workspace.ID, user.ID, boundaryID); err != nil {
		t.Fatal(err)
	}
	docs, err = s.EffectiveDocuments(ctx, user.ID, workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Evaluate(docs, request) != policy.DecisionAllow {
		t.Fatal("boundary lost matching grant")
	}
	request.Action = "job:List"
	if policy.Evaluate(docs, request) != policy.DecisionDeny {
		t.Fatal("boundary lost narrowing")
	}
}

func TestAccountSelfPasswordChangeRevokesAllSessions(t *testing.T) {
	db, s := accountFixture(t)
	user, current, token := accountClaim(t, s, "self-password-user")
	ctx := context.Background()
	_, otherToken, err := s.Login(ctx, user.Username, accountTestPassword, true)
	if err != nil {
		t.Fatal(err)
	}
	reset, err := s.IssuePasswordReset(ctx, current.ID, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	newPassword := "A self changed OrbitJob account passphrase"
	if err = s.ChangePassword(ctx, current.ID, "incorrect current password", newPassword); !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatalf("wrong current password=%v", err)
	}
	if err = s.ChangePassword(ctx, current.ID, accountTestPassword, "passwordpassword"); !errors.Is(err, account.ErrInvalid) {
		t.Fatalf("weak replacement=%v", err)
	}
	for _, secret := range []string{token, otherToken} {
		if _, err = s.Authenticate(ctx, secret); err != nil {
			t.Fatalf("rejection invalidated session: %v", err)
		}
	}
	if err = s.ChangePassword(ctx, current.ID, accountTestPassword, newPassword); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{token, otherToken} {
		if _, err = s.Authenticate(ctx, secret); !errors.Is(err, account.ErrUnauthenticated) {
			t.Fatalf("password change left session active: %v", err)
		}
	}
	if err = s.ResetPassword(ctx, reset.Token, "A previously issued reset passphrase"); !errors.Is(err, account.ErrInvalid) {
		t.Fatalf("old reset survived password change: %v", err)
	}
	if _, _, err = s.Login(ctx, user.Username, accountTestPassword, false); !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatalf("old password survived: %v", err)
	}
	fresh, _, err := s.Login(ctx, user.Username, newPassword, false)
	if err != nil {
		t.Fatal(err)
	}
	// A password that changed after verification cannot authorize a stale write.
	repo := NewAccountRepository(queryAdminConnection(t, db))
	var credential struct {
		PasswordHash string `json:"password_hash"`
	}
	if err = repo.Call(ctx, "session_credential", map[string]any{"session_id": fresh.ID}, &credential); err != nil {
		t.Fatal(err)
	}
	concurrentHash, err := account.HashPassword("A concurrent OrbitJob account passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE account_users SET password_hash=$2 WHERE id=$1`, user.ID, concurrentHash); err != nil {
		t.Fatal(err)
	}
	if err = repo.Call(ctx, "change_password", map[string]any{"session_id": fresh.ID, "password_hash": credential.PasswordHash, "new_password_hash": credential.PasswordHash}, nil); !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatalf("stale verified hash accepted: %v", err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM installation_audit_events WHERE event_type='account.password_changed' AND actor_id=$1 AND resource_id=$1`, user.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("password change audit=%d err=%v", count, err)
	}
}

func TestAccountReactivationCannotRestoreGrantsBeyondCaller(t *testing.T) {
	db, s := accountFixture(t)
	_, owner, _ := accountClaim(t, s, "reactivation-owner")
	ctx := context.Background()
	workspace, err := s.CreateWorkspace(ctx, owner.ID, "Reactivation permissions", "reactivation-permissions")
	if err != nil {
		t.Fatal(err)
	}
	readID, wideID := scan.GenerateID(), scan.GenerateID()
	if _, err = db.Exec(`INSERT INTO policies(id,tenant_id,name,document) VALUES
	 ($1,$3,'Reactivation reader','{"version":"1","statement":[{"effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}]}'),
	 ($2,$3,'Reactivation operator','{"version":"1","statement":[{"effect":"Allow","action":["job:Get","job:Trigger"],"resource":["orbitjob:self:*:job/*"]}]}')`, readID, wideID, workspace.ID); err != nil {
		t.Fatal(err)
	}
	createMember := func(username, policyID string) (account.User, account.Session) {
		t.Helper()
		invitation, inviteErr := s.Invite(ctx, owner.ID, account.Grant{TenantID: workspace.ID, Role: account.RoleAdmin, Username: username, PolicyIDs: []string{policyID}})
		if inviteErr != nil {
			t.Fatal(inviteErr)
		}
		user, acceptErr := s.AcceptInvitation(ctx, "", invitation.Token, username, accountTestPassword, "")
		if acceptErr != nil {
			t.Fatal(acceptErr)
		}
		session, _, loginErr := s.Login(ctx, username, accountTestPassword, false)
		if loginErr != nil {
			t.Fatal(loginErr)
		}
		return user, session
	}
	actor, actorSession := createMember("reactivation-limited-admin", readID)
	target, _ := createMember("reactivation-disabled-operator", wideID)
	docs, err := s.EffectiveDocuments(ctx, actor.ID, workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := policy.Request{Action: "job:Trigger", Resource: policy.ARN{Tenant: workspace.ID, Group: "*", Type: "job", ID: "one-job"}}
	if policy.Evaluate(docs, request) != policy.DecisionDeny {
		t.Fatal("limited administrator unexpectedly holds the target's execution permission")
	}
	disabled := "disabled"
	if err = s.UpdateMember(ctx, owner.ID, target.ID, workspace.ID, account.MemberChange{Status: &disabled}); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var state string
		err := db.QueryRow(`SELECT jsonb_build_object('membership',to_jsonb(m),
		 'policies',(SELECT jsonb_agg(policy_id ORDER BY policy_id) FROM account_membership_policies WHERE tenant_id=m.tenant_id AND user_id=m.user_id),
		 'audits',(SELECT count(*) FROM audit_events WHERE tenant_id=m.tenant_id))::text
		 FROM account_memberships m WHERE tenant_id=$1 AND user_id=$2`, workspace.ID, target.ID).Scan(&state)
		if err != nil {
			t.Fatal(err)
		}
		return state
	}
	before := snapshot()
	active := "active"
	if err = s.UpdateMember(ctx, actorSession.ID, target.ID, workspace.ID, account.MemberChange{Status: &active}); !errors.Is(err, account.ErrForbidden) {
		t.Fatalf("limited administrator restored wider grants: %v", err)
	}
	if after := snapshot(); before != after {
		t.Fatal("rejected reactivation changed the membership, its policies or its audit history")
	}
	if _, err = s.Membership(ctx, target.ID, workspace.ID); !errors.Is(err, account.ErrForbidden) {
		t.Fatalf("rejected reactivation restored workspace access: %v", err)
	}
	// The installation owner with explicit workspace grants can restore it.
	if err = s.UpdateMember(ctx, owner.ID, target.ID, workspace.ID, account.MemberChange{Status: &active}); err != nil {
		t.Fatal(err)
	}
	if membership, getErr := s.Membership(ctx, target.ID, workspace.ID); getErr != nil || membership.Status != "active" {
		t.Fatalf("authorized reactivation failed: status=%s err=%v", membership.Status, getErr)
	}
	// Restoring a retained unbounded binding must not drop a bounded caller's
	// ceiling, even when the currently retained grant is already narrow.
	if err = s.UpdateMember(ctx, owner.ID, actor.ID, workspace.ID, account.MemberChange{BoundaryPolicyID: &readID}); err != nil {
		t.Fatal(err)
	}
	if err = s.UpdateMember(ctx, owner.ID, target.ID, workspace.ID, account.MemberChange{Status: &disabled, PolicyIDs: []string{readID}}); err != nil {
		t.Fatal(err)
	}
	before = snapshot()
	if err = s.UpdateMember(ctx, actorSession.ID, target.ID, workspace.ID, account.MemberChange{Status: &active}); !errors.Is(err, account.ErrForbidden) {
		t.Fatalf("bounded administrator restored an unbounded binding: %v", err)
	}
	if after := snapshot(); before != after {
		t.Fatal("rejected reactivation changed the original boundary, policies or audit history")
	}
	if err = s.UpdateMember(ctx, owner.ID, target.ID, workspace.ID, account.MemberChange{Status: &active}); err != nil {
		t.Fatal(err)
	}
	membership, err := s.Membership(ctx, target.ID, workspace.ID)
	if err != nil || membership.Status != "active" || membership.BoundaryPolicyID != "" || len(membership.PolicyIDs) != 1 || membership.PolicyIDs[0] != readID {
		t.Fatalf("authorized status change rewrote the retained binding: membership=%+v err=%v", membership, err)
	}
}

func TestAccountDelegationPreservesOrNarrowsCallerBoundary(t *testing.T) {
	db, s := accountFixture(t)
	_, owner, _ := accountClaim(t, s, "boundary-owner")
	ctx := context.Background()
	workspace, err := s.CreateWorkspace(ctx, owner.ID, "Delegation boundaries", "delegation-boundaries")
	if err != nil {
		t.Fatal(err)
	}
	grantID, callerBoundaryID := scan.GenerateID(), scan.GenerateID()
	narrowBoundaryID, wideBoundaryID := scan.GenerateID(), scan.GenerateID()
	if _, err = db.Exec(`INSERT INTO policies(id,tenant_id,name,document) VALUES
	 ($1,$5,'Boundary reader','{"version":"1","statement":[{"effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}]}'),
	 ($2,$5,'Caller ceiling','{"version":"1","statement":[{"effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}]}'),
	 ($3,$5,'Narrower ceiling','{"version":"1","statement":[{"effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/one-job"]}]}'),
	 ($4,$5,'Wider ceiling','{"version":"1","statement":[{"effect":"Allow","action":["job:Get","job:Trigger"],"resource":["orbitjob:self:*:job/*"]}]}')`, grantID, callerBoundaryID, narrowBoundaryID, wideBoundaryID, workspace.ID); err != nil {
		t.Fatal(err)
	}
	invitation, err := s.Invite(ctx, owner.ID, account.Grant{TenantID: workspace.ID, Role: account.RoleAdmin, Username: "bounded-delegator", PolicyIDs: []string{grantID}, BoundaryPolicyID: callerBoundaryID})
	if err != nil {
		t.Fatal(err)
	}
	actor, err := s.AcceptInvitation(ctx, "", invitation.Token, "bounded-delegator", accountTestPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	actorSession, _, err := s.Login(ctx, actor.Username, accountTestPassword, false)
	if err != nil {
		t.Fatal(err)
	}
	// The requested grant is only Get. Its wider ceiling still must be rejected
	// even though intersecting the two currently produces just Get.
	var before int
	if err = db.QueryRow(`SELECT count(*) FROM account_capabilities WHERE issued_by=$1`, actor.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Invite(ctx, actorSession.ID, account.Grant{TenantID: workspace.ID, Role: account.RoleViewer, Username: "widened-boundary-member", PolicyIDs: []string{grantID}, BoundaryPolicyID: wideBoundaryID}); !errors.Is(err, account.ErrForbidden) {
		t.Fatalf("wider boundary was delegated: %v", err)
	}
	var after int
	if err = db.QueryRow(`SELECT count(*) FROM account_capabilities WHERE issued_by=$1`, actor.ID).Scan(&after); err != nil || before != after {
		t.Fatalf("rejected delegation created a capability: before=%d after=%d err=%v", before, after, err)
	}
	for _, tc := range []struct {
		username          string
		requestedBoundary string
		wantBoundary      string
		wantOtherJob      policy.Decision
	}{
		{username: "inherited-boundary-member", wantBoundary: callerBoundaryID, wantOtherJob: policy.DecisionAllow},
		{username: "narrowed-boundary-member", requestedBoundary: narrowBoundaryID, wantBoundary: narrowBoundaryID, wantOtherJob: policy.DecisionDeny},
	} {
		t.Run(tc.username, func(t *testing.T) {
			capability, inviteErr := s.Invite(ctx, actorSession.ID, account.Grant{TenantID: workspace.ID, Role: account.RoleViewer, Username: tc.username, PolicyIDs: []string{grantID}, BoundaryPolicyID: tc.requestedBoundary})
			if inviteErr != nil {
				t.Fatal(inviteErr)
			}
			var persistedBoundary string
			if getErr := db.QueryRow(`SELECT grant_data->>'boundary_policy_id' FROM account_capabilities WHERE id=$1`, capability.ID).Scan(&persistedBoundary); getErr != nil || persistedBoundary != tc.wantBoundary {
				t.Fatalf("invitation boundary=%q want=%q err=%v", persistedBoundary, tc.wantBoundary, getErr)
			}
			child, acceptErr := s.AcceptInvitation(ctx, "", capability.Token, tc.username, accountTestPassword, "")
			if acceptErr != nil {
				t.Fatal(acceptErr)
			}
			membership, membershipErr := s.Membership(ctx, child.ID, workspace.ID)
			if membershipErr != nil || membership.BoundaryPolicyID != tc.wantBoundary {
				t.Fatalf("member boundary=%q want=%q err=%v", membership.BoundaryPolicyID, tc.wantBoundary, membershipErr)
			}
			docs, docsErr := s.EffectiveDocuments(ctx, child.ID, workspace.ID)
			if docsErr != nil {
				t.Fatal(docsErr)
			}
			for _, assertion := range []struct {
				action, job string
				want        policy.Decision
			}{
				{action: "job:Get", job: "one-job", want: policy.DecisionAllow},
				{action: "job:Get", job: "other-job", want: tc.wantOtherJob},
				{action: "job:Trigger", job: "one-job", want: policy.DecisionDeny},
			} {
				decision := policy.Evaluate(docs, policy.Request{Action: assertion.action, Resource: policy.ARN{Tenant: workspace.ID, Group: "*", Type: "job", ID: assertion.job}})
				if decision != assertion.want {
					t.Fatalf("action=%s job=%s decision=%v want=%v", assertion.action, assertion.job, decision, assertion.want)
				}
			}
		})
	}
}

func TestAccountRecoveryRevokesEarlierResetAndSessions(t *testing.T) {
	db, s := accountFixture(t)
	user, session, token := accountClaim(t, s, "recovered-existing-user")
	ctx := context.Background()
	_, otherToken, err := s.Login(ctx, user.Username, accountTestPassword, true)
	if err != nil {
		t.Fatal(err)
	}
	reset, err := s.IssuePasswordReset(ctx, session.ID, user.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := s.IssueInstallationCapability(ctx, "recovery", user.Username, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	recoveredPassword := "A recovered OrbitJob account passphrase"
	recovered, err := s.Setup(ctx, recovery.Token, user.Username, recoveredPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	if recovered.ID != user.ID || !recovered.InstallationAdmin || recovered.Status != "active" {
		t.Fatal("recovery did not preserve and restore the existing account identity")
	}
	for _, previousToken := range []string{token, otherToken} {
		if _, err = s.Authenticate(ctx, previousToken); !errors.Is(err, account.ErrUnauthenticated) {
			t.Fatalf("recovery left an earlier session usable: %v", err)
		}
	}
	var sessions int
	if err = db.QueryRow(`SELECT count(*) FROM account_sessions WHERE user_id=$1`, user.ID).Scan(&sessions); err != nil || sessions != 0 {
		t.Fatalf("recovery retained sessions=%d err=%v", sessions, err)
	}
	if err = s.ResetPassword(ctx, reset.Token, "An earlier recovery token must not replace this password"); !errors.Is(err, account.ErrInvalid) {
		t.Fatalf("earlier reset survived installation recovery: %v", err)
	}
	if _, _, err = s.Login(ctx, user.Username, accountTestPassword, false); !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatalf("recovery retained the earlier password: %v", err)
	}
	fresh, freshToken, err := s.Login(ctx, user.Username, recoveredPassword, false)
	if err != nil || fresh.User.ID != user.ID {
		t.Fatalf("recovered password login failed: user=%s err=%v", fresh.User.ID, err)
	}
	if authenticated, authErr := s.Authenticate(ctx, freshToken); authErr != nil || authenticated.User.ID != user.ID {
		t.Fatalf("earlier reset invalidated the recovered password: %v", authErr)
	}
}
