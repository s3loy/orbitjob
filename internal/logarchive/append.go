package logarchive

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *Store) Append(id Identity, records []Record) error {
	return s.AppendBatch(id, "", records)
}

func (s *Store) AppendBatch(id Identity, batchID string, records []Record) (result error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := id.Key()
	old := s.items[key]
	if old == nil {
		return ErrMissing
	}
	defer func() {
		if errors.Is(result, ErrCapacity) {
			if err := s.markLossLocked(key, "execution log capacity limit rejected output"); err != nil {
				result = fmt.Errorf("persist collection loss marker: %w", err)
			}
		}
	}()
	if batchID != "" {
		for _, v := range old.Batches {
			if v == batchID {
				return nil
			}
		}
	}
	if old.State == "deleted" || old.State == "expired" {
		return ErrSealed
	}
	if old.Identity.RunUID != id.RunUID {
		return fmt.Errorf("archive immutable identity mismatch")
	}
	if len(records) > 1000 {
		return fmt.Errorf("archive batch too large")
	}
	m := clone(old)
	// Preserve every admitted record fingerprint, including evicted output.
	// The bounded manifest is the admission ceiling; forgetting old IDs would
	// make collector restart/repartitioned retries charge the same output twice.
	seen := make(map[string]bool, len(m.Seen))
	for _, v := range m.Seen {
		seen[v] = true
	}
	for _, v := range m.Events {
		seen[v.ID] = true
	}
	now := s.limits.Now().UTC()
	var output []Record
	var raw int64
	for _, r := range records {
		if r.Kind == "event" {
			r.Text = redactEvent(r.Text)
		}
		if r.ID == "" || len(r.ID) > 128 || len(r.Text) > MaxRecordBytes || r.Attempt < 0 {
			return fmt.Errorf("invalid archive record")
		}
		if seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		m.Seen = append(m.Seen, r.ID)
		r.Sequence = m.Next
		m.Next++
		r.ObservedAt = now
		if r.Time.IsZero() {
			r.Time = now
			r.TimeSource = "observed"
		} else {
			r.TimeSource = "source"
		}
		switch r.Kind {
		case "stdout", "stderr":
			raw += int64(len(r.Text))
			output = append(output, r)
		case "event":
			m.Events = append(m.Events, r)
		default:
			return fmt.Errorf("unsupported archive record kind")
		}
	}
	if raw > 256*1024 {
		return fmt.Errorf("archive batch exceeds 256 KiB")
	}
	if m.Next == old.Next {
		return nil
	}
	// Lifecycle metadata has its own budget; retain the beginning and recent facts.
	for len(m.Events) > 1024 || eventSize(m.Events) > 64*1024 {
		if old.SealedAt != nil {
			return ErrCapacity
		}
		if len(m.Events) < 3 {
			break
		}
		index := 1
		if len(m.Events) > 32 {
			index = 16
		}
		r := m.Events[index]
		m.Events = append(m.Events[:index], m.Events[index+1:]...)
		addGap(m, r.Sequence, r.Sequence, "lifecycle metadata limit")
	}
	m.OutputBytes += raw
	m.UpdatedAt = now
	if len(output) == 0 {
		return s.saveLocked(key, m)
	}
	combined := output
	var previous *Segment
	if len(m.Segments) > 0 {
		last := m.Segments[len(m.Segments)-1]
		if last.Bytes+raw <= 256*1024 && last.Records+len(output) <= 1000 {
			prior, e := s.segmentRecords(key, last)
			if e != nil {
				return e
			}
			combined = append(prior, output...)
			previous = &last
		}
	}
	data, err := encodeRecords(combined)
	if err != nil {
		return err
	}
	var tenantBytes, workflowBytes int64
	for _, other := range s.items {
		if other.Identity.TenantID == id.TenantID {
			tenantBytes += other.RetainedBytes
		}
		if id.WorkflowID > 0 && other.Identity.TenantID == id.TenantID && other.Identity.WorkflowID == id.WorkflowID {
			workflowBytes += other.RetainedBytes
		}
	}
	if (old.SealedAt != nil && m.RetainedBytes+raw > s.limits.RunBytes) || tenantBytes+raw > s.limits.TenantBytes || (id.WorkflowID > 0 && workflowBytes+raw > s.limits.WorkflowBytes) || !s.roomLocked(int64(len(data))*2+s.limits.ManifestBytes*2) {
		addGap(m, output[0].Sequence, output[len(output)-1].Sequence, "log storage capacity")
		m.State = "partial"
		m.Reason = "output omitted because the log storage budget is exhausted"
		if e := s.saveLocked(key, m); e != nil {
			return e
		}
		return ErrCapacity
	}
	seg := Segment{Name: fmt.Sprintf("%020d.gz", output[0].Sequence), First: output[0].Sequence, Last: output[len(output)-1].Sequence, Bytes: raw, DiskBytes: int64(len(data)), Records: len(output)}
	if previous != nil {
		seg.Name = previous.Name
		seg.First = previous.First
		seg.Bytes += previous.Bytes
		seg.Records += previous.Records
		m.Segments = m.Segments[:len(m.Segments)-1]
	}
	if err = s.atomicLocked(key+"/"+seg.Name, data); err != nil {
		return err
	}
	m.Segments = append(m.Segments, seg)
	m.RetainedBytes += raw
	if old.SealedAt != nil {
		until := now.Add(Retention)
		if m.ProtectedUntil == nil || m.ProtectedUntil.Before(until) {
			m.ProtectedUntil = &until
		}
	}
	var obsolete []Segment
	// Segments are bounded to 256 KiB. Never evict another run to retain this tail.
	var head int64
	for m.RetainedBytes > s.limits.RunBytes && len(m.Segments) > 1 {
		index := 0
		head = 0
		for index < len(m.Segments)-1 && head+m.Segments[index].Bytes <= s.limits.HeadBytes {
			head += m.Segments[index].Bytes
			index++
		}
		drop := m.Segments[index]
		m.Segments = append(m.Segments[:index], m.Segments[index+1:]...)
		m.RetainedBytes -= drop.Bytes
		obsolete = append(obsolete, drop)
		addGap(m, drop.First, drop.Last, "execution output size limit")
		m.State = "partial"
		m.Reason = "retained beginning and recent output; execution output limit reached"
	}
	if err = s.saveLocked(key, m); err != nil {
		// The previous manifest remains authoritative; the new segment is unacknowledged.
		if previous == nil {
			_ = s.removeLocked(key + "/" + seg.Name)
		}
		return err
	}
	for _, drop := range obsolete {
		if err = s.removeLocked(key + "/" + drop.Name); err != nil {
			return err
		}
	}
	return nil
}

func encodeRecords(records []Record) ([]byte, error) {
	var out bytes.Buffer
	w := gzip.NewWriter(&out)
	enc := json.NewEncoder(w)
	for _, r := range records {
		if err := enc.Encode(r); err != nil {
			_ = w.Close()
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func eventSize(records []Record) int { b, _ := json.Marshal(records); return len(b) }

func addGap(m *Manifest, from, through int64, reason string) {
	if len(m.Gaps) > 0 {
		last := &m.Gaps[len(m.Gaps)-1]
		if last.Reason == reason && from <= last.Through+1 {
			if through > last.Through {
				last.Through = through
			}
			return
		}
	}
	m.Gaps = append(m.Gaps, Gap{From: from, Through: through, Reason: reason})
	if len(m.Gaps) > 32 {
		m.Gaps = []Gap{{From: m.Gaps[0].From, Through: through, Reason: "multiple output gaps; exact subranges unavailable"}}
	}
}

func (s *Store) removeLocked(name string) error {
	info, err := s.root.Stat(name)
	if err != nil {
		return err
	}
	if err = s.root.Remove(name); err != nil {
		return err
	}
	s.used -= allocated(info)
	return nil
}

func (s *Store) Complete(id Identity, outcome string, completed time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := id.Key()
	old := s.items[key]
	if old == nil {
		return ErrMissing
	}
	if old.SealedAt != nil {
		return nil
	}
	if !strings.Contains("|Succeeded|Failed|Canceled|Rejected|", "|"+outcome+"|") {
		return fmt.Errorf("not a terminal execution outcome")
	}
	if old.CompletedAt != nil {
		return nil
	}
	m := clone(old)
	m.Outcome = outcome
	if m.CompletedAt == nil {
		t := completed.UTC()
		m.CompletedAt = &t
	}
	m.UpdatedAt = s.limits.Now().UTC()
	return s.saveLocked(key, m)
}

// Seal freezes the retained window. Coverage stays unverified unless collector
// source watermarks prove EOF; a quiet interval alone is not proof of completeness.
func (s *Store) Seal(id Identity) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := id.Key()
	old := s.items[key]
	if old == nil {
		return ErrMissing
	}
	if old.SealedAt != nil {
		return nil
	}
	if old.CompletedAt == nil {
		return fmt.Errorf("active archive cannot be sealed")
	}
	m := clone(old)
	now := s.limits.Now().UTC()
	m.SealedAt = &now
	end := now
	if m.CompletedAt.After(end) {
		end = *m.CompletedAt
	}
	until := end.Add(Retention)
	m.ProtectedUntil = &until
	if m.State != "partial" {
		m.State = "sealed"
		m.Reason = "retained output sealed; source coverage is not independently certified"
	}
	m.UpdatedAt = now
	return s.saveLocked(key, m)
}
