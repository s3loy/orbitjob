package logarchive

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

func (s *Store) segmentRecords(key string, seg Segment) ([]Record, error) {
	f, err := s.root.Open(key + "/" + seg.Name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer func() { _ = gz.Close() }()
	scan := bufio.NewScanner(io.LimitReader(gz, 4*MiB))
	scan.Buffer(make([]byte, 4096), MaxRecordBytes*8)
	var records []Record
	for scan.Scan() {
		var r Record
		if err = json.Unmarshal(scan.Bytes(), &r); err != nil {
			return nil, err
		}
		if r.Sequence >= seg.First && r.Sequence <= seg.Last {
			records = append(records, r)
		}
		if len(records) > 16000 {
			return nil, fmt.Errorf("oversized archive segment")
		}
	}
	return records, scan.Err()
}

func (s *Store) Read(id Identity, q Query) (Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.items[id.Key()]
	if m == nil {
		return Page{State: "unavailable", Reason: "no durable collection was registered", Records: []Record{}, Gaps: []Gap{}}, nil
	}
	m = clone(m)
	s.applyControlLocked(id.Key(), m)
	if q.After < 0 || q.Limit < 0 || q.Limit > 1000 || len(q.Search) > 256 || q.Attempt < 0 {
		return Page{}, fmt.Errorf("invalid archive query")
	}
	if q.Limit == 0 {
		q.Limit = 500
	}
	p := Page{State: m.State, Reason: m.Reason, Outcome: m.Outcome, Records: []Record{}, Gaps: append([]Gap{}, m.Gaps...), Next: q.After, OutputBytes: m.OutputBytes, RetainedBytes: m.RetainedBytes, LimitBytes: s.limits.RunBytes, ProtectedUntil: m.ProtectedUntil}
	if m.State == "deleted" || m.State == "expired" {
		return p, nil
	}
	// Merge bounded lifecycle events with each ordered output segment.
	var candidates []Record
	upper := m.Next - 1
	for _, r := range m.Events {
		if r.Sequence > q.After && matches(r, q) {
			candidates = append(candidates, r)
		}
	}
	for _, seg := range m.Segments {
		if seg.Last <= q.After {
			continue
		}
		records, err := s.segmentRecords(id.Key(), seg)
		if err != nil {
			return Page{}, fmt.Errorf("archive segment unavailable: %w", err)
		}
		for _, r := range records {
			if r.Sequence > q.After && matches(r, q) {
				candidates = append(candidates, r)
			}
		}
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].Sequence < candidates[j].Sequence })
		matching := 0
		for _, r := range candidates {
			if r.Sequence <= seg.Last {
				matching++
			}
		}
		if matching > q.Limit {
			upper = seg.Last
			break
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Sequence < candidates[j].Sequence })
	var bytes int
	for _, r := range candidates {
		if r.Sequence > upper {
			break
		}
		if matches(r, q) {
			if len(p.Records) >= q.Limit || bytes+len(r.Text) > 256*1024 {
				p.HasMore = true
				break
			}
			p.Records = append(p.Records, r)
			bytes += len(r.Text)
		}
		p.Next = r.Sequence
	}
	if !p.HasMore {
		p.Next = upper
		p.HasMore = upper < m.Next-1
	}
	// Empty/evicted ranges still advance the cursor; otherwise a follow loop spins.
	if len(candidates) == 0 {
		p.Next = m.Next - 1
		p.HasMore = false
	}
	return p, nil
}

func matches(r Record, q Query) bool {
	return (q.Attempt == 0 || r.Attempt == q.Attempt) && (q.Search == "" || strings.Contains(strings.ToLower(r.Text), strings.ToLower(q.Search)))
}

func (s *Store) Delete(id Identity, manual bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deleteLocked(id.Key(), manual)
}

func (s *Store) deleteLocked(key string, manual bool) error {
	old := s.items[key]
	if old == nil {
		return ErrMissing
	}
	if old.SealedAt == nil {
		return fmt.Errorf("cannot delete active execution logs")
	}
	if !manual && (old.ProtectedUntil == nil || s.limits.Now().Before(*old.ProtectedUntil)) {
		return fmt.Errorf("archive retention protection is active")
	}
	m := clone(old)
	obsolete := m.Segments
	m.Segments = nil
	m.Events = nil
	m.Seen = nil
	m.Batches = nil
	m.Sources = nil
	m.RetainedBytes = 0
	m.State = "expired"
	m.Reason = "logs removed after their retention protection interval"
	if manual {
		m.State = "deleted"
		m.Reason = "logs explicitly deleted by an authorized administrator"
	}
	m.UpdatedAt = s.limits.Now().UTC()
	if err := s.saveLocked(key, m); err != nil {
		return err
	}
	for _, seg := range obsolete {
		if err := s.removeLocked(key + "/" + seg.Name); err != nil {
			return err
		}
	}
	return nil
}

// Sweep reclaims eligible archives only; protected runs never compete with spam.
func (s *Store) Sweep() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var keys []string
	for key, m := range s.items {
		if m.SealedAt != nil && m.ProtectedUntil != nil && !s.limits.Now().Before(*m.ProtectedUntil) && m.State != "expired" && m.State != "deleted" {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return s.items[keys[i]].ProtectedUntil.Before(*s.items[keys[j]].ProtectedUntil) })
	n := 0
	for _, key := range keys {
		if n >= 128 {
			break
		}
		if err := s.deleteLocked(key, false); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
