package command

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"time"

	"golang.org/x/crypto/bcrypt"

	"orbitjob/internal/platform/scan"
)

const (
	apiKeyPrefix = "otj_"
	apiKeyMinLen = 12
)

// CreateInput is the control-plane command model for creating an API key.
type CreateInput struct {
	TenantID string
}

// APIKeyCreateResult is the control-plane response model for a created API key.
// The full key is returned only once.
type APIKeyCreateResult struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	KeyPrefix string `json:"key_prefix"`
	CreatedAt string `json:"created_at"`
}

type apiKeyCreator interface {
	Create(ctx context.Context, tenantID, id, keyHash, keyPrefix string) error
}

// Creator creates API keys.
type Creator struct {
	repo        apiKeyCreator
	generateKey func() (string, error)
	hashKey     func(key []byte) ([]byte, error)
}

// NewCreator builds a Creator backed by repo.
func NewCreator(repo apiKeyCreator) *Creator {
	return &Creator{
		repo:        repo,
		generateKey: func() (string, error) { return generateAPIKey(rand.Reader) },
		hashKey:     func(key []byte) ([]byte, error) { return bcrypt.GenerateFromPassword(key, bcrypt.DefaultCost) },
	}
}

// Create generates a new API key, hashes it, and persists it.
func (c *Creator) Create(ctx context.Context, in CreateInput) (APIKeyCreateResult, error) {
	key, err := c.generateKey()
	if err != nil {
		return APIKeyCreateResult{}, fmt.Errorf("generate api key: %w", err)
	}

	hash, err := c.hashKey([]byte(key))
	if err != nil {
		return APIKeyCreateResult{}, fmt.Errorf("hash api key: %w", err)
	}

	id := scan.GenerateID()
	prefix := keyPrefix(key)
	createdAt := time.Now().UTC()

	if err := c.repo.Create(ctx, in.TenantID, id, string(hash), prefix); err != nil {
		return APIKeyCreateResult{}, fmt.Errorf("create api key: %w", err)
	}

	return APIKeyCreateResult{
		ID:        id,
		Key:       key,
		KeyPrefix: prefix,
		CreatedAt: createdAt.Format(time.RFC3339),
	}, nil
}

func generateAPIKey(r io.Reader) (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return apiKeyPrefix + base64.URLEncoding.EncodeToString(b), nil
}

func keyPrefix(key string) string {
	if len(key) < apiKeyMinLen {
		return key
	}
	return key[:apiKeyMinLen]
}
