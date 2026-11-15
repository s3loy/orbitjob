// Package drafts stores opt-in private configuration drafts without credentials.
package drafts

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

const Lifetime = 7 * 24 * time.Hour
const MaxPlaintext = 256 * 1024

type Address struct{ TenantID, OwnerID, Kind, Namespace, Name string }
type Payload struct {
	SpecJSON            string `json:"spec_json"`
	BaseSpecJSON        string `json:"base_spec_json,omitempty"`
	BaseUID             string `json:"base_uid,omitempty"`
	BaseResourceVersion string `json:"base_resource_version,omitempty"`
	OperationRequestID  string `json:"operation_request_id,omitempty"`
}
type Record struct {
	ID, KeyID                       string
	Ciphertext, Nonce               []byte
	Version                         int64
	CreatedAt, UpdatedAt, ExpiresAt time.Time
}
type Draft struct {
	ID        string    `json:"draft_id"`
	Version   int64     `json:"version"`
	Payload   Payload   `json:"payload"`
	UpdatedAt time.Time `json:"updated_at"`
	ExpiresAt time.Time `json:"expires_at"`
}
type Store interface {
	Read(context.Context, Address, time.Time) (Record, error)
	Write(context.Context, Address, Record, int64) (Record, error)
	Delete(context.Context, Address, int64) error
	Recover(context.Context, Address, Address, Record, int64) (Record, error)
}
type Service struct {
	Store     Store
	ActiveKey string
	Keys      map[string][]byte
	Now       func() time.Time
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func (s Service) available() error {
	if s.Store == nil || len(s.Keys[s.ActiveKey]) != 32 {
		return &resource.UnavailableError{Resource: "private drafts", Message: "private draft encryption is not configured; export your configuration instead"}
	}
	return nil
}
func associated(address Address, record Record) []byte {
	data, _ := json.Marshal([]string{address.TenantID, address.OwnerID, address.Kind, address.Namespace, address.Name, record.ID, record.KeyID})
	return data
}
func (s Service) encrypt(address Address, payload Payload, record Record) (Record, error) {
	plaintext, err := json.Marshal(payload)
	if err != nil || len(plaintext) > MaxPlaintext {
		return record, validation.New("payload", "keep the complete private draft under 256 KiB")
	}
	block, err := aes.NewCipher(s.Keys[s.ActiveKey])
	if err != nil {
		return record, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return record, err
	}
	record.KeyID = s.ActiveKey
	record.Nonce = make([]byte, gcm.NonceSize())
	if _, err = rand.Read(record.Nonce); err != nil {
		return record, err
	}
	record.Ciphertext = gcm.Seal(nil, record.Nonce, plaintext, associated(address, record))
	return record, nil
}
func (s Service) decrypt(address Address, record Record) (Payload, error) {
	key := s.Keys[record.KeyID]
	if len(key) != 32 {
		return Payload{}, &resource.UnavailableError{Resource: "private drafts", Message: "the encryption key for this draft is unavailable; restore the original authoring key"}
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return Payload{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return Payload{}, err
	}
	if len(record.Nonce) != gcm.NonceSize() {
		return Payload{}, fmt.Errorf("private draft nonce is invalid")
	}
	plaintext, err := gcm.Open(nil, record.Nonce, record.Ciphertext, associated(address, record))
	if err != nil {
		return Payload{}, fmt.Errorf("private draft identity or integrity verification failed")
	}
	var payload Payload
	if json.Unmarshal(plaintext, &payload) != nil {
		return Payload{}, fmt.Errorf("private draft is corrupt")
	}
	return payload, nil
}
func (s Service) Read(ctx context.Context, address Address) (Draft, error) {
	if err := s.available(); err != nil {
		return Draft{}, err
	}
	record, err := s.Store.Read(ctx, address, s.now())
	if err != nil {
		return Draft{}, err
	}
	if !s.now().Before(record.ExpiresAt) {
		return Draft{}, &resource.NotFoundError{Resource: "expired private draft"}
	}
	payload, err := s.decrypt(address, record)
	if err != nil {
		return Draft{}, err
	}
	return Draft{ID: record.ID, Version: record.Version, Payload: payload, UpdatedAt: record.UpdatedAt, ExpiresAt: record.ExpiresAt}, nil
}
func (s Service) Write(ctx context.Context, address Address, payload Payload, version int64) (Draft, error) {
	if err := s.available(); err != nil {
		return Draft{}, err
	}
	if version < 0 {
		return Draft{}, validation.New("version", "must be nonnegative")
	}
	now := s.now()
	record := Record{ID: uuid.NewString(), CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(Lifetime)}
	var err error
	record, err = s.encrypt(address, payload, record)
	if err != nil {
		return Draft{}, err
	}
	record, err = s.Store.Write(ctx, address, record, version)
	if err != nil {
		return Draft{}, err
	}
	return Draft{ID: record.ID, Version: record.Version, Payload: payload, UpdatedAt: record.UpdatedAt, ExpiresAt: record.ExpiresAt}, nil
}
func (s Service) Delete(ctx context.Context, address Address, version int64) error {
	if err := s.available(); err != nil {
		return err
	}
	return s.Store.Delete(ctx, address, version)
}

// Recover copies into the newly authenticated principal only after the HTTP
// boundary has independently authorized recovery for this exact resource.
func (s Service) Recover(ctx context.Context, from, to Address, version int64) (Draft, error) {
	if err := s.available(); err != nil {
		return Draft{}, err
	}
	if from.TenantID != to.TenantID || from.Kind != to.Kind || from.Namespace != to.Namespace || from.Name != to.Name || from.OwnerID == to.OwnerID {
		return Draft{}, validation.New("owner_id", "recover from another key under the same resource")
	}
	old, err := s.Store.Read(ctx, from, s.now())
	if err != nil {
		return Draft{}, err
	}
	if old.Version != version {
		return Draft{}, &resource.ConflictError{Resource: "private draft", Message: "the original draft changed; read it again before recovery"}
	}
	payload, err := s.decrypt(from, old)
	if err != nil {
		return Draft{}, err
	}
	now := s.now()
	record := Record{ID: uuid.NewString(), CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(Lifetime)}
	record, err = s.encrypt(to, payload, record)
	if err != nil {
		return Draft{}, err
	}
	record, err = s.Store.Recover(ctx, from, to, record, version)
	if err != nil {
		return Draft{}, err
	}
	return Draft{ID: record.ID, Version: record.Version, Payload: payload, UpdatedAt: record.UpdatedAt, ExpiresAt: record.ExpiresAt}, nil
}
