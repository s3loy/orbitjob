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

func TestActorRequiresExplicitServerIdentity(t *testing.T) {
	for _, kind := range []string{"user", "api_key", "system"} {
		t.Run(kind, func(t *testing.T) {
			id, gotKind, err := Actor(WithActor(context.Background(), "actor-id", kind))
			if err != nil || id != "actor-id" || gotKind != kind {
				t.Fatalf("Actor() = (%q, %q, %v)", id, gotKind, err)
			}
		})
	}
	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{
		{"missing", context.Background()},
		{"empty ID", WithActor(context.Background(), "", "user")},
		{"blank ID", WithActor(context.Background(), " \t", "api_key")},
		{"unknown kind", WithActor(context.Background(), "actor-id", "untrusted")},
		{"empty kind", WithActor(context.Background(), "actor-id", "")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, kind, err := Actor(tc.ctx)
			if err != ErrActorRequired || id != "" || kind != "" {
				t.Fatalf("Actor() = (%q, %q, %v), want no identity", id, kind, err)
			}
		})
	}
}
