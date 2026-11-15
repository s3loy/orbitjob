//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/lib/pq"
	"orbitjob/internal/admin/app/account"
	"orbitjob/internal/platform/scan"
)

func TestAccountRecoveryConsumesPreviouslyIssuedPasswordReset(t *testing.T) {
	db, s := accountFixture(t)
	user, session, sessionToken := accountClaim(t, s, "recovery-admin")
	ctx := context.Background()
	reset, err := s.IssuePasswordReset(ctx, session.ID, user.ID, "00000000000000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	_, rememberedToken, err := s.Login(ctx, user.Username, accountTestPassword, true)
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := s.IssueInstallationCapability(ctx, "recovery", user.Username, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	const recoveredPassword = "A locally recovered account passphrase 42"
	recovered, err := s.Setup(ctx, recovery.Token, user.Username, recoveredPassword, "")
	if err != nil || recovered.ID != user.ID || recovered.Status != "active" || !recovered.InstallationAdmin {
		t.Fatalf("recovered user=%+v err=%v", recovered, err)
	}
	for _, token := range []string{sessionToken, rememberedToken} {
		if _, err = s.Authenticate(ctx, token); !errors.Is(err, account.ErrUnauthenticated) {
			t.Fatalf("recovery left an existing session valid: %v", err)
		}
	}
	if err = s.ResetPassword(ctx, reset.Token, "An obsolete reset holder's passphrase 73"); !errors.Is(err, account.ErrInvalid) {
		t.Fatalf("pre-recovery reset capability remained usable: %v", err)
	}
	if _, _, err = s.Login(ctx, user.Username, recoveredPassword, false); err != nil {
		t.Fatalf("recovered password was replaced: %v", err)
	}
	if _, _, err = s.Login(ctx, user.Username, accountTestPassword, false); !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatalf("pre-recovery password remained usable: %v", err)
	}
	if _, err = s.Setup(ctx, recovery.Token, user.Username, "A replayed recovery account passphrase 74", ""); !errors.Is(err, account.ErrInvalid) {
		t.Fatalf("recovery replay accepted: %v", err)
	}
	var consumed int
	if err = db.QueryRow(`SELECT count(*) FROM account_capabilities WHERE id IN($1,$2) AND consumed_at IS NOT NULL`, reset.ID, recovery.ID).Scan(&consumed); err != nil || consumed != 2 {
		t.Fatalf("consumed recovery/reset capabilities=%d err=%v", consumed, err)
	}
	var claims, passwordResets int
	if err = db.QueryRow(`SELECT count(*) FILTER(WHERE event_type='installation.claimed'),count(*) FILTER(WHERE event_type='account.password_reset') FROM installation_audit_events WHERE actor_id=$1`, user.ID).Scan(&claims, &passwordResets); err != nil || claims != 2 || passwordResets != 0 {
		t.Fatalf("claim audits=%d reset audits=%d err=%v", claims, passwordResets, err)
	}
}

type accountLockProbeRepository struct {
	repository account.Repository
	beforeCall func(string)
}

func (r accountLockProbeRepository) Call(ctx context.Context, operation string, input, output any) error {
	r.beforeCall(operation)
	return r.repository.Call(ctx, operation, input, output)
}

func (r accountLockProbeRepository) Transaction(ctx context.Context, fn func(account.Repository) error) error {
	return r.repository.Transaction(ctx, func(scoped account.Repository) error {
		return fn(accountLockProbeRepository{repository: scoped, beforeCall: r.beforeCall})
	})
}

func TestAccountMemberReactivationPreservesSavedGrantsAndSerialization(t *testing.T) {
	db, s := accountFixture(t)
	_, admin, _ := accountClaim(t, s, "member-recovery-admin")
	ctx := context.Background()
	workspace, err := s.CreateWorkspace(ctx, admin.ID, "Member recovery", "member-recovery")
	if err != nil {
		t.Fatal(err)
	}
	wideID, narrowID, ceilingID := scan.GenerateID(), scan.GenerateID(), scan.GenerateID()
	const wide = `{"version":"1","statement":[{"effect":"Allow","action":["job:Get","job:Trigger"],"resource":["orbitjob:self:*:job/*"]}]}`
	const narrow = `{"version":"1","statement":[{"effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}]}`
	if _, err = db.Exec(`INSERT INTO policies(id,tenant_id,name,document)VALUES($1,$4,'MemberWide',$5::jsonb),($2,$4,'MemberNarrow',$6::jsonb),($3,$4,'MemberCeiling',$5::jsonb)`, wideID, narrowID, ceilingID, workspace.ID, wide, narrow); err != nil {
		t.Fatal(err)
	}
	createMember := func(username, role, policyID, boundaryID string) account.User {
		t.Helper()
		invite, err := s.Invite(ctx, admin.ID, account.Grant{TenantID: workspace.ID, Role: role, PolicyIDs: []string{policyID}, BoundaryPolicyID: boundaryID, Username: username})
		if err != nil {
			t.Fatal(err)
		}
		user, err := s.AcceptInvitation(ctx, "", invite.Token, username, accountTestPassword, "")
		if err != nil {
			t.Fatal(err)
		}
		return user
	}
	limited := createMember("bounded-member-admin", account.RoleAdmin, narrowID, ceilingID)
	limitedSession, _, err := s.Login(ctx, limited.Username, accountTestPassword, false)
	if err != nil {
		t.Fatal(err)
	}
	wider := createMember("wider-saved-member", account.RoleViewer, wideID, "")
	same := createMember("same-saved-member", account.RoleViewer, narrowID, "")
	bounded := createMember("bounded-saved-member", account.RoleViewer, narrowID, ceilingID)
	boundedWider := createMember("bounded-wider-saved-member", account.RoleViewer, wideID, ceilingID)
	disabled, active := "disabled", "active"
	for _, user := range []account.User{wider, same, bounded, boundedWider} {
		if err = s.UpdateMember(ctx, admin.ID, user.ID, workspace.ID, account.MemberChange{Status: &disabled}); err != nil {
			t.Fatal(err)
		}
	}
	grantState := func(userID string) (string, string, int) {
		t.Helper()
		var grant, status string
		var auditCount int
		if err := db.QueryRow(`SELECT jsonb_build_object('role',m.role,'boundary',m.boundary_policy_id,'policies',(SELECT jsonb_agg(policy_id ORDER BY policy_id) FROM account_membership_policies WHERE tenant_id=m.tenant_id AND user_id=m.user_id))::text,m.status,(SELECT count(*) FROM audit_events WHERE tenant_id=m.tenant_id AND resource_id=m.user_id AND event_type='membership.updated') FROM account_memberships m WHERE tenant_id=$1 AND user_id=$2`, workspace.ID, userID).Scan(&grant, &status, &auditCount); err != nil {
			t.Fatal(err)
		}
		return grant, status, auditCount
	}
	lockChecks := make(map[string]int)
	probe := accountLockProbeRepository{repository: NewAccountRepository(queryAdminConnection(t, db)), beforeCall: func(operation string) {
		t.Helper()
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		tx, err := db.BeginTx(probeCtx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		var one int
		err = tx.QueryRowContext(probeCtx, `SELECT 1 FROM account_installation WHERE singleton FOR UPDATE NOWAIT`).Scan(&one)
		var lockErr *pq.Error
		if !errors.As(err, &lockErr) || lockErr.Code != "55P03" {
			t.Fatalf("operation %s ran without the installation lock: %v", operation, err)
		}
		lockChecks[operation]++
	}}
	checked := account.NewService(probe)
	before, _, beforeAudits := grantState(wider.ID)
	if err = checked.UpdateMember(ctx, limitedSession.ID, wider.ID, workspace.ID, account.MemberChange{Status: &active}); !errors.Is(err, account.ErrForbidden) {
		t.Fatalf("bounded administrator restored a wider saved grant: %v", err)
	}
	after, status, audits := grantState(wider.ID)
	if after != before || status != disabled || audits != beforeAudits {
		t.Fatal("rejected reactivation changed saved grants, status or audit")
	}
	before, _, beforeAudits = grantState(boundedWider.ID)
	if err = checked.UpdateMember(ctx, limitedSession.ID, boundedWider.ID, workspace.ID, account.MemberChange{Status: &active}); !errors.Is(err, account.ErrForbidden) {
		t.Fatalf("matching boundary concealed a wider saved policy grant: %v", err)
	}
	after, status, audits = grantState(boundedWider.ID)
	if after != before || status != disabled || audits != beforeAudits {
		t.Fatal("wider-policy rejection changed saved grants, status or audit")
	}
	before, _, beforeAudits = grantState(same.ID)
	if err = checked.UpdateMember(ctx, limitedSession.ID, same.ID, workspace.ID, account.MemberChange{Status: &active}); !errors.Is(err, account.ErrForbidden) {
		t.Fatalf("bounded administrator restored a member without a saved boundary: %v", err)
	}
	after, status, audits = grantState(same.ID)
	if after != before || status != disabled || audits != beforeAudits {
		t.Fatal("missing-boundary rejection changed saved grants, status or audit")
	}
	for _, entry := range []struct {
		user    account.User
		service *account.Service
		session account.Session
	}{{same, s, admin}, {bounded, checked, limitedSession}} {
		user := entry.user
		before, _, beforeAudits := grantState(user.ID)
		if err = entry.service.UpdateMember(ctx, entry.session.ID, user.ID, workspace.ID, account.MemberChange{Status: &active}); err != nil {
			t.Fatalf("same-scope saved grant could not be restored: %v", err)
		}
		after, status, audits := grantState(user.ID)
		if after != before || status != active || audits != beforeAudits+1 {
			t.Fatal("status-only reactivation implicitly rewrote the saved grant")
		}
	}
	for _, operation := range []string{"members", "session_actor", "membership", "documents", "grant_documents", "update_member"} {
		if lockChecks[operation] == 0 {
			t.Fatalf("lock probe missed %s", operation)
		}
	}
	if err = s.UpdateMember(ctx, admin.ID, wider.ID, workspace.ID, account.MemberChange{Status: &active}); err != nil {
		t.Fatal(err)
	}
	before, _, _ = grantState(wider.ID)
	for _, next := range []*string{&active, &disabled} {
		if err = checked.UpdateMember(ctx, limitedSession.ID, wider.ID, workspace.ID, account.MemberChange{Status: next}); err != nil {
			t.Fatalf("status no-op or disable required wider delegation: %v", err)
		}
		after, status, _ = grantState(wider.ID)
		if after != before || status != *next {
			t.Fatal("status no-op or disable changed the saved grant")
		}
	}
	if err = checked.UpdateMember(ctx, limitedSession.ID, wider.ID, workspace.ID, account.MemberChange{Status: &active, PolicyIDs: []string{narrowID}}); err != nil {
		t.Fatalf("explicit grant narrowing failed: %v", err)
	}
	member, err := s.Membership(ctx, wider.ID, workspace.ID)
	if err != nil || member.BoundaryPolicyID != ceilingID || len(member.PolicyIDs) != 1 || member.PolicyIDs[0] != narrowID {
		t.Fatalf("explicit grant edit did not preserve boundary inheritance: member=%+v err=%v", member, err)
	}
	if err = s.UpdateMember(ctx, admin.ID, bounded.ID, workspace.ID, account.MemberChange{Status: &disabled}); err != nil {
		t.Fatal(err)
	}
	before, _, beforeAudits = grantState(bounded.ID)
	if _, err = db.Exec(`CREATE FUNCTION account_member_reject_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type='membership.updated' AND NEW.resource_id=TG_ARGV[0] THEN RAISE EXCEPTION 'injected member audit failure'; END IF; RETURN NEW; END $$`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(fmt.Sprintf(`CREATE TRIGGER account_member_reject_audit BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION account_member_reject_audit('%s')`, bounded.ID)); err != nil {
		t.Fatal(err)
	}
	if err = checked.UpdateMember(ctx, limitedSession.ID, bounded.ID, workspace.ID, account.MemberChange{Status: &active}); err == nil {
		t.Fatal("injected audit failure did not abort reactivation")
	}
	after, status, audits = grantState(bounded.ID)
	if after != before || status != disabled || audits != beforeAudits {
		t.Fatal("failed audit write committed part of the reactivation")
	}
	var singleton int
	if err = db.QueryRow(`SELECT 1 FROM account_installation WHERE singleton FOR UPDATE NOWAIT`).Scan(&singleton); err != nil || singleton != 1 {
		t.Fatalf("failed transaction retained installation lock: %v", err)
	}
}
