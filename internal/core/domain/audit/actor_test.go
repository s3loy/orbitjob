package audit

import (
	"context"
	"testing"
)

func TestActorTypeRequiresVerifiedHumanIdentityMatch(t *testing.T) {
	for _, tc := range []struct {
		name, verifiedID, kind, writtenID, want string
	}{
		{"matching human", "user-id", "user", "user-id", "user"},
		{"different actor", "user-id", "user", "key-id", "api_key"},
		{"automation", "key-id", "api_key", "key-id", "api_key"},
		{"unrecognized type", "user-id", "untrusted", "user-id", "api_key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := WithActor(context.Background(), tc.verifiedID, tc.kind)
			if got := ActorType(ctx, tc.writtenID); got != tc.want {
				t.Fatalf("actor type = %q, want %q", got, tc.want)
			}
		})
	}
	if got := ActorType(context.Background(), "key-id"); got != "api_key" {
		t.Fatalf("legacy operation without human context = %q", got)
	}
}
