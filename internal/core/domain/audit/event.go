// Package audit models the authorization-change record.
//
// Granting a permission is itself a privileged act, so the record of who
// granted what has to be as durable as the grant. Kubernetes guidance calls
// this out for binding creation specifically: being able to create bindings is
// an escalation vector, and answering "who gave this key that" after the fact
// requires the grant to have been written down when it happened.
package audit

import "context"

// Event is one authorization change to record.
type Event struct {
	TenantID string
	// ActorID is the stable authenticated user or API key that performed the change.
	ActorID string
	// EventType is the verb: create, update, revoke, delete.
	EventType string
	// ResourceType is the kind of object changed: policy, apikey, resource_group.
	ResourceType string
	ResourceID   string
	// Diff carries the granted policy ids or boundary, so the record shows the
	// change itself rather than only that something happened.
	Diff map[string]any
}

// Event types. Named constants rather than literals so a typo cannot make a
// category of grant invisible to a query that looks for it.
//
// There is no separate "bind" type. A policy is bound only when a key is
// created carrying it, so the key-creation event is the binding event, and its
// diff holds the granted policy ids and boundary. Recording a second row for
// the same act would make one grant look like two.
const (
	EventCreate = "create"
	EventUpdate = "update"
	EventDelete = "delete"
	EventRevoke = "revoke"
)

// Resource types.
const (
	ResourcePolicy = "policy"
	ResourceAPIKey = "apikey"
	ResourceGroup  = "resource_group"
)

type actorContextKey struct{}
type actorIdentity struct{ ID, Type string }

// WithActor preserves authenticated actor metadata across application boundaries.
func WithActor(ctx context.Context, id, kind string) context.Context {
	return context.WithValue(ctx, actorContextKey{}, actorIdentity{id, kind})
}

// ActorType reports the verified actor type only when the supplied ID matches.
func ActorType(ctx context.Context, id string) string {
	actor, _ := ctx.Value(actorContextKey{}).(actorIdentity)
	if actor.ID == id && actor.Type == "user" {
		return "user"
	}
	return "api_key"
}
