package account

import (
	"context"
	"errors"
	"strings"
	"testing"

	"orbitjob/internal/platform/scan"
)

func TestAcceptInvitationBindsEveryAuthenticatedRequestToSessionActor(t *testing.T) {
	for _, tc := range []struct {
		name, username string
		actorErr       error
		wantErr        error
	}{
		{name: "token only"},
		{name: "matching username", username: "alice"},
		{name: "normalized username", username: " Alice "},
		{name: "conflicting username", username: "mallory", wantErr: ErrUnauthenticated},
		{name: "invalid username", username: "a", wantErr: ErrInvalid},
		{name: "revoked session with username", username: "alice", actorErr: ErrUnauthenticated, wantErr: ErrUnauthenticated},
		{name: "revoked session without username", actorErr: ErrUnauthenticated, wantErr: ErrUnauthenticated},
		{name: "store unavailable", actorErr: errors.New("offline")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actor := User{ID: scan.GenerateID(), Username: "alice"}
			sessionID := scan.GenerateID()
			token := strings.Repeat("a", 43)
			actorCalls, acceptCalls := 0, 0
			repo := &memoryRepository{call: func(op string, in, out any) error {
				params := in.(map[string]any)
				switch op {
				case "session_actor":
					actorCalls++
					if params["session_id"] != sessionID {
						t.Fatal("actor lookup used a different session")
					}
					if tc.actorErr != nil {
						return tc.actorErr
					}
					return assign(out, actor)
				case "accept_invitation":
					acceptCalls++
					if params["session_id"] != sessionID || params["username"] != actor.Username {
						t.Fatal("invitation acceptance escaped session identity")
					}
					if params["password_hash"] != "" || params["token_hash"] != digest(token) {
						t.Fatal("token-only acceptance changed credentials or capability")
					}
					return assign(out, actor)
				default:
					t.Fatalf("unexpected operation %s", op)
					return nil
				}
			}}
			got, err := NewService(repo).AcceptInvitation(context.Background(), sessionID, token, tc.username, "", "")
			wantErr := tc.wantErr
			if tc.actorErr != nil {
				wantErr = tc.actorErr
			}
			if !errors.Is(err, wantErr) {
				t.Fatalf("error=%v want=%v", err, wantErr)
			}
			if actorCalls != 1 || (wantErr == nil && (acceptCalls != 1 || got.ID != actor.ID)) || (wantErr != nil && acceptCalls != 0) {
				t.Fatalf("actor calls=%d accept calls=%d user=%+v", actorCalls, acceptCalls, got)
			}
		})
	}
}

func TestAcceptInvitationPreservesAnonymousAccountCreation(t *testing.T) {
	password := "An invitation passphrase for OrbitJob 42"
	repo := &memoryRepository{call: func(op string, in, out any) error {
		if op != "accept_invitation" {
			t.Fatalf("anonymous creation called %s", op)
		}
		params := in.(map[string]any)
		if params["session_id"] != "" || params["username"] != "new-user" || params["display_name"] != "New User" {
			t.Fatal("anonymous account input changed")
		}
		if err := VerifyPassword(password, params["password_hash"].(string)); err != nil {
			t.Fatal("new account password was not hashed")
		}
		return assign(out, User{ID: params["id"].(string), Username: "new-user"})
	}}
	user, err := NewService(repo).AcceptInvitation(context.Background(), "", strings.Repeat("a", 43), " New-User ", password, "New User")
	if err != nil || user.Username != "new-user" {
		t.Fatalf("user=%+v err=%v", user, err)
	}
}
