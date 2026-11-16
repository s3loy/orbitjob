package account

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"orbitjob/internal/platform/scan"
)

func TestMemberEmptyPolicyEditPersistsExactlyTheValidatedPreset(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		reactivate   bool
		bounded      bool
		deny         bool
	}{
		{name: "active member", status: "active"},
		{name: "disabled member", status: "disabled"},
		{name: "reactivate broader saved grant", status: "disabled", reactivate: true},
		{name: "reactivate with caller boundary", status: "disabled", reactivate: true, bounded: true},
		{name: "caller deny remains enforced", status: "disabled", reactivate: true, deny: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tenantID, actorID, targetID, sessionID := scan.GenerateID(), scan.GenerateID(), scan.GenerateID(), scan.GenerateID()
			savedPolicyID, boundaryID := scan.GenerateID(), scan.GenerateID()
			presetIDs, err := rolePolicies(RoleViewer)
			if err != nil {
				t.Fatal(err)
			}
			narrow := json.RawMessage(`{"version":"1","statement":[{"effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}]}`)
			denied := json.RawMessage(`{"version":"1","statement":[{"effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]},{"effect":"Deny","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}]}`)
			checked, writes := 0, 0
			repo := &memberTransactionRepository{t: t}
			repo.call = func(operation string, input, output any) error {
				switch operation {
				case "members":
					return assign(output, []Membership{{User: User{ID: targetID}, TenantID: tenantID, Role: RoleViewer, Status: tc.status, PolicyIDs: []string{savedPolicyID}}})
				case "session_actor":
					return assign(output, User{ID: actorID})
				case "membership":
					membership := Membership{Role: RoleAdmin, PolicyIDs: presetIDs}
					if tc.bounded {
						membership.BoundaryPolicyID = boundaryID
					}
					return assign(output, membership)
				case "documents":
					document := narrow
					if tc.deny {
						document = denied
					}
					var boundary any
					if tc.bounded {
						boundary = narrow
					}
					return assign(output, map[string]any{"grants": []json.RawMessage{document}, "boundary": boundary})
				case "grant_documents":
					params := input.(map[string]any)
					if !reflect.DeepEqual(params["policy_ids"], presetIDs) {
						t.Fatalf("containment checked unexpected policies: %v", params["policy_ids"])
					}
					checked++
					var boundary any
					if params["boundary_policy_id"] == boundaryID {
						boundary = narrow
					}
					return assign(output, map[string]any{"grants": []json.RawMessage{narrow}, "boundary": boundary})
				case "update_member":
					params := input.(map[string]any)
					encoded, err := json.Marshal(params["change"])
					if err != nil {
						return err
					}
					var persisted MemberChange
					if err := json.Unmarshal(encoded, &persisted); err != nil {
						return err
					}
					if !reflect.DeepEqual(persisted.PolicyIDs, presetIDs) {
						t.Fatalf("database would retain unvalidated saved policies: change=%s", encoded)
					}
					if persisted.BoundaryPolicyID == nil || (tc.bounded && *persisted.BoundaryPolicyID != boundaryID) {
						t.Fatalf("database change lost checked boundary: %s", encoded)
					}
					if tc.reactivate && (persisted.Status == nil || *persisted.Status != "active") {
						t.Fatalf("database change lost reactivation: %s", encoded)
					}
					writes++
					return nil
				default:
					t.Fatalf("unexpected operation %s", operation)
					return nil
				}
			}
			change := MemberChange{PolicyIDs: []string{}}
			if tc.reactivate {
				active := "active"
				change.Status = &active
			}
			err = NewService(repo).UpdateMember(context.Background(), sessionID, targetID, tenantID, change)
			if tc.deny {
				if !errors.Is(err, ErrForbidden) || writes != 0 || repo.rollbacks != 1 {
					t.Fatalf("denied grant edit: err=%v writes=%d rollbacks=%d", err, writes, repo.rollbacks)
				}
			} else if err != nil || checked == 0 || writes != 1 || repo.commits != 1 {
				t.Fatalf("grant edit: err=%v checked=%d writes=%d commits=%d", err, checked, writes, repo.commits)
			}
		})
	}
}
