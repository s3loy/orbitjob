package command

import (
	"context"
	"errors"
	"strings"
	"testing"

	"orbitjob/internal/core/domain/audit"
)

type renameRepo struct {
	called           bool
	tenant, id, name string
	event            audit.Event
	err              error
}

func (r *renameRepo) Rename(_ context.Context, tenant, id, name string, ev audit.Event) error {
	r.called, r.tenant, r.id, r.name, r.event = true, tenant, id, name, ev
	return r.err
}

func TestCreateNameIsMetadataAndAudited(t *testing.T) {
	for _, name := range []string{"", "Daily operator"} {
		r := newFakeKeyRepo()
		c := NewCreator(r)
		c.generateKey = func() (string, error) { return "otj_fixed_credential", nil }
		c.hashKey = func([]byte) ([]byte, error) { return []byte("hash"), nil }
		out, err := c.Create(context.Background(), CreateInput{TenantID: "own", Name: name, Caller: Caller{KeyID: "actor"}})
		if err != nil || out.Name != name || r.created.Name != name || out.Key != "otj_fixed_credential" || out.KeyPrefix != "otj_fixed_cr" {
			t.Fatalf("out=%+v persisted=%+v err=%v", out, r.created, err)
		}
		if r.createEv.Diff["name"] != name || len(r.created.PolicyIDs) != 0 || r.created.BoundaryPolicyID != "" {
			t.Fatalf("name changed grants or missing audit: %+v %+v", r.created, r.createEv)
		}
	}
}

func TestCreateAndRenameRejectNamesBeforePersistence(t *testing.T) {
	for _, name := range []string{" label", "label\n", strings.Repeat("x", 129)} {
		c := &stubAPIKeyCreator{}
		if _, err := NewCreator(c).Create(context.Background(), CreateInput{Name: name}); err == nil || c.called {
			t.Fatal("invalid create reached persistence")
		}
		r := &renameRepo{}
		if _, err := NewRenamer(r).Rename(context.Background(), RenameInput{Name: name}); err == nil || r.called {
			t.Fatal("invalid rename reached persistence")
		}
	}
}

func TestRenameRecordsOnlyDisplayMetadata(t *testing.T) {
	for _, name := range []string{"New name", ""} {
		r := &renameRepo{}
		out, err := NewRenamer(r).Rename(context.Background(), RenameInput{TenantID: "own", ID: "key", Name: name, ActorID: "actor"})
		if err != nil || out.Name != name || out.ID != "key" || r.tenant != "own" || r.id != "key" || r.name != name {
			t.Fatalf("out=%+v repo=%+v err=%v", out, r, err)
		}
		if r.event.ActorID != "actor" || r.event.EventType != audit.EventUpdate || r.event.ResourceType != audit.ResourceAPIKey || len(r.event.Diff) != 1 || r.event.Diff["name"] != name {
			t.Fatalf("incorrect audit %+v", r.event)
		}
	}
	r := &renameRepo{err: errors.New("audit unavailable")}
	if _, err := NewRenamer(r).Rename(context.Background(), RenameInput{Name: "Label"}); err == nil {
		t.Fatal("persistence/audit error lost")
	}
}
