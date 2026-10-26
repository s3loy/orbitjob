package logarchive

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testLimits(now *time.Time) Limits {
	l := DefaultLimits()
	l.TotalBytes = 16 * MiB
	l.RunBytes = 4 * MiB
	l.HeadBytes = MiB
	l.TenantBytes = 12 * MiB
	l.WorkflowBytes = 8 * MiB
	l.MetadataBytes = 4 * MiB
	l.DeclaredBytes = 32 * MiB
	l.ReserveBytes = 64 * 1024
	l.Now = func() time.Time { return *now }
	return l
}
func identity(n int) Identity {
	return Identity{TenantID: "tenant", SourceUID: "source", OccurrenceKey: fmt.Sprint(n), RunName: fmt.Sprint("run-", n), RunUID: fmt.Sprint("uid-", n), Namespace: "own", RevisionID: 1, RunID: int64(n)}
}
func openTest(t *testing.T, now *time.Time) *Store {
	t.Helper()
	s, e := Open(t.TempDir(), testLimits(now))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestArchiveNoisyNeighborAndProtectedRetention(t *testing.T) {
	now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	s := openTest(t, &now)
	quiet, loud := identity(1), identity(2)
	for _, id := range []Identity{quiet, loud} {
		if e := s.Register(id); e != nil {
			t.Fatal(e)
		}
	}
	if e := s.Append(quiet, []Record{{ID: "quiet", Kind: "stdout", Text: "keep this evidence", Attempt: 1}}); e != nil {
		t.Fatal(e)
	}
	if e := s.Complete(quiet, "Succeeded", now); e != nil {
		t.Fatal(e)
	}
	if e := s.Seal(quiet); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 96; i++ {
		if e := s.Append(loud, []Record{{ID: fmt.Sprint(i), Kind: "stdout", Attempt: 1, Text: strings.Repeat(fmt.Sprintf("%04d", i), MaxRecordBytes/4)}}); e != nil {
			t.Fatal(e)
		}
	}
	p, e := s.Read(loud, Query{})
	if e != nil {
		t.Fatal(e)
	}
	if p.RetainedBytes > s.limits.RunBytes || len(p.Gaps) == 0 || p.State != "partial" {
		t.Fatalf("unbounded/no gap: %+v", p)
	}
	q, e := s.Read(quiet, Query{})
	if e != nil || len(q.Records) != 1 || q.Records[0].Text != "keep this evidence" {
		t.Fatalf("noisy neighbor erased quiet archive: %+v %v", q, e)
	}
	now = now.Add(Retention - time.Second)
	if n, e := s.Sweep(); e != nil || n != 0 {
		t.Fatalf("protected archive pruned: %d %v", n, e)
	}
	now = now.Add(2 * time.Second)
	if n, e := s.Sweep(); e != nil || n != 1 {
		t.Fatalf("eligible archive not pruned: %d %v", n, e)
	}
	q, _ = s.Read(quiet, Query{})
	if q.State != "expired" || len(q.Records) != 0 {
		t.Fatalf("expired archive: %+v", q)
	}
}

func TestArchiveRestartAndReplay(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	l := testLimits(&now)
	s, e := Open(dir, l)
	if e != nil {
		t.Fatal(e)
	}
	id := identity(1)
	if e = s.Register(id); e != nil {
		t.Fatal(e)
	}
	records := []Record{{ID: "persisted-uuid-a", Kind: "stdout", Text: "same file offset, generation one", Attempt: 1}, {ID: "persisted-uuid-b", Kind: "stderr", Text: "same file offset, generation two", Attempt: 1}}
	if e = s.Append(id, records); e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(dir, l)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = s.Close() }()
	if e = s.Append(id, records); e != nil {
		t.Fatal(e)
	}
	p, e := s.Read(id, Query{})
	if e != nil || len(p.Records) != 2 || p.OutputBytes != int64(len(records[0].Text)+len(records[1].Text)) {
		t.Fatalf("restart/replay: %+v %v", p, e)
	}
	foreign := id
	foreign.TenantID = "other"
	p, e = s.Read(foreign, Query{})
	if e != nil || len(p.Records) != 0 {
		t.Fatalf("cross-tenant archive: %+v %v", p, e)
	}
}

func TestArchiveCursorSearchAndDelayedSeal(t *testing.T) {
	now := time.Now()
	s := openTest(t, &now)
	id := identity(1)
	if e := s.Register(id); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 30; i++ {
		kind := "stdout"
		if i%3 == 0 {
			kind = "event"
		}
		if e := s.Append(id, []Record{{ID: fmt.Sprint(i), Kind: kind, Text: fmt.Sprint("line-", i), Attempt: 1}}); e != nil {
			t.Fatal(e)
		}
	}
	var after int64
	seen := map[int64]bool{}
	for i := 0; i < 20; i++ {
		p, e := s.Read(id, Query{After: after, Limit: 4})
		if e != nil {
			t.Fatal(e)
		}
		for _, r := range p.Records {
			if seen[r.Sequence] {
				t.Fatal("cursor duplicated record")
			}
			seen[r.Sequence] = true
		}
		after = p.Next
		if !p.HasMore {
			break
		}
	}
	if len(seen) != 30 {
		t.Fatalf("cursor lost records: %d", len(seen))
	}
	p, e := s.Read(id, Query{Search: "nothing"})
	if e != nil || p.Next != 30 || p.HasMore {
		t.Fatalf("search stalled: %+v %v", p, e)
	}
	completed := now
	if e = s.Complete(id, "Succeeded", completed); e != nil {
		t.Fatal(e)
	}
	now = now.Add(time.Hour)
	if e = s.Seal(id); e != nil {
		t.Fatal(e)
	}
	m, _ := s.Snapshot(id)
	if m.ProtectedUntil == nil || !m.ProtectedUntil.Equal(now.Add(Retention)) {
		t.Fatal("delayed seal shortened retention")
	}
	if e = s.Append(id, []Record{{ID: "late", Kind: "stdout", Text: "late"}}); e != nil {
		t.Fatalf("bounded late output was lost: %v", e)
	}
	p, e = s.Read(id, Query{Search: "late"})
	if e != nil || len(p.Records) != 1 {
		t.Fatalf("late output missing: %+v %v", p, e)
	}
}

func TestArchiveWriterLockAndPaths(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	s, e := Open(dir, testLimits(&now))
	if e != nil {
		t.Fatal(e)
	}
	if other, e := Open(dir, testLimits(&now)); e == nil {
		_ = other.Close()
		t.Fatal("overlapping archive writers")
	}
	_ = s.Close()
	outside := t.TempDir()
	if e = os.Symlink(outside, filepath.Join(dir, "escape")); e != nil {
		t.Fatal(e)
	}
	if other, e := Open(dir, testLimits(&now)); e == nil {
		_ = other.Close()
		t.Fatal("archive accepted symlink")
	}
}
