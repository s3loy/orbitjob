package command

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type stubAPIKeyCreator struct {
	called    bool
	tenantID  string
	id        string
	keyHash   string
	keyPrefix string
	createdAt time.Time
	err       error
}

func (s *stubAPIKeyCreator) Create(ctx context.Context, tenantID, id, keyHash, keyPrefix string, createdAt time.Time) error {
	s.called = true
	s.tenantID = tenantID
	s.id = id
	s.keyHash = keyHash
	s.keyPrefix = keyPrefix
	s.createdAt = createdAt
	return s.err
}

func TestCreator_Create_Success(t *testing.T) {
	repo := &stubAPIKeyCreator{}
	uc := NewCreator(repo)

	out, err := uc.Create(context.Background(), CreateInput{TenantID: "tenant1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !repo.called {
		t.Fatal("expected repo.Create to be called")
	}
	if repo.tenantID != "tenant1" {
		t.Fatalf("expected tenantID=%q, got %q", "tenant1", repo.tenantID)
	}
	if repo.id == "" {
		t.Fatal("expected generated id")
	}
	if out.ID != repo.id {
		t.Fatalf("expected result id=%q, got %q", repo.id, out.ID)
	}
	if !strings.HasPrefix(out.Key, apiKeyPrefix) {
		t.Fatalf("expected key to start with %q, got %q", apiKeyPrefix, out.Key)
	}
	if len(out.Key) != apiKeyRandomLen+len(apiKeyPrefix) {
		t.Fatalf("expected key length=%d, got %d", apiKeyRandomLen+len(apiKeyPrefix), len(out.Key))
	}
	if out.KeyPrefix != out.Key[:apiKeyMinLen] {
		t.Fatalf("expected prefix=%q, got %q", out.Key[:apiKeyMinLen], out.KeyPrefix)
	}
	if repo.keyHash == "" {
		t.Fatal("expected non-empty key hash")
	}
	if !strings.HasPrefix(repo.keyHash, "$2a$") && !strings.HasPrefix(repo.keyHash, "$2b$") && !strings.HasPrefix(repo.keyHash, "$2y$") {
		t.Fatalf("expected bcrypt hash, got %q", repo.keyHash)
	}
	if repo.keyPrefix != out.KeyPrefix {
		t.Fatalf("expected repo prefix=%q, got %q", out.KeyPrefix, repo.keyPrefix)
	}
	if out.CreatedAt == "" {
		t.Fatal("expected created_at")
	}
}

func TestCreator_Create_GenerateError(t *testing.T) {
	repo := &stubAPIKeyCreator{}
	uc := NewCreator(repo)
	uc.generateKey = func() (string, error) { return "", errors.New("rand failed") }

	_, err := uc.Create(context.Background(), CreateInput{TenantID: "tenant1"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if repo.called {
		t.Fatal("expected repo.Create not to be called")
	}
}

func TestCreator_Create_RepoError(t *testing.T) {
	repo := &stubAPIKeyCreator{err: errors.New("db down")}
	uc := NewCreator(repo)

	_, err := uc.Create(context.Background(), CreateInput{TenantID: "tenant1"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCreator_Create_HashError(t *testing.T) {
	repo := &stubAPIKeyCreator{}
	uc := NewCreator(repo)
	uc.hashKey = func(key []byte) ([]byte, error) { return nil, errors.New("hash failed") }

	_, err := uc.Create(context.Background(), CreateInput{TenantID: "tenant1"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if repo.called {
		t.Fatal("expected repo.Create not to be called on hash error")
	}
}

func TestCreator_Create_KeyPrefixShort(t *testing.T) {
	repo := &stubAPIKeyCreator{}
	uc := NewCreator(repo)
	uc.generateKey = func() (string, error) { return "otj_abc", nil }

	out, err := uc.Create(context.Background(), CreateInput{TenantID: "tenant1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.KeyPrefix != "otj_abc" {
		t.Fatalf("expected prefix=%q, got %q", "otj_abc", out.KeyPrefix)
	}
	if repo.keyPrefix != "otj_abc" {
		t.Fatalf("expected repo prefix=%q, got %q", "otj_abc", repo.keyPrefix)
	}
}

func TestGenerateAPIKey_Error(t *testing.T) {
	errReader := &errorReader{}
	_, err := generateAPIKey(errReader)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

type errorReader struct{}

func (e *errorReader) Read(p []byte) (int, error) {
	return 0, errors.New("rand failed")
}
