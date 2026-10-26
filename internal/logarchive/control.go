package logarchive

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

// Every registered run reserves one allocated control block. Capacity notices
// overwrite that block, so a full payload/manifest cannot hide a known loss.
func (s *Store) initControlLocked(key string) error {
	if _, e := s.root.Stat(key + "/control"); e == nil {
		return nil
	}
	data := bytes.Repeat([]byte{' '}, 4096)
	copy(data, "OK\n")
	return s.atomicLocked(key+"/control", data)
}

func (s *Store) markLossLocked(key, reason string) error {
	m := s.items[key]
	if m == nil {
		return ErrMissing
	}
	m.State = "partial"
	m.Reason = reason
	data := bytes.Repeat([]byte{' '}, 4096)
	copy(data, "LOSS: "+reason+"\n")
	f, e := s.root.OpenFile(key+"/control", os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer func() { _ = f.Close() }()
	if _, e = f.WriteAt(data, 0); e != nil {
		return e
	}
	return f.Sync()
}

func (s *Store) applyControlLocked(key string, m *Manifest) {
	if m.State == "deleted" || m.State == "expired" {
		return
	}
	data, e := s.root.ReadFile(key + "/control")
	if e != nil || !strings.HasPrefix(string(data), "OK\n") {
		reason := "collection control record unavailable"
		if e == nil && bytes.HasPrefix(data, []byte("LOSS: ")) {
			reason = strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0][6:])
		}
		m.State = "partial"
		m.Reason = reason
		m.Gaps = append(m.Gaps, Gap{Reason: fmt.Sprintf("%s; exact missing source range is unavailable", reason)})
	}
}

func (s *Store) NoteLoss(id Identity, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.markLossLocked(id.Key(), reason)
}
