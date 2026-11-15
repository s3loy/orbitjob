package account

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"orbitjob/internal/platform/scan"
)

type memberTransactionRepository struct {
	t                  *testing.T
	call               func(string, any, any) error
	inTransaction      bool
	commits, rollbacks int
}

func (r *memberTransactionRepository) Call(_ context.Context, operation string, input, output any) error {
	r.t.Helper()
	if !r.inTransaction {
		r.t.Fatalf("member operation %s escaped its transaction", operation)
	}
	return r.call(operation, input, output)
}

func (r *memberTransactionRepository) Transaction(ctx context.Context, fn func(Repository) error) error {
	r.t.Helper()
	if r.inTransaction {
		r.t.Fatal("member update opened a nested transaction")
	}
	r.inTransaction = true
	defer func() { r.inTransaction = false }()
	if err := fn(r); err != nil {
		r.rollbacks++
		return err
	}
	r.commits++
	return nil
}

func TestMemberStatusChangesValidateSavedGrantWithoutChangingIt(t *testing.T) {
	for _, tc := range []struct {
		name, status, next, failOperation string
		wide, bounded, checkAuthority     bool
		unboundedActor, narrowActor       bool
		wantErr                           error
	}{
		{name: "wider saved grant", status: "disabled", next: "active", wide: true, checkAuthority: true, wantErr: ErrForbidden},
		{name: "wider saved grant with valid boundary", status: "disabled", next: "active", wide: true, bounded: true, narrowActor: true, checkAuthority: true, wantErr: ErrForbidden},
		{name: "bounded caller cannot restore missing boundary", status: "disabled", next: "active", checkAuthority: true, wantErr: ErrForbidden},
		{name: "unbounded caller restores missing boundary", status: "disabled", next: "active", checkAuthority: true, unboundedActor: true},
		{name: "same scope with saved boundary", status: "disabled", next: "active", wide: true, bounded: true, checkAuthority: true},
		{name: "already active", status: "active", next: "active", wide: true},
		{name: "disable wider member", status: "active", next: "disabled", wide: true},
		{name: "membership read failure", status: "disabled", next: "active", failOperation: "members", wantErr: ErrConflict},
		{name: "permission read failure", status: "disabled", next: "active", checkAuthority: true, failOperation: "documents", wantErr: ErrConflict},
		{name: "write failure", status: "disabled", next: "active", bounded: true, checkAuthority: true, failOperation: "update_member", wantErr: ErrConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tenantID, actorID, targetID, sessionID := scan.GenerateID(), scan.GenerateID(), scan.GenerateID(), scan.GenerateID()
			wideID, narrowID, ceilingID := scan.GenerateID(), scan.GenerateID(), scan.GenerateID()
			wide := json.RawMessage(`{"version":"1","statement":[{"effect":"Allow","action":["job:Get","job:Trigger"],"resource":["orbitjob:self:*:job/*"]}]}`)
			narrow := json.RawMessage(`{"version":"1","statement":[{"effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}]}`)
			member := Membership{User: User{ID: targetID}, TenantID: tenantID, Role: RoleViewer, Status: tc.status, PolicyIDs: []string{narrowID}}
			if tc.wide {
				member.PolicyIDs = []string{wideID}
			}
			if tc.bounded {
				member.BoundaryPolicyID = ceilingID
			}
			actorCalls, writes := 0, 0
			repo := &memberTransactionRepository{t: t}
			repo.call = func(operation string, input, output any) error {
				if operation == tc.failOperation {
					return ErrConflict
				}
				switch operation {
				case "members":
					return assign(output, []Membership{member})
				case "session_actor":
					actorCalls++
					return assign(output, User{ID: actorID})
				case "membership":
					membership := Membership{Role: RoleAdmin, PolicyIDs: []string{wideID}, BoundaryPolicyID: ceilingID}
					if tc.narrowActor {
						membership.PolicyIDs = []string{narrowID}
					}
					if tc.unboundedActor {
						membership.BoundaryPolicyID = ""
					}
					return assign(output, membership)
				case "documents":
					var boundary any = narrow
					grant := wide
					if tc.narrowActor {
						grant, boundary = narrow, wide
					}
					if tc.unboundedActor {
						boundary = nil
					}
					return assign(output, map[string]any{"grants": []json.RawMessage{grant}, "boundary": boundary})
				case "grant_documents":
					params := input.(map[string]any)
					grant := narrow
					if params["policy_ids"].([]string)[0] == wideID {
						grant = wide
					}
					var boundary any
					if params["boundary_policy_id"] == ceilingID {
						boundary = narrow
						if tc.narrowActor {
							boundary = wide
						}
					}
					return assign(output, map[string]any{"grants": []json.RawMessage{grant}, "boundary": boundary})
				case "update_member":
					writes++
					params := input.(map[string]any)
					change := params["change"].(MemberChange)
					if params["target_id"] != targetID || params["tenant_id"] != tenantID || params["session_id"] != sessionID {
						t.Fatal("member write lost trusted scope")
					}
					if change.Status == nil || *change.Status != tc.next || change.Role != nil || change.PolicyIDs != nil || change.BoundaryPolicyID != nil {
						t.Fatal("status-only mutation rewrote saved grants")
					}
					return nil
				default:
					t.Fatalf("unexpected operation %s", operation)
					return nil
				}
			}
			err := NewService(repo).UpdateMember(context.Background(), sessionID, targetID, tenantID, MemberChange{Status: &tc.next})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error=%v want=%v", err, tc.wantErr)
			}
			if (actorCalls > 0) != tc.checkAuthority {
				t.Fatalf("authority checks=%d want=%v", actorCalls, tc.checkAuthority)
			}
			if tc.wantErr == nil && (repo.commits != 1 || repo.rollbacks != 0 || writes != 1) {
				t.Fatalf("commits=%d rollbacks=%d writes=%d", repo.commits, repo.rollbacks, writes)
			}
			if tc.wantErr != nil && (repo.commits != 0 || repo.rollbacks != 1 || writes != 0) {
				t.Fatalf("failed grant committed: commits=%d rollbacks=%d writes=%d", repo.commits, repo.rollbacks, writes)
			}
		})
	}
}

func TestMemberGrantEditsAndInvitationsStillInheritCallerBoundary(t *testing.T) {
	for _, invitation := range []bool{false, true} {
		t.Run(map[bool]string{false: "member edit", true: "invitation"}[invitation], func(t *testing.T) {
			tenantID, actorID, targetID, sessionID := scan.GenerateID(), scan.GenerateID(), scan.GenerateID(), scan.GenerateID()
			policyID, ceilingID := scan.GenerateID(), scan.GenerateID()
			document := json.RawMessage(`{"version":"1","statement":[{"effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}]}`)
			writes := 0
			repo := &memberTransactionRepository{t: t}
			repo.call = func(operation string, input, output any) error {
				switch operation {
				case "members":
					return assign(output, []Membership{{User: User{ID: targetID}, Role: RoleViewer, PolicyIDs: []string{policyID}, Status: "active"}})
				case "session_actor":
					return assign(output, User{ID: actorID})
				case "membership":
					return assign(output, Membership{Role: RoleAdmin, PolicyIDs: []string{policyID}, BoundaryPolicyID: ceilingID})
				case "documents", "grant_documents":
					return assign(output, map[string]any{"grants": []json.RawMessage{document}, "boundary": document})
				case "update_member":
					change := input.(map[string]any)["change"].(MemberChange)
					if change.BoundaryPolicyID == nil || *change.BoundaryPolicyID != ceilingID || !reflect.DeepEqual(change.PolicyIDs, []string{policyID}) {
						t.Fatal("grant edit did not save inherited boundary")
					}
					writes++
					return nil
				case "invite":
					grant := input.(map[string]any)["grant"].(Grant)
					if grant.BoundaryPolicyID != ceilingID {
						t.Fatal("invitation did not inherit caller boundary")
					}
					writes++
					return nil
				default:
					t.Fatalf("unexpected operation %s", operation)
					return nil
				}
			}
			s := NewService(repo)
			var err error
			if invitation {
				_, err = s.Invite(context.Background(), sessionID, Grant{TenantID: tenantID, Role: RoleViewer, PolicyIDs: []string{policyID}})
			} else {
				err = s.UpdateMember(context.Background(), sessionID, targetID, tenantID, MemberChange{PolicyIDs: []string{policyID}})
			}
			if err != nil || writes != 1 || repo.commits != 1 {
				t.Fatalf("error=%v writes=%d commits=%d", err, writes, repo.commits)
			}
		})
	}
}
