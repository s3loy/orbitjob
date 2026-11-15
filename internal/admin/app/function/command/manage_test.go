package command

import (
	"context"
	"errors"
	"testing"

	"orbitjob/internal/core/domain/function"
)

const managementTenant = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

type fakeRepository struct {
	write   function.ManagedWrite
	created bool
	err     error
	calls   int
}

func (r *fakeRepository) CreateManaged(_ context.Context, in function.ManagedWrite) (function.Definition, bool, error) {
	r.write, r.calls = in, r.calls+1
	return in.Definition, r.created, r.err
}
func (r *fakeRepository) UpdateManaged(_ context.Context, in function.ManagedWrite) (function.Definition, error) {
	r.write, r.calls = in, r.calls+1
	return in.Definition, r.err
}

func managementInput() Input {
	return Input{ActorID: "key-1", IdempotencyKey: "receipt-1", Definition: function.Definition{
		ID: 7, Version: 4, TenantID: managementTenant, Name: " charge ", Image: "app:v1",
		Status: function.StatusActive, TimeoutSeconds: 60,
	}}
}

func TestCreateUsesAuthenticatedGroupAndStableNormalizedHash(t *testing.T) {
	repo := &fakeRepository{created: true}
	in := managementInput()
	in.Definition.ResourceGroupID = "untrusted"
	in.ResourceGroupID = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	manager := NewManager(repo)
	got, err := manager.Create(context.Background(), in)
	if err != nil || !got.Created || repo.write.Definition.ResourceGroupID != in.ResourceGroupID || repo.write.Definition.Version != 1 || repo.write.Definition.ID != 0 {
		t.Fatalf("result = %+v, write = %+v, error = %v", got, repo.write, err)
	}
	hash := repo.write.PayloadHash
	in.Definition.Name = "charge"
	repo.created = false
	got, err = manager.Create(context.Background(), in)
	if err != nil || got.Created || hash != repo.write.PayloadHash || len(hash) != 64 {
		t.Fatalf("normalized replay hash changed: %q vs %q, error = %v", hash, repo.write.PayloadHash, err)
	}
	in.Definition.Image = "app:v2"
	if _, err := manager.Create(context.Background(), in); err != nil || hash == repo.write.PayloadHash {
		t.Fatalf("changed definition must change hash: %v", err)
	}
}

func TestManagementValidationPrecedesWrites(t *testing.T) {
	for _, tc := range []struct {
		name   string
		update bool
		change func(*Input)
	}{
		{"no create identity", false, func(in *Input) { in.IdempotencyKey = " " }},
		{"no actor", false, func(in *Input) { in.ActorID = " " }},
		{"invalid definition", false, func(in *Input) { in.Definition.Image = " " }},
		{"invalid group", true, func(in *Input) { in.ResourceGroupID = "bad" }},
		{"missing expected version", true, func(in *Input) { in.Definition.Version = 0 }},
		{"missing ID", true, func(in *Input) { in.Definition.ID = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepository{}
			in := managementInput()
			tc.change(&in)
			var err error
			if tc.update {
				_, err = NewManager(repo).Update(context.Background(), in)
			} else {
				_, err = NewManager(repo).Create(context.Background(), in)
			}
			if err == nil || repo.calls != 0 {
				t.Fatalf("error = %v, writes = %d", err, repo.calls)
			}
		})
	}
}

func TestUpdatePinsVersionAndCannotMoveGroups(t *testing.T) {
	repo := &fakeRepository{}
	in := managementInput()
	in.Definition.Status = function.StatusPaused
	in.Definition.ResourceGroupID = "untrusted"
	in.ResourceGroupID = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	_, err := NewManager(repo).Update(context.Background(), in)
	if err != nil || repo.write.Definition.ResourceGroupID != "" || repo.write.CallerGroupID != in.ResourceGroupID || repo.write.Definition.Version != 4 || repo.write.Definition.Status != function.StatusPaused {
		t.Fatalf("write = %+v, error = %v", repo.write, err)
	}
}

func TestManagementPropagatesStorageFailures(t *testing.T) {
	failure := errors.New("audit failed")
	manager := NewManager(&fakeRepository{err: failure})
	if _, err := manager.Create(context.Background(), managementInput()); !errors.Is(err, failure) {
		t.Fatalf("create error = %v", err)
	}
	if _, err := manager.Update(context.Background(), managementInput()); !errors.Is(err, failure) {
		t.Fatalf("update error = %v", err)
	}
	if _, err := NewManager(nil).Create(context.Background(), managementInput()); err == nil {
		t.Fatal("expected unavailable repository error")
	}
	if _, err := NewManager(nil).Update(context.Background(), managementInput()); err == nil {
		t.Fatal("expected unavailable repository error")
	}
}
