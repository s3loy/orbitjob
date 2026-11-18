package logarchive

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const (
	controlBytes     = 4096
	controlLossBytes = 2048
	terminalSlotSize = 1024
)

// Every registered run reserves one allocated block for loss and terminal
// metadata when capacity prevents replacing the manifest.
func (s *Store) initControlLocked(key string) error {
	if _, err := s.root.Stat(key + "/control"); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	data := bytes.Repeat([]byte{' '}, controlBytes)
	copy(data, "OK\n")
	return s.atomicLocked(key+"/control", data)
}

func (s *Store) markLossLocked(key, reason string) error {
	m := s.items[key]
	if m == nil {
		return ErrMissing
	}
	if len("LOSS: ")+len(reason)+1 > controlLossBytes {
		return fmt.Errorf("collection loss reason exceeds the control record")
	}
	data := bytes.Repeat([]byte{' '}, controlLossBytes)
	copy(data, "LOSS: "+reason+"\n")
	if err := s.writeControlLocked(key, data, 0); err != nil {
		return err
	}
	m.State = "partial"
	m.Reason = reason
	return nil
}

func (s *Store) writeControlLocked(key string, data []byte, offset int64) error {
	f, err := s.root.OpenFile(key+"/control", os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() != controlBytes || !info.Mode().IsRegular() {
		return fmt.Errorf("invalid archive control block")
	}
	if _, err = f.WriteAt(data, offset); err != nil {
		return err
	}
	return f.Sync()
}

func (s *Store) readControl(key string) ([]byte, error) {
	f, err := s.root.Open(key + "/control")
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, controlBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) != controlBytes {
		return nil, fmt.Errorf("invalid archive control block size")
	}
	return data, nil
}

func (s *Store) applyControlLocked(key string, m *Manifest) {
	if m.State == "deleted" || m.State == "expired" {
		return
	}
	data, e := s.readControl(key)
	if e != nil || !bytes.HasPrefix(data, []byte("OK\n")) {
		reason := "collection control record unavailable"
		if e == nil && bytes.HasPrefix(data, []byte("LOSS: ")) {
			line, _, _ := bytes.Cut(data[:controlLossBytes], []byte{'\n'})
			reason = strings.TrimSpace(string(line[6:]))
		}
		m.State = "partial"
		m.Reason = reason
		m.Gaps = append(m.Gaps, Gap{Reason: fmt.Sprintf("%s; exact missing source range is unavailable", reason)})
	}
}

type terminalControl struct {
	Outcome     string     `json:"outcome"`
	CompletedAt time.Time  `json:"completed_at"`
	SealedAt    *time.Time `json:"sealed_at,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func terminalOutcome(outcome string) bool {
	switch outcome {
	case "Succeeded", "Failed", "Canceled", "Rejected":
		return true
	}
	return false
}

// Capacity can prevent a manifest replacement even when its existing evidence
// is sound. Complete and Seal each have a separate slot in the allocated block.
// A failed seal write leaves the completion slot and the manifest intact.
func (s *Store) saveTerminalLocked(key string, m *Manifest) error {
	if err := s.saveLocked(key, m); !errors.Is(err, ErrCapacity) {
		return err
	}
	payload, err := json.Marshal(terminalControl{
		Outcome: m.Outcome, CompletedAt: *m.CompletedAt, SealedAt: m.SealedAt, UpdatedAt: m.UpdatedAt,
	})
	if err != nil {
		return err
	}
	digest := sha256.Sum256(append([]byte(key), payload...))
	if hex.EncodedLen(len(digest))+1+len(payload) > terminalSlotSize {
		return fmt.Errorf("terminal archive metadata exceeds the control record")
	}
	data := bytes.Repeat([]byte{' '}, terminalSlotSize)
	n := hex.Encode(data, digest[:])
	data[n] = '\n'
	copy(data[n+1:], payload)
	offset := int64(controlLossBytes)
	if m.SealedAt != nil {
		offset += terminalSlotSize
	}
	if err = s.writeControlLocked(key, data, offset); err != nil {
		return fmt.Errorf("persist terminal archive metadata: %w", err)
	}
	s.items[key] = m
	return nil
}

func decodeTerminalSlot(key string, data []byte, sealed bool) (terminalControl, error) {
	var record terminalControl
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return record, nil
	}
	checksum, payload, _ := bytes.Cut(data, []byte{'\n'})
	digest := sha256.Sum256(append([]byte(key), payload...))
	if string(checksum) != hex.EncodeToString(digest[:]) || json.Unmarshal(payload, &record) != nil || !terminalOutcome(record.Outcome) || (record.SealedAt != nil) != sealed {
		return terminalControl{}, fmt.Errorf("invalid terminal archive control record")
	}
	return record, nil
}

func (s *Store) restoreTerminal(key string, m *Manifest) error {
	if m.SealedAt != nil || m.State == "deleted" || m.State == "expired" {
		return nil
	}
	data, err := s.readControl(key)
	if err != nil {
		return err
	}
	completion, completeErr := decodeTerminalSlot(key, data[controlLossBytes:controlLossBytes+terminalSlotSize], false)
	seal, sealErr := decodeTerminalSlot(key, data[controlLossBytes+terminalSlotSize:], true)
	if seal.Outcome != "" {
		completion = seal
	}
	if m.CompletedAt == nil && completion.Outcome != "" {
		m.Outcome = completion.Outcome
		m.CompletedAt = &completion.CompletedAt
	}
	updatedAt := m.UpdatedAt
	if completion.SealedAt != nil && m.Outcome == completion.Outcome && m.CompletedAt.Equal(completion.CompletedAt) {
		sealManifest(m, *completion.SealedAt)
	}
	if completion.UpdatedAt.After(updatedAt) {
		updatedAt = completion.UpdatedAt
	}
	if updatedAt.After(m.UpdatedAt) {
		m.UpdatedAt = updatedAt
	}
	if completeErr != nil || sealErr != nil || (seal.SealedAt != nil && m.SealedAt == nil) {
		return fmt.Errorf("terminal archive control record is corrupt or inconsistent with the manifest")
	}
	return nil
}

func (s *Store) NoteLoss(id Identity, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.markLossLocked(id.Key(), reason)
}
