package account

import (
	"context"
	"encoding/json"
	"errors"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/platform/scan"
	"strings"
	"testing"
	"time"
)

func TestPasswordHashValidationAndSalt(t *testing.T) {
	for _, password := range []string{"", "fourteen chars", "12345678901234", "correct horse battery staple", "password123456789", strings.Repeat("abc", 5), strings.Repeat("a", 20), strings.Repeat("x", 1025), string([]byte{0xff})} {
		if _, err := HashPassword(password); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid password accepted: %v", err)
		}
	}
	password := "fifteen-or-more characters"
	first, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("password hashes reused salt")
	}
	if !strings.HasPrefix(first, "$argon2id$v=19$m=65536,t=3,p=1$") {
		t.Fatal("unexpected password format")
	}
	if err = VerifyPassword(password, first); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ password, hash string }{{"wrong", first}, {password, "invalid"}, {password, strings.Replace(first, "m=65536", "m=999999999", 1)}, {strings.Repeat("x", 1025), first}, {password, strings.Replace(first, "v=19", "v=16", 1)}} {
		if !errors.Is(VerifyPassword(tc.password, tc.hash), ErrUnauthenticated) {
			t.Fatal("invalid password hash accepted")
		}
	}
	if _, err = HashPassword("\u8fd9\u662f\u4e00\u4e2a\u957f\u5ea6\u8d85\u8fc7\u5341\u4e94\u4e2a\u5b57\u7b26\u7684\u4e2d\u6587\u5bc6\u7801\u6d4b\u8bd5"); err != nil {
		t.Fatal(err)
	}
}
func TestUsernameNormalizationAndBoundaries(t *testing.T) {
	got, err := NormalizeUsername("  Alice.Example  ")
	if err != nil || got != "alice.example" {
		t.Fatalf("%q %v", got, err)
	}
	for _, username := range []string{"a", "two words", "\u96ea\u4eba\u8d26\u6237", "@alice", strings.Repeat("a", 65)} {
		if _, err = NormalizeUsername(username); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid username=%q %v", username, err)
		}
	}
}

type memoryRepository struct {
	call         func(string, any, any) error
	transactions int
}

func (r *memoryRepository) Call(_ context.Context, op string, in, out any) error {
	return r.call(op, in, out)
}
func (r *memoryRepository) Transaction(_ context.Context, fn func(Repository) error) error {
	r.transactions++
	return fn(r)
}
func assign(out any, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
func TestLoginNeverPersistsBearerSecretAndUsesAbsoluteExpiry(t *testing.T) {
	ctx := context.Background()
	hash, err := HashPassword("A test passphrase for OrbitJob 42")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	repo := &memoryRepository{}
	loginCount := 0
	repo.call = func(op string, in, out any) error {
		params, _ := in.(map[string]any)
		switch op {
		case "credential":
			return assign(out, map[string]any{"user": User{ID: "user-id", Username: "alice"}, "password_hash": hash})
		case "login":
			loginCount++
			if _, ok := params["token"]; ok {
				t.Fatal("bearer token persisted")
			}
			if len(params["token_hash"].(string)) != 64 {
				t.Fatal("token not hashed")
			}
			if params["remember"].(bool) != (loginCount == 2) {
				t.Fatal("remember mismatch")
			}
			return assign(out, Session{ID: "session-id", User: User{ID: "user-id"}, ExpiresAt: now.Add(8 * time.Hour), RecentAuthAt: now})
		case "authenticate":
			return assign(out, Session{ID: "session-id", User: User{ID: "user-id"}, ExpiresAt: now.Add(8 * time.Hour)})
		}
		return ErrInvalid
	}
	s := NewService(repo)
	s.now = func() time.Time { return now }
	for _, remember := range []bool{false, true} {
		session, token, err := s.Login(ctx, "Alice", "A test passphrase for OrbitJob 42", remember)
		if err != nil {
			t.Fatal(err)
		}
		if len(token) != 43 || session.CSRFToken == "" || session.CSRFToken == token {
			t.Fatal("invalid session secrets")
		}
		read, err := s.Authenticate(ctx, token)
		if err != nil || read.CSRFToken != session.CSRFToken {
			t.Fatal("csrf not stable across replica authentication")
		}
	}
	if _, err = s.Authenticate(ctx, "invalid"); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal(err)
	}
}
func TestMalformedCapabilityAvoidsHashingAndRepositoryCalls(t *testing.T) {
	repo := &memoryRepository{call: func(string, any, any) error { t.Fatal("malformed capability reached repository"); return nil }}
	s := NewService(repo)
	ctx := context.Background()
	if _, err := s.Setup(ctx, "bad", "alice", "too-short", ""); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.AcceptInvitation(ctx, "", "bad", "alice", "too-short", ""); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if err := s.ResetPassword(ctx, "bad", "too-short"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.IssueInstallationCapability(ctx, "unsupported", "", time.Minute); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.IssueInstallationCapability(ctx, "setup", "", 2*time.Hour); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.Cleanup(ctx, 1001); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
func TestDelegationRetainsDenyAndCannotUseRoleAsPrivilege(t *testing.T) {
	rawAllow := json.RawMessage(`{"version":"1","statement":[{"effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}]}`)
	rawDeny := json.RawMessage(`{"version":"1","statement":[{"effect":"Deny","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}]}`)
	repo := &memoryRepository{}
	invited := false
	repo.call = func(op string, in, out any) error {
		switch op {
		case "session_actor":
			return assign(out, User{ID: "alice"})
		case "membership":
			return assign(out, Membership{Role: RoleAdmin})
		case "documents":
			return assign(out, map[string]any{"grants": []json.RawMessage{rawAllow, rawDeny}})
		case "grant_documents":
			return assign(out, map[string]any{"grants": []json.RawMessage{rawAllow}})
		case "invite":
			invited = true
			return nil
		}
		return ErrInvalid
	}
	s := NewService(repo)
	_, err := s.Invite(context.Background(), "session", Grant{TenantID: "workspace", Role: RoleAdmin, PolicyIDs: []string{"custom-get"}})
	if !errors.Is(err, ErrForbidden) || invited {
		t.Fatalf("deny bypass: err=%v invited=%v", err, invited)
	}
	if repo.transactions != 1 {
		t.Fatal("grant checked outside transaction")
	}
}

func TestCapabilitiesAndAccountLifecycleKeepSecretsOutOfStorage(t *testing.T) {
	ctx := context.Background()
	rawGrant := json.RawMessage(`{"version":"1","statement":[{"effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}]}`)
	repo := &memoryRepository{}
	repo.call = func(op string, in, out any) error {
		p, _ := in.(map[string]any)
		if p != nil {
			if _, ok := p["token"]; ok {
				t.Fatal("capability token sent to storage")
			}
			if v, ok := p["token_hash"]; ok && len(v.(string)) != 64 {
				t.Fatal("invalid token digest")
			}
		}
		switch op {
		case "initialized":
			return assign(out, map[string]bool{"initialized": true})
		case "session_actor":
			return assign(out, User{ID: "alice", Username: "alice"})
		case "session_credential":
			return assign(out, map[string]any{"password_hash": dummyPasswordHash})
		case "membership":
			return assign(out, Membership{Role: RoleAdmin, PolicyIDs: []string{"grant"}})
		case "documents", "grant_documents":
			return assign(out, map[string]any{"grants": []json.RawMessage{rawGrant}})
		case "setup", "accept_invitation":
			return assign(out, User{ID: "alice", Username: "alice"})
		case "issue_installation", "invite", "issue_reset":
			return assign(out, map[string]any{"expires_at": time.Now().Add(time.Minute)})
		case "users":
			return assign(out, []User{{ID: "alice"}})
		case "sessions":
			return assign(out, []Session{{ID: "session-id"}})
		case "members":
			return assign(out, []Membership{{User: User{ID: "alice"}, Role: RoleAdmin, PolicyIDs: []string{"grant"}}})
		case "workspaces", "workspace_directory":
			return assign(out, []Workspace{{ID: "workspace"}})
		case "create_workspace":
			return assign(out, Workspace{ID: "created"})
		case "cleanup":
			return assign(out, map[string]int{"deleted": 3})
		}
		return nil
	}
	s := NewService(repo)
	if initialized, err := s.Initialized(ctx); err != nil || !initialized {
		t.Fatal(err)
	}
	cap, err := s.IssueInstallationCapability(ctx, "setup", "Alice", time.Minute)
	if err != nil || len(cap.Token) != 43 {
		t.Fatal(err)
	}
	if _, err = s.Setup(ctx, cap.Token, "Alice", "A test passphrase for OrbitJob 42", "Alice"); err != nil {
		t.Fatal(err)
	}
	invitation, err := s.Invite(ctx, "session-id", Grant{TenantID: "workspace", Role: RoleViewer, Username: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AcceptInvitation(ctx, "session-id", invitation.Token, "", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AcceptInvitation(ctx, "", invitation.Token, "bob", "A new passphrase for OrbitJob 45", "Bob"); err != nil {
		t.Fatal(err)
	}
	reset, err := s.IssuePasswordReset(ctx, "session-id", "alice", "workspace")
	if err != nil || len(reset.Token) != 43 {
		t.Fatal(err)
	}
	if err = s.ResetPassword(ctx, reset.Token, "Another passphrase for OrbitJob 69"); err != nil {
		t.Fatal(err)
	}
	if err = s.Logout(ctx, "session-id"); err != nil {
		t.Fatal(err)
	}
	if sessions, err := s.Sessions(ctx, "session-id"); err != nil || len(sessions) != 1 {
		t.Fatal(err)
	}
	if err = s.RevokeSession(ctx, "session-id", "other-session"); err != nil {
		t.Fatal(err)
	}
	if spaces, err := s.Workspaces(ctx, "alice"); err != nil || len(spaces) != 1 {
		t.Fatal(err)
	}
	if members, err := s.Members(ctx, "session-id", "workspace"); err != nil || len(members) != 1 {
		t.Fatal(err)
	}
	role := RoleViewer
	if err = s.UpdateMember(ctx, "session-id", "alice", "workspace", MemberChange{Role: &role}); err != nil {
		t.Fatal(err)
	}
	if users, err := s.Users(ctx, "session-id"); err != nil || len(users) != 1 {
		t.Fatal(err)
	}
	name := "New name"
	if err = s.UpdateUser(ctx, "session-id", "alice", UserChange{DisplayName: &name}); err != nil {
		t.Fatal(err)
	}
	if workspace, err := s.CreateWorkspace(ctx, "session-id", "Test", "test"); err != nil || workspace.ID != "created" {
		t.Fatal(err)
	}
	if spaces, err := s.WorkspaceDirectory(ctx, "session-id"); err != nil || len(spaces) != 1 {
		t.Fatal(err)
	}
	if n, err := s.Cleanup(ctx, 3); err != nil || n != 3 {
		t.Fatal(err)
	}
}
func TestEffectiveDocumentsPreserveBoundaryAndSelf(t *testing.T) {
	allow := json.RawMessage(`{"version":"1","statement":[{"effect":"Allow","action":["job:Get","job:Trigger"],"resource":["orbitjob:self:*:job/*"]}]}`)
	bound := json.RawMessage(`{"version":"1","statement":[{"effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}]}`)
	repo := &memoryRepository{call: func(_ string, _ any, out any) error {
		return assign(out, map[string]any{"grants": []json.RawMessage{allow}, "boundary": bound})
	}}
	s := NewService(repo)
	docs, err := s.EffectiveDocuments(context.Background(), "alice", "workspace")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		action, tenant string
		want           policy.Decision
	}{{"job:Get", "workspace", policy.DecisionAllow}, {"job:Trigger", "workspace", policy.DecisionDeny}, {"job:Get", "foreign", policy.DecisionDeny}} {
		got := policy.Evaluate(docs, policy.Request{Action: tc.action, Resource: policy.ARN{Tenant: tc.tenant, Group: "*", Type: "job", ID: "example"}})
		if got != tc.want {
			t.Fatalf("%+v decision=%v", tc, got)
		}
	}
}

func TestSelfPasswordChangeRequiresCurrentPasswordAndHashesReplacement(t *testing.T) {
	ctx := context.Background()
	current := "An existing OrbitJob account passphrase"
	replacement := "A replacement OrbitJob account passphrase"
	hash, err := HashPassword(current)
	if err != nil {
		t.Fatal(err)
	}
	mutations := 0
	repo := &memoryRepository{call: func(op string, in, out any) error {
		p := in.(map[string]any)
		if p["session_id"] != "session-id" {
			t.Fatal("current session scope missing")
		}
		switch op {
		case "session_credential":
			return assign(out, map[string]string{"password_hash": hash})
		case "change_password":
			mutations++
			if p["password_hash"] != hash {
				t.Fatal("verified hash missing from race guard")
			}
			newHash := p["new_password_hash"].(string)
			if newHash == replacement || newHash == hash {
				t.Fatal("replacement hash not fresh")
			}
			if err := VerifyPassword(replacement, newHash); err != nil {
				t.Fatal(err)
			}
			return nil
		}
		return ErrInvalid
	}}
	s := NewService(repo)
	if err = s.ChangePassword(ctx, "session-id", "wrong current password", replacement); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("wrong current password=%v", err)
	}
	if err = s.ChangePassword(ctx, "session-id", current, "passwordpassword"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("weak replacement=%v", err)
	}
	if mutations != 0 {
		t.Fatal("rejected password change reached storage mutation")
	}
	if err = s.ChangePassword(ctx, "session-id", current, replacement); err != nil {
		t.Fatal(err)
	}
	if mutations != 1 {
		t.Fatal("password change not persisted")
	}
}

func TestMemberReactivationRechecksRetainedGrants(t *testing.T) {
	getOnly := json.RawMessage(`{"version":"1","statement":[{"effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}]}`)
	getAndTrigger := json.RawMessage(`{"version":"1","statement":[{"effect":"Allow","action":["job:Get","job:Trigger"],"resource":["orbitjob:self:*:job/*"]}]}`)
	for _, tc := range []struct {
		name       string
		targetDocs json.RawMessage
		wantError  error
		bounded    bool
	}{
		{name: "retained grant exceeds caller", targetDocs: getAndTrigger, wantError: ErrForbidden},
		{name: "retained grant fits caller", targetDocs: getOnly},
		{name: "bounded caller cannot restore an unbounded binding", targetDocs: getOnly, bounded: true, wantError: ErrForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actorID, targetID, tenantID, sessionID := scan.GenerateID(), scan.GenerateID(), scan.GenerateID(), scan.GenerateID()
			callerPolicyID, targetPolicyID := scan.GenerateID(), scan.GenerateID()
			callerBoundaryID := ""
			if tc.bounded {
				callerBoundaryID = scan.GenerateID()
			}
			member := Membership{User: User{ID: targetID}, TenantID: tenantID, Role: RoleAdmin, Status: "disabled", PolicyIDs: []string{targetPolicyID}}
			mutations := 0
			repo := &memoryRepository{call: func(op string, input, output any) error {
				switch op {
				case "members":
					return assign(output, []Membership{member})
				case "session_actor":
					return assign(output, User{ID: actorID})
				case "membership":
					return assign(output, Membership{User: User{ID: actorID}, Role: RoleAdmin, PolicyIDs: []string{callerPolicyID}, BoundaryPolicyID: callerBoundaryID})
				case "documents":
					if tc.bounded {
						return assign(output, map[string]any{"grants": []json.RawMessage{getOnly}, "boundary": getOnly})
					}
					return assign(output, map[string]any{"grants": []json.RawMessage{getOnly}})
				case "grant_documents":
					params := input.(map[string]any)
					ids := params["policy_ids"].([]string)
					if len(ids) == 1 && ids[0] == callerPolicyID {
						return assign(output, map[string]any{"grants": []json.RawMessage{getOnly}, "boundary": getOnly})
					}
					if len(ids) != 1 || ids[0] != targetPolicyID {
						t.Fatal("reactivation checked a replacement role instead of the retained grant")
					}
					return assign(output, map[string]any{"grants": []json.RawMessage{tc.targetDocs}})
				case "update_member":
					mutations++
					member.Status = "active"
					return nil
				default:
					t.Fatalf("unexpected operation %s", op)
					return ErrInvalid
				}
			}}
			active := "active"
			err := NewService(repo).UpdateMember(context.Background(), sessionID, targetID, tenantID, MemberChange{Status: &active})
			if !errors.Is(err, tc.wantError) {
				t.Fatalf("reactivation error=%v want=%v", err, tc.wantError)
			}
			if tc.wantError != nil {
				if mutations != 0 || member.Status != "disabled" {
					t.Fatal("rejected reactivation changed the retained membership")
				}
			} else if mutations != 1 || member.Status != "active" {
				t.Fatal("grant within the caller's permissions could not be reactivated")
			}
			if repo.transactions != 1 {
				t.Fatal("reactivation was checked outside the mutation transaction")
			}
		})
	}
}

func TestDelegationCannotWidenAnInheritedBoundary(t *testing.T) {
	grant := json.RawMessage(`{"version":"1","statement":[{"effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}]}`)
	callerBoundary := grant
	widerBoundary := json.RawMessage(`{"version":"1","statement":[{"effect":"Allow","action":["job:Get","job:Trigger"],"resource":["orbitjob:self:*:job/*"]}]}`)
	narrowerBoundary := json.RawMessage(`{"version":"1","statement":[{"effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/one-job"]}]}`)
	for _, tc := range []struct {
		name      string
		requested string
		wantError error
	}{
		{name: "inherit caller ceiling"},
		{name: "narrow caller ceiling", requested: "narrow"},
		{name: "reject a wider ceiling despite a narrow current grant", requested: "wide", wantError: ErrForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actorID, tenantID, sessionID, grantID := scan.GenerateID(), scan.GenerateID(), scan.GenerateID(), scan.GenerateID()
			callerBoundaryID, narrowerBoundaryID, widerBoundaryID := scan.GenerateID(), scan.GenerateID(), scan.GenerateID()
			requestedID := map[string]string{"narrow": narrowerBoundaryID, "wide": widerBoundaryID}[tc.requested]
			mutations, persistedBoundary := 0, ""
			repo := &memoryRepository{call: func(op string, input, output any) error {
				switch op {
				case "session_actor":
					return assign(output, User{ID: actorID})
				case "membership":
					return assign(output, Membership{User: User{ID: actorID}, Role: RoleAdmin, PolicyIDs: []string{grantID}, BoundaryPolicyID: callerBoundaryID})
				case "documents":
					return assign(output, map[string]any{"grants": []json.RawMessage{grant}, "boundary": callerBoundary})
				case "grant_documents":
					params := input.(map[string]any)
					var bound json.RawMessage
					switch params["boundary_policy_id"] {
					case callerBoundaryID:
						bound = callerBoundary
					case narrowerBoundaryID:
						bound = narrowerBoundary
					case widerBoundaryID:
						bound = widerBoundary
					default:
						t.Fatal("delegation discarded the caller's boundary")
					}
					return assign(output, map[string]any{"grants": []json.RawMessage{grant}, "boundary": bound})
				case "invite":
					mutations++
					persistedBoundary = input.(map[string]any)["grant"].(Grant).BoundaryPolicyID
					return assign(output, map[string]any{"expires_at": time.Now().Add(time.Hour)})
				default:
					t.Fatalf("unexpected operation %s", op)
					return ErrInvalid
				}
			}}
			_, err := NewService(repo).Invite(context.Background(), sessionID, Grant{TenantID: tenantID, Role: RoleViewer, PolicyIDs: []string{grantID}, BoundaryPolicyID: requestedID})
			if !errors.Is(err, tc.wantError) {
				t.Fatalf("delegation error=%v want=%v", err, tc.wantError)
			}
			if tc.wantError != nil {
				if mutations != 0 {
					t.Fatal("rejected widened ceiling reached the mutation")
				}
			} else {
				want := requestedID
				if want == "" {
					want = callerBoundaryID
				}
				if mutations != 1 || persistedBoundary != want {
					t.Fatalf("persisted boundary=%q want=%q mutations=%d", persistedBoundary, want, mutations)
				}
			}
		})
	}
}
