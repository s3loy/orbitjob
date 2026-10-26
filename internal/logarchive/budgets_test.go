package logarchive

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func TestProductionOutputCapAndFifteenDayProtection(t *testing.T) {
	now := time.Date(2026, 10, 26, 1, 0, 0, 0, time.UTC)
	limits := DefaultLimits()
	limits.Now = func() time.Time { return now }
	s, e := Open(t.TempDir(), limits)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = s.Close() }()
	id := identity(1)
	if e = s.Register(id); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 288; i++ {
		marker := "ordinary output "
		if i == 0 {
			marker = "FIRST OUTPUT SENTINEL "
		}
		if i == 287 {
			marker = "LAST OUTPUT SENTINEL "
		}
		text := marker + strings.Repeat("x", MaxRecordBytes-len(marker))
		if e = s.AppendBatch(id, uuid.NewString(), []Record{{ID: uuid.NewString(), Kind: "stdout", Attempt: 1, Text: text}}); e != nil {
			t.Fatal(e)
		}
	}
	m, e := s.Snapshot(id)
	if e != nil {
		t.Fatal(e)
	}
	if m.OutputBytes != 36*MiB || m.RetainedBytes != 32*MiB || len(m.Gaps) == 0 {
		t.Fatalf("32-MiB cap did not retain a marked bounded window: total=%d retained=%d gaps=%d", m.OutputBytes, m.RetainedBytes, len(m.Gaps))
	}
	for _, needle := range []string{"FIRST OUTPUT SENTINEL", "LAST OUTPUT SENTINEL"} {
		p, e := s.Read(id, Query{Search: needle})
		if e != nil || len(p.Records) != 1 {
			t.Fatalf("lost %q: %d records %v", needle, len(p.Records), e)
		}
	}
	if e = s.Complete(id, "Succeeded", now); e != nil {
		t.Fatal(e)
	}
	now = now.Add(time.Hour)
	if e = s.Seal(id); e != nil {
		t.Fatal(e)
	}
	sealedAt := now
	now = sealedAt.Add(Retention - time.Second)
	if n, e := s.Sweep(); e != nil || n != 0 {
		t.Fatalf("deleted inside 15-day protection: %d %v", n, e)
	}
	p, e := s.Read(id, Query{Search: "LAST OUTPUT SENTINEL"})
	if e != nil || len(p.Records) != 1 {
		t.Fatal("protected historical output unreadable")
	}
	now = sealedAt.Add(Retention + time.Second)
	if n, e := s.Sweep(); e != nil || n != 1 {
		t.Fatalf("did not reclaim after 15 days: %d %v", n, e)
	}
	p, e = s.Read(id, Query{})
	if e != nil || p.State != "expired" || len(p.Records) != 0 {
		t.Fatalf("expiry state did not match reclaimed output: %+v %v", p, e)
	}
	now = now.Add(Retention + time.Second)
	if e = s.Reap(); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Snapshot(id); !errors.Is(e, ErrMissing) {
		t.Fatalf("expired metadata was never reclaimed: %v", e)
	}
}

func TestGlobalBudgetPreservesOtherProtectedExecutions(t *testing.T) {
	now := time.Now()
	limits := testLimits(&now)
	limits.TotalBytes = 4 * MiB
	limits.RunBytes = MiB
	limits.HeadBytes = 256 * 1024
	limits.TenantBytes = 4 * MiB
	s, e := Open(t.TempDir(), limits)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = s.Close() }()
	quiet := identity(1)
	if e = s.Register(quiet); e != nil {
		t.Fatal(e)
	}
	if e = s.Append(quiet, []Record{{ID: uuid.NewString(), Kind: "stdout", Text: "protected evidence"}}); e != nil {
		t.Fatal(e)
	}
	if e = s.Complete(quiet, "Succeeded", now); e != nil {
		t.Fatal(e)
	}
	if e = s.Seal(quiet); e != nil {
		t.Fatal(e)
	}
	capacity := false
	for n := 2; n < 40 && !capacity; n++ {
		id := identity(n)
		if e = s.Register(id); errors.Is(e, ErrCapacity) {
			capacity = true
			break
		} else if e != nil {
			t.Fatal(e)
		}
		for j := 0; j < 8; j++ {
			bytes := make([]byte, 45*1024)
			if _, e = rand.Read(bytes); e != nil {
				t.Fatal(e)
			}
			e = s.AppendBatch(id, uuid.NewString(), []Record{{ID: uuid.NewString(), Kind: "stdout", Text: base64.StdEncoding.EncodeToString(bytes)}})
			if errors.Is(e, ErrCapacity) {
				capacity = true
				break
			}
			if e != nil {
				t.Fatal(e)
			}
		}
	}
	if !capacity {
		t.Fatal("controlled global storage budget was not exercised")
	}
	used, budget := s.Usage()
	if used > budget {
		t.Fatalf("archive exceeded its storage budget: %d>%d", used, budget)
	}
	p, e := s.Read(quiet, Query{})
	if e != nil || len(p.Records) != 1 || p.Records[0].Text != "protected evidence" {
		t.Fatalf("capacity pressure evicted protected evidence: %+v %v", p, e)
	}
}

func TestRejectOutputLimitSmallerThanMaximumSegment(t *testing.T) {
	l := DefaultLimits()
	l.RunBytes = 64 * 1024
	l.HeadBytes = 16 * 1024
	if l.Validate() == nil {
		t.Fatal("accepted a run limit that a single supported segment could exceed")
	}
}
