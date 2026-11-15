package logarchive

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func (s *Store) segmentRecords(ctx context.Context, key string, seg Segment) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if info, err := s.root.Lstat(key + "/" + seg.Name); err != nil {
		return nil, err
	} else if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("archive segment is not a regular file")
	}
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
	scan := bufio.NewScanner(io.LimitReader(contextReader{ctx, gz}, 4*MiB+1))
	scan.Buffer(make([]byte, 4096), MaxRecordBytes*8)
	var records []Record
	var decodedBytes, outputBytes int64
	for scan.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		decodedBytes += int64(len(scan.Bytes()) + 1)
		if decodedBytes > 4*MiB {
			return nil, fmt.Errorf("oversized archive segment")
		}
		var r Record
		if err = json.Unmarshal(scan.Bytes(), &r); err != nil {
			return nil, err
		}
		if r.ID == "" || len(r.ID) > 128 || len(r.Text) > MaxRecordBytes || (r.Kind != "stdout" && r.Kind != "stderr") || r.Sequence < 1 {
			return nil, fmt.Errorf("invalid record in archive segment")
		}
		if r.Sequence >= seg.First && r.Sequence <= seg.Last {
			if len(records) > 0 && r.Sequence <= records[len(records)-1].Sequence {
				return nil, fmt.Errorf("archive segment order is invalid")
			}
			records = append(records, r)
			outputBytes += int64(len(r.Text))
		}
		if len(records) > 16000 {
			return nil, fmt.Errorf("oversized archive segment")
		}
	}
	if err := scan.Err(); err != nil {
		return nil, err
	}
	if len(records) != seg.Records || len(records) == 0 || records[0].Sequence != seg.First || records[len(records)-1].Sequence != seg.Last || outputBytes != seg.Bytes {
		return nil, fmt.Errorf("archive segment contents do not match its manifest")
	}
	return records, nil
}

func (s *Store) Read(id Identity, q Query) (Page, error) {
	return s.ReadContext(context.Background(), id, q)
}

// ReadContext pins this archive's segment names, not the installation metadata
// lock. Other archives can append, expire and bind while decoding this snapshot.
func (s *Store) ReadContext(ctx context.Context, id Identity, q Query) (Page, error) {
	if err := id.Validate(); err != nil {
		return Page{}, err
	}
	if q.After < 0 || q.Limit < 0 || q.Limit > 1000 || len(q.Search) > 256 || q.Attempt < 0 {
		return Page{}, fmt.Errorf("invalid archive query")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	archiveRelease, err := s.coordinate(ctx, "archive:"+id.Key(), 1)
	if err != nil {
		return Page{}, err
	}
	defer archiveRelease()
	tenantRelease, err := s.coordinate(ctx, "tenant-read:"+id.TenantID, 2)
	if err != nil {
		return Page{}, err
	}
	defer tenantRelease()
	readRelease, err := s.coordinate(ctx, "installation-read", 4)
	if err != nil {
		return Page{}, err
	}
	defer readRelease()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Page{}, fmt.Errorf("archive store is closed")
	}
	m := s.items[id.Key()]
	if m == nil {
		reason := s.quarantined[id.Key()]
		s.mu.Unlock()
		if reason != "" {
			return Page{State: "unavailable", Reason: reason + "; quarantined evidence is preserved", Records: []Record{}, Gaps: []Gap{{Reason: "archive integrity failure; missing range is unknown"}}}, nil
		}
		return Page{State: "unavailable", Reason: "no durable collection was registered", Records: []Record{}, Gaps: []Gap{}}, nil
	}
	if m.Identity.RunUID != id.RunUID || m.Identity.Namespace != id.Namespace || m.Identity.RevisionID != id.RevisionID {
		s.mu.Unlock()
		return Page{}, fmt.Errorf("archive immutable identity mismatch")
	}
	m = clone(m)
	s.applyControlLocked(id.Key(), m)
	s.reads.Add(1)
	s.mu.Unlock()
	defer s.reads.Done()
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
		records, err := s.segmentRecords(ctx, id.Key(), seg)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return Page{}, err
			}
			archiveCorruption.WithLabelValues("segment").Inc()
			s.mu.Lock()
			if markErr := s.markLossLocked(id.Key(), "retained archive segment is missing or corrupt"); markErr != nil {
				archiveCorruption.WithLabelValues("control").Inc()
			}
			s.mu.Unlock()
			p.State = "partial"
			p.Reason = "one or more retained segments are unreadable; remaining verified evidence is returned"
			p.Gaps = append(p.Gaps, Gap{From: seg.First, Through: seg.Last, Reason: "retained segment is missing or corrupt"})
			continue
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
	release, err := s.coordinate(context.Background(), "archive:"+id.Key(), 1)
	if err != nil {
		return err
	}
	defer release()
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
	var keys []string
	for key, m := range s.items {
		if m.SealedAt != nil && m.ProtectedUntil != nil && !s.limits.Now().Before(*m.ProtectedUntil) && m.State != "expired" && m.State != "deleted" {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return s.items[keys[i]].ProtectedUntil.Before(*s.items[keys[j]].ProtectedUntil) })
	s.mu.Unlock()
	n := 0
	for _, key := range keys {
		if n >= 128 {
			break
		}
		release, err := s.coordinate(context.Background(), "archive:"+key, 1)
		if err != nil {
			return n, err
		}
		s.mu.Lock()
		m := s.items[key]
		if m == nil || m.State == "expired" || m.State == "deleted" || m.ProtectedUntil == nil || s.limits.Now().Before(*m.ProtectedUntil) {
			s.mu.Unlock()
			release()
			continue
		}
		err = s.deleteLocked(key, false)
		s.mu.Unlock()
		release()
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
