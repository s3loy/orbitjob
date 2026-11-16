package account

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/core/domain/tenant"
	"orbitjob/internal/platform/scan"
)

type Service struct {
	repository Repository
	now        func() time.Time
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository, now: time.Now}
}
func identifier() string { return scan.GenerateID() }
func secret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func csrf(s string) string   { return digest("orbitjob-session-csrf:" + s) }

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`)

func NormalizeUsername(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if !usernamePattern.MatchString(s) {
		return "", fmt.Errorf("%w: username must contain 3-64 ASCII letters, digits, dots, hyphens or underscores", ErrInvalid)
	}
	return s, nil
}
func displayName(s string) error {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > 128 {
		return fmt.Errorf("%w: display name exceeds 128 characters", ErrInvalid)
	}
	return nil
}
func (s *Service) Initialized(ctx context.Context) (bool, error) {
	var out struct {
		Initialized bool `json:"initialized"`
	}
	err := s.repository.Call(ctx, "initialized", nil, &out)
	return out.Initialized, err
}
func (s *Service) Authenticate(ctx context.Context, token string) (*Session, error) {
	if len(token) != 43 {
		return nil, ErrUnauthenticated
	}
	var out Session
	if err := s.repository.Call(ctx, "authenticate", map[string]any{"token_hash": digest(token)}, &out); err != nil {
		return nil, err
	}
	out.CSRFToken = csrf(token)
	return &out, nil
}
func (s *Service) Login(ctx context.Context, username, password string, remember bool) (Session, string, error) {
	username, err := NormalizeUsername(username)
	if err != nil {
		return Session{}, "", ErrUnauthenticated
	}
	var credential struct {
		User         User   `json:"user"`
		PasswordHash string `json:"password_hash"`
	}
	if err = s.repository.Call(ctx, "credential", map[string]any{"username": username}, &credential); err != nil {
		if err == ErrUnauthenticated {
			_ = VerifyPassword(password, dummyPasswordHash)
		}
		return Session{}, "", err
	}
	if err = VerifyPassword(password, credential.PasswordHash); err != nil {
		return Session{}, "", ErrUnauthenticated
	}
	token, err := secret()
	if err != nil {
		return Session{}, "", err
	}
	life := 8 * time.Hour
	if remember {
		life = 7 * 24 * time.Hour
	}
	var out Session
	err = s.repository.Call(ctx, "login", map[string]any{"id": identifier(), "user_id": credential.User.ID, "password_hash": credential.PasswordHash, "token_hash": digest(token), "remember": remember, "expires_at": s.now().Add(life)}, &out)
	if err != nil {
		return Session{}, "", err
	}
	out.CSRFToken = csrf(token)
	return out, token, nil
}
func (s *Service) Reauthenticate(ctx context.Context, sessionID, password string) error {
	var credential struct {
		PasswordHash string `json:"password_hash"`
	}
	if err := s.repository.Call(ctx, "session_credential", map[string]any{"session_id": sessionID}, &credential); err != nil {
		return err
	}
	if err := VerifyPassword(password, credential.PasswordHash); err != nil {
		return ErrUnauthenticated
	}
	return s.repository.Call(ctx, "reauth", map[string]any{"session_id": sessionID, "password_hash": credential.PasswordHash}, nil)
}
func (s *Service) Logout(ctx context.Context, sessionID string) error {
	return s.repository.Call(ctx, "logout", map[string]any{"session_id": sessionID}, nil)
}
func (s *Service) Sessions(ctx context.Context, sessionID string) ([]Session, error) {
	out := []Session{}
	err := s.repository.Call(ctx, "sessions", map[string]any{"session_id": sessionID}, &out)
	return out, err
}
func (s *Service) RevokeSession(ctx context.Context, sessionID, targetID string) error {
	return s.repository.Call(ctx, "revoke_session", map[string]any{"session_id": sessionID, "target_id": targetID}, nil)
}
func (s *Service) Workspaces(ctx context.Context, userID string) ([]Workspace, error) {
	out := []Workspace{}
	err := s.repository.Call(ctx, "workspaces", map[string]any{"user_id": userID}, &out)
	return out, err
}
func (s *Service) Membership(ctx context.Context, userID, tenantID string) (Membership, error) {
	var out Membership
	err := s.repository.Call(ctx, "membership", map[string]any{"user_id": userID, "tenant_id": tenantID}, &out)
	return out, err
}
func (s *Service) EffectiveDocuments(ctx context.Context, userID, tenantID string) ([]policy.Document, error) {
	var out struct {
		Grants   []json.RawMessage `json:"grants"`
		Boundary json.RawMessage   `json:"boundary"`
	}
	if err := s.repository.Call(ctx, "documents", map[string]any{"user_id": userID, "tenant_id": tenantID}, &out); err != nil {
		return nil, err
	}
	docs := make([]policy.Document, 0, len(out.Grants))
	for _, raw := range out.Grants {
		doc, err := policy.ParseDocument(raw)
		if err != nil {
			return nil, err
		}
		docs = append(docs, policy.ResolveSelf(doc, tenantID))
	}
	if len(out.Boundary) > 0 && string(out.Boundary) != "null" {
		bound, err := policy.ParseDocument(out.Boundary)
		if err != nil {
			return nil, err
		}
		docs = policy.Intersect(docs, policy.ResolveSelf(bound, tenantID))
	}
	return docs, nil
}
func (s *Service) Setup(ctx context.Context, token, username, password, name string) (User, error) {
	if len(token) != 43 {
		return User{}, ErrInvalid
	}
	username, err := NormalizeUsername(username)
	if err != nil {
		return User{}, err
	}
	if err = displayName(name); err != nil {
		return User{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	var out User
	err = s.repository.Call(ctx, "setup", map[string]any{"token_hash": digest(token), "username": username, "password_hash": hash, "display_name": name, "id": identifier()}, &out)
	return out, err
}
func (s *Service) IssueInstallationCapability(ctx context.Context, kind, username string, lifetime time.Duration) (Capability, error) {
	if kind != "setup" && kind != "recovery" {
		return Capability{}, ErrInvalid
	}
	if lifetime <= 0 || lifetime > time.Hour {
		return Capability{}, ErrInvalid
	}
	var err error
	if username != "" {
		username, err = NormalizeUsername(username)
		if err != nil {
			return Capability{}, err
		}
	}
	token, err := secret()
	if err != nil {
		return Capability{}, err
	}
	out := Capability{Token: token, ID: identifier(), ExpiresAt: s.now().Add(lifetime)}
	err = s.repository.Call(ctx, "issue_installation", map[string]any{"id": out.ID, "token_hash": digest(token), "kind": kind, "username": username, "lifetime_seconds": int(lifetime.Seconds())}, &out)
	return out, err
}
func rolePolicies(role string) ([]string, error) {
	switch role {
	case RoleAdmin:
		return []string{"00000000000000000000000011"}, nil
	case RoleOperator:
		return []string{"00000000000000000000000013"}, nil
	case RoleViewer:
		return []string{"00000000000000000000000012"}, nil
	}
	return nil, ErrInvalid
}
func (s *Service) GrantDocuments(ctx context.Context, grant Grant) ([]policy.Document, *policy.Document, error) {
	ids := grant.PolicyIDs
	var err error
	if len(ids) == 0 {
		ids, err = rolePolicies(grant.Role)
		if err != nil {
			return nil, nil, err
		}
	}
	var out struct {
		Grants   []json.RawMessage `json:"grants"`
		Boundary json.RawMessage   `json:"boundary"`
	}
	err = s.repository.Call(ctx, "grant_documents", map[string]any{"tenant_id": grant.TenantID, "policy_ids": ids, "boundary_policy_id": grant.BoundaryPolicyID}, &out)
	if err != nil {
		return nil, nil, err
	}
	docs := []policy.Document{}
	for _, raw := range out.Grants {
		doc, e := policy.ParseDocument(raw)
		if e != nil {
			return nil, nil, e
		}
		docs = append(docs, policy.ResolveSelf(doc, grant.TenantID))
	}
	var boundary *policy.Document
	if len(out.Boundary) > 0 && string(out.Boundary) != "null" {
		doc, e := policy.ParseDocument(out.Boundary)
		if e != nil {
			return nil, nil, e
		}
		doc = policy.ResolveSelf(doc, grant.TenantID)
		boundary = &doc
	}
	return docs, boundary, nil
}
func (s *Service) Invite(ctx context.Context, sessionID string, grant Grant) (Capability, error) {
	var out Capability
	err := s.repository.Transaction(ctx, func(repository Repository) error {
		scoped := &Service{repository: repository, now: s.now}
		var err error
		out, err = scoped.invite(ctx, sessionID, grant)
		return err
	})
	return out, err
}
func (s *Service) invite(ctx context.Context, sessionID string, grant Grant) (Capability, error) {
	if grant.Username != "" {
		var err error
		grant.Username, err = NormalizeUsername(grant.Username)
		if err != nil {
			return Capability{}, err
		}
	}
	if len(grant.PolicyIDs) == 0 {
		var err error
		grant.PolicyIDs, err = rolePolicies(grant.Role)
		if err != nil {
			return Capability{}, err
		}
	}
	if _, err := rolePolicies(grant.Role); err != nil {
		return Capability{}, err
	}
	token, err := secret()
	if err != nil {
		return Capability{}, err
	}
	out := Capability{Token: token, ID: identifier(), ExpiresAt: s.now().Add(24 * time.Hour)}
	if err = s.checkGrant(ctx, sessionID, &grant); err != nil {
		return Capability{}, err
	}
	err = s.repository.Call(ctx, "invite", map[string]any{"session_id": sessionID, "tenant_id": grant.TenantID, "grant": grant, "id": out.ID, "token_hash": digest(token)}, &out)
	return out, err
}
func (s *Service) AcceptInvitation(ctx context.Context, sessionID, token, username, password, name string) (User, error) {
	if len(token) != 43 {
		return User{}, ErrInvalid
	}
	var err error
	hash := ""
	if sessionID != "" {
		var actor User
		if err = s.repository.Call(ctx, "session_actor", map[string]any{"session_id": sessionID}, &actor); err != nil {
			return User{}, err
		}
		if username != "" {
			username, err = NormalizeUsername(username)
			if err != nil {
				return User{}, err
			}
			if username != actor.Username {
				return User{}, ErrUnauthenticated
			}
		}
		username = actor.Username
	}
	if username != "" {
		username, err = NormalizeUsername(username)
		if err != nil {
			return User{}, err
		}
	}
	if password != "" {
		hash, err = HashPassword(password)
		if err != nil {
			return User{}, err
		}
	}
	if err = displayName(name); err != nil {
		return User{}, err
	}
	var out User
	err = s.repository.Call(ctx, "accept_invitation", map[string]any{"session_id": sessionID, "token_hash": digest(token), "username": username, "password_hash": hash, "display_name": name, "id": identifier()}, &out)
	return out, err
}

// ChangePassword requires the current password and revokes every existing
// session. The caller must authenticate again after a successful change.
func (s *Service) ChangePassword(ctx context.Context, sessionID, currentPassword, newPassword string) error {
	var credential struct {
		PasswordHash string `json:"password_hash"`
	}
	if err := s.repository.Call(ctx, "session_credential", map[string]any{"session_id": sessionID}, &credential); err != nil {
		return err
	}
	if err := VerifyPassword(currentPassword, credential.PasswordHash); err != nil {
		return ErrUnauthenticated
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	return s.repository.Call(ctx, "change_password", map[string]any{"session_id": sessionID, "password_hash": credential.PasswordHash, "new_password_hash": hash}, nil)
}
func (s *Service) ResetPassword(ctx context.Context, token, password string) error {
	if len(token) != 43 {
		return ErrInvalid
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return s.repository.Call(ctx, "reset_password", map[string]any{"token_hash": digest(token), "password_hash": hash}, nil)
}
func (s *Service) IssuePasswordReset(ctx context.Context, sessionID, targetID, tenantID string) (Capability, error) {
	token, err := secret()
	if err != nil {
		return Capability{}, err
	}
	out := Capability{Token: token, ID: identifier(), ExpiresAt: s.now().Add(time.Hour)}
	err = s.repository.Call(ctx, "issue_reset", map[string]any{"session_id": sessionID, "target_id": targetID, "tenant_id": tenantID, "token_hash": digest(token), "id": out.ID}, &out)
	return out, err
}
func (s *Service) Members(ctx context.Context, sessionID, tenantID string) ([]Membership, error) {
	out := []Membership{}
	err := s.repository.Call(ctx, "members", map[string]any{"session_id": sessionID, "tenant_id": tenantID}, &out)
	return out, err
}
func (s *Service) UpdateMember(ctx context.Context, sessionID, userID, tenantID string, change MemberChange) error {
	return s.repository.Transaction(ctx, func(repository Repository) error {
		scoped := &Service{repository: repository, now: s.now}
		return scoped.updateMember(ctx, sessionID, userID, tenantID, change)
	})
}
func (s *Service) updateMember(ctx context.Context, sessionID, userID, tenantID string, change MemberChange) error {
	if change.Status != nil && *change.Status != "active" && *change.Status != "disabled" {
		return ErrInvalid
	}
	grantChanged := change.Role != nil || change.PolicyIDs != nil || change.BoundaryPolicyID != nil
	if grantChanged || (change.Status != nil && *change.Status == "active") {
		members, err := s.Members(ctx, sessionID, tenantID)
		if err != nil {
			return err
		}
		var member Membership
		for _, candidate := range members {
			if candidate.User.ID == userID {
				member = candidate
				break
			}
		}
		if member.User.ID == "" {
			return ErrInvalid
		}
		grant := Grant{TenantID: tenantID, Role: member.Role, PolicyIDs: member.PolicyIDs, BoundaryPolicyID: member.BoundaryPolicyID}
		if change.Role != nil {
			grant.Role = *change.Role
			grant.PolicyIDs, err = rolePolicies(grant.Role)
			if err != nil {
				return err
			}
		}
		if change.PolicyIDs != nil {
			grant.PolicyIDs = change.PolicyIDs
		}
		if change.BoundaryPolicyID != nil {
			grant.BoundaryPolicyID = *change.BoundaryPolicyID
		}
		if grantChanged {
			if err = s.checkGrant(ctx, sessionID, &grant); err != nil {
				return err
			}
			change.PolicyIDs = grant.PolicyIDs
			change.BoundaryPolicyID = &grant.BoundaryPolicyID
		} else if member.Status == "disabled" {
			actor, membership, err := s.grantAuthority(ctx, sessionID, tenantID)
			if err != nil {
				return err
			}
			if err = s.validateGrantContainment(ctx, actor, membership, grant); err != nil {
				return err
			}
		}
	}
	return s.repository.Call(ctx, "update_member", map[string]any{"session_id": sessionID, "target_id": userID, "tenant_id": tenantID, "change": change}, nil)
}
func (s *Service) Users(ctx context.Context, sessionID string) ([]User, error) {
	out := []User{}
	err := s.repository.Call(ctx, "users", map[string]any{"session_id": sessionID}, &out)
	return out, err
}
func (s *Service) UpdateUser(ctx context.Context, sessionID, userID string, change UserChange) error {
	if change.Status != nil && *change.Status != "active" && *change.Status != "disabled" {
		return ErrInvalid
	}
	if change.DisplayName != nil {
		if err := displayName(*change.DisplayName); err != nil {
			return err
		}
	}
	return s.repository.Call(ctx, "update_user", map[string]any{"session_id": sessionID, "target_id": userID, "change": change}, nil)
}
func (s *Service) Cleanup(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 1000 {
		return 0, ErrInvalid
	}
	var out struct {
		Deleted int `json:"deleted"`
	}
	err := s.repository.Call(ctx, "cleanup", map[string]any{"limit": limit}, &out)
	return out.Deleted, err
}

func (s *Service) grantAuthority(ctx context.Context, sessionID, tenantID string) (User, Membership, error) {
	var actor User
	if err := s.repository.Call(ctx, "session_actor", map[string]any{"session_id": sessionID}, &actor); err != nil {
		return User{}, Membership{}, err
	}
	membership, err := s.Membership(ctx, actor.ID, tenantID)
	if err != nil {
		return User{}, Membership{}, err
	}
	if membership.Role != RoleAdmin {
		return User{}, Membership{}, ErrForbidden
	}
	return actor, membership, nil
}

func (s *Service) checkGrant(ctx context.Context, sessionID string, grant *Grant) error {
	if len(grant.PolicyIDs) == 0 {
		var err error
		grant.PolicyIDs, err = rolePolicies(grant.Role)
		if err != nil {
			return err
		}
	}
	actor, membership, err := s.grantAuthority(ctx, sessionID, grant.TenantID)
	if err != nil {
		return err
	}
	if grant.BoundaryPolicyID == "" {
		grant.BoundaryPolicyID = membership.BoundaryPolicyID
	}
	return s.validateGrantContainment(ctx, actor, membership, *grant)
}

func (s *Service) validateGrantContainment(ctx context.Context, actor User, membership Membership, grant Grant) error {
	caller, err := s.EffectiveDocuments(ctx, actor.ID, grant.TenantID)
	if err != nil {
		return err
	}
	granted, boundary, err := s.GrantDocuments(ctx, grant)
	if err != nil {
		return err
	}
	if membership.BoundaryPolicyID != "" {
		if grant.BoundaryPolicyID == "" {
			return ErrForbidden
		}
		_, callerBoundary, err := s.GrantDocuments(ctx, Grant{TenantID: grant.TenantID, PolicyIDs: membership.PolicyIDs, BoundaryPolicyID: membership.BoundaryPolicyID})
		if err != nil {
			return err
		}
		if _, err = policy.PropagateBoundary(callerBoundary, boundary); err != nil {
			return ErrForbidden
		}
	}
	if boundary != nil {
		granted = policy.Intersect(granted, *boundary)
	}
	if err = policy.CheckGrantable(caller, granted); err != nil {
		return ErrForbidden
	}
	return nil
}
func (s *Service) CreateWorkspace(ctx context.Context, sessionID, name, slug string) (Workspace, error) {
	normalized, err := tenant.NormalizeCreateTenant(tenant.CreateTenantInput{Name: name, Slug: slug})
	if err != nil {
		return Workspace{}, err
	}
	t := tenant.Tenant{ID: identifier(), Name: normalized.Name, Slug: normalized.Slug, Status: tenant.StatusActive}
	if err = t.Validate(); err != nil {
		return Workspace{}, err
	}
	var out Workspace
	err = s.repository.Call(ctx, "create_workspace", map[string]any{"session_id": sessionID, "id": t.ID, "name": t.Name, "slug": t.Slug}, &out)
	return out, err
}
func (s *Service) WorkspaceDirectory(ctx context.Context, sessionID string) ([]Workspace, error) {
	out := []Workspace{}
	err := s.repository.Call(ctx, "workspace_directory", map[string]any{"session_id": sessionID}, &out)
	return out, err
}
