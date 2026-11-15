package logarchive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	kfake "k8s.io/client-go/kubernetes/fake"
	kt "k8s.io/client-go/testing"
)

func bindCollectorSource(t *testing.T, s *Store, id Identity) string {
	t.Helper()
	pod := uuid.NewString()
	if err := s.Register(id); err != nil {
		t.Fatal(err)
	}
	if err := s.Bind(id, Source{Namespace: id.Namespace, PodName: "pod", PodUID: pod, JobUID: uuid.NewString(), Attempt: 1, Container: "task"}); err != nil {
		t.Fatal(err)
	}
	return pod
}

func collectorBatch(inputs []Input) []byte {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	for _, in := range inputs {
		_ = encoder.Encode(in)
	}
	return body.Bytes()
}

func ingestBatch(o *Observer, body []byte) *httptest.ResponseRecorder {
	r := collectorRequest(Input{})
	r.Body = ioAuditBody(bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	o.ingestHTTP(w, r)
	return w
}

func TestCollectorRejectsMalformedTailBeforeAnyOutput(t *testing.T) {
	now := time.Now()
	s := openTest(t, &now)
	id := identity(501)
	pod := bindCollectorSource(t, s, id)
	var inputs []Input
	for i := 0; i < 1100; i++ {
		in := sourceInput(pod, "prefix")
		in.Offset = int64(i)
		inputs = append(inputs, in)
	}
	body := append(collectorBatch(inputs), []byte("{broken tail")...)
	w := ingestBatch(&Observer{Store: s}, body)
	m, err := s.Snapshot(id)
	if w.Code != http.StatusBadRequest || err != nil || m.OutputBytes != 0 || len(m.Segments) != 0 {
		t.Fatalf("malformed batch committed its prefix: HTTP%d %+v %v", w.Code, m, err)
	}
}

func TestCollectorStructuralBudgetsPrecedeAppend(t *testing.T) {
	for _, kind := range []string{"records", "sources"} {
		t.Run(kind, func(t *testing.T) {
			now := time.Now()
			s := openTest(t, &now)
			id := identity(502)
			pod := bindCollectorSource(t, s, id)
			count := 20001
			if kind == "sources" {
				count = 257
			}
			inputs := make([]Input, 0, count+1100)
			if kind == "sources" {
				for i := 0; i < 1100; i++ {
					in := sourceInput(pod, "prefix")
					in.Offset = int64(i)
					inputs = append(inputs, in)
				}
			}
			for i := 0; i < count; i++ {
				p := pod
				if kind == "sources" {
					p = uuid.NewString()
				}
				in := sourceInput(p, "line")
				in.Offset = int64(i)
				inputs = append(inputs, in)
			}
			w := ingestBatch(&Observer{Store: s}, collectorBatch(inputs))
			m, err := s.Snapshot(id)
			if w.Code != http.StatusRequestEntityTooLarge || err != nil || m.OutputBytes != 0 {
				t.Fatalf("%s limit committed prefix: HTTP%d %+v %v", kind, w.Code, m, err)
			}
		})
	}
}

func TestCollectorResolvesAllSourcesBeforeAppendingOutput(t *testing.T) {
	now := time.Now()
	s := openTest(t, &now)
	id := identity(503)
	pod := bindCollectorSource(t, s, id)
	k := kfake.NewSimpleClientset()
	k.PrependReactor("get", "pods", func(kt.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewServiceUnavailable("temporary source read")
	})
	var inputs []Input
	for i := 0; i < 1100; i++ {
		in := sourceInput(pod, "prefix")
		in.Offset = int64(i)
		inputs = append(inputs, in)
	}
	inputs = append(inputs, sourceInput(uuid.NewString(), "not verified yet"))
	w := ingestBatch(&Observer{Store: s, Kube: k, Namespaces: map[string]string{"own": "tenant"}}, collectorBatch(inputs))
	m, err := s.Snapshot(id)
	if w.Code != http.StatusServiceUnavailable || err != nil || m.OutputBytes != 0 {
		t.Fatalf("source failure followed committed output: HTTP%d %+v %v", w.Code, m, err)
	}
}

func TestCollectorPartialPersistenceReplaysWithoutDuplicateOutput(t *testing.T) {
	now := time.Now()
	s := openTest(t, &now)
	ids := []Identity{identity(504), identity(505)}
	sort.Slice(ids, func(i, j int) bool { return ids[i].Key() < ids[j].Key() })
	inputs := []Input{sourceInput(bindCollectorSource(t, s, ids[0]), "first source"), sourceInput(bindCollectorSource(t, s, ids[1]), "second source")}
	blocked := filepath.Join(s.root.Name(), ids[1].Key(), fmt.Sprintf("%020d.gz.tmp", 1))
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	body := collectorBatch(inputs)
	o := &Observer{Store: s}
	w := ingestBatch(o, body)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("partial persistence incorrectly ACKed: %d %s", w.Code, w.Body.String())
	}
	first, _ := s.Snapshot(ids[0])
	second, _ := s.Snapshot(ids[1])
	if first.OutputBytes == 0 || second.OutputBytes != 0 {
		t.Fatal("test did not interrupt after the first durable group")
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	w = ingestBatch(o, body)
	if w.Code != http.StatusOK {
		t.Fatalf("replay failed: %d %s", w.Code, w.Body.String())
	}
	for _, id := range ids {
		p, err := s.Read(id, Query{})
		if err != nil || len(p.Records) != 1 {
			t.Fatalf("replay duplicated/lost output: %+v %v", p, err)
		}
	}
}

func TestInvalidRecordDoesNotBlockHealthyRecordsAndPersistsLoss(t *testing.T) {
	now := time.Now()
	s := openTest(t, &now)
	id := identity(506)
	pod := bindCollectorSource(t, s, id)
	bad := sourceInput(pod, "invalid")
	bad.Log = "not CRI"
	w := ingestBatch(&Observer{Store: s}, collectorBatch([]Input{bad, sourceInput(pod, "healthy")}))
	p, err := s.Read(id, Query{})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"rejected":1`) || err != nil || len(p.Records) != 1 || p.State != "partial" || len(p.Gaps) == 0 {
		t.Fatalf("invalid record blocked/hid loss: HTTP%d %+v %v", w.Code, p, err)
	}
}

func TestReadCancellationAndArchiveCoordinationDoNotBlockOtherArchives(t *testing.T) {
	now := time.Now()
	s := openTest(t, &now)
	first, other := identity(507), identity(508)
	for _, id := range []Identity{first, other} {
		if err := s.Register(id); err != nil {
			t.Fatal(err)
		}
	}
	release, err := s.coordinate(context.Background(), "archive:"+first.Key(), 1)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	read := make(chan error, 1)
	go func() { _, err := s.ReadContext(ctx, first, Query{}); read <- err }()
	write := make(chan error, 1)
	go func() {
		write <- s.Append(other, []Record{{ID: uuid.NewString(), Kind: "stdout", Text: "other tenant keeps progressing"}})
	}()
	select {
	case err := <-write:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued read held the global metadata lock")
	}
	cancel()
	select {
	case err := <-read:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("read ignored cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued read did not cancel")
	}
	release()
	s.mu.Lock()
	entries := len(s.coordinators)
	s.mu.Unlock()
	if entries != 0 {
		t.Fatalf("coordinator entries leaked: %d", entries)
	}
}

func TestCorruptSegmentReturnsVerifiedRemainderAndExplicitGap(t *testing.T) {
	now := time.Now()
	s := openTest(t, &now)
	id := identity(509)
	if err := s.Register(id); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if err := s.Append(id, []Record{{ID: uuid.NewString(), Kind: "stdout", Text: strings.Repeat(fmt.Sprint(i), MaxRecordBytes)}}); err != nil {
			t.Fatal(err)
		}
	}
	m, _ := s.Snapshot(id)
	if len(m.Segments) != 3 {
		t.Fatalf("need three segments: %d", len(m.Segments))
	}
	bad := m.Segments[1]
	if err := os.WriteFile(filepath.Join(s.root.Name(), id.Key(), bad.Name), []byte("corrupt gzip"), 0600); err != nil {
		t.Fatal(err)
	}
	var after int64
	var count int
	gap := false
	for pages := 0; pages < 4; pages++ {
		p, err := s.Read(id, Query{After: after})
		if err != nil {
			t.Fatal(err)
		}
		count += len(p.Records)
		for _, g := range p.Gaps {
			if p.State == "partial" && g.From == bad.First && g.Through == bad.Last {
				gap = true
			}
		}
		after = p.Next
		if !p.HasMore {
			break
		}
	}
	if count != 4 || !gap {
		t.Fatalf("corruption hid healthy evidence or its gap: records=%d gap=%v", count, gap)
	}
}

func TestBadManifestAndMissingManifestAreIsolatedWithoutDeletingEvidence(t *testing.T) {
	for _, mode := range []string{"malformed", "checksum", "missing"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now()
			dir := t.TempDir()
			limits := testLimits(&now)
			s, err := Open(dir, limits)
			if err != nil {
				t.Fatal(err)
			}
			good, bad := identity(510), identity(511)
			for _, id := range []Identity{good, bad} {
				if err = s.Register(id); err != nil {
					t.Fatal(err)
				}
				if err = s.Append(id, []Record{{ID: uuid.NewString(), Kind: "stdout", Text: "preserve"}}); err != nil {
					t.Fatal(err)
				}
			}
			m, _ := s.Snapshot(bad)
			segment := filepath.Join(dir, bad.Key(), m.Segments[0].Name)
			before, _ := os.ReadFile(segment)
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			manifest := filepath.Join(dir, bad.Key(), "manifest.json")
			switch mode {
			case "malformed":
				err = os.WriteFile(manifest, []byte("{broken"), 0600)
			case "missing":
				err = os.Remove(manifest)
			case "checksum":
				data, _ := os.ReadFile(manifest)
				var m Manifest
				_ = json.Unmarshal(data, &m)
				m.OutputBytes++
				data, _ = json.Marshal(m)
				err = os.WriteFile(manifest, data, 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			s, err = Open(dir, limits)
			if err != nil {
				t.Fatalf("one corrupt archive stopped healthy service: %v", err)
			}
			defer func() { _ = s.Close() }()
			p, err := s.Read(good, Query{})
			if err != nil || len(p.Records) != 1 {
				t.Fatalf("healthy evidence unavailable: %+v %v", p, err)
			}
			p, err = s.Read(bad, Query{})
			if err != nil || p.State != "unavailable" || len(p.Gaps) != 1 || !strings.Contains(p.Reason, "quarantined") {
				t.Fatalf("bad archive looks empty/complete: %+v %v", p, err)
			}
			if err = s.Register(bad); !errors.Is(err, ErrQuarantined) {
				t.Fatalf("quarantine replaced by fresh archive: %v", err)
			}
			after, err := os.ReadFile(segment)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("quarantine discarded original segment")
			}
		})
	}
}

func TestIdentityPathMismatchStillStopsStore(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	limits := testLimits(&now)
	s, err := Open(dir, limits)
	if err != nil {
		t.Fatal(err)
	}
	id := identity(512)
	if err = s.Register(id); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if err = os.Rename(filepath.Join(dir, id.Key()), filepath.Join(dir, identity(513).Key())); err != nil {
		t.Fatal(err)
	}
	if other, err := Open(dir, limits); err == nil {
		_ = other.Close()
		t.Fatal("untrusted root identity was isolated as ordinary file corruption")
	}
}

func TestReadOnlyInspectionPreservesCorruptFiles(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	limits := testLimits(&now)
	s, err := Open(dir, limits)
	if err != nil {
		t.Fatal(err)
	}
	id := identity(514)
	if err = s.Register(id); err != nil {
		t.Fatal(err)
	}
	if err = s.Append(id, []Record{{ID: uuid.NewString(), Kind: "stdout", Text: "preserve original evidence"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = Inspect(context.Background(), dir, limits); err == nil {
		t.Fatal("inspection raced an active archive writer")
	}
	m, _ := s.Snapshot(id)
	_ = s.Close()
	file := filepath.Join(dir, id.Key(), m.Segments[0].Name)
	if err = os.WriteFile(file, []byte("bad gzip remains unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dir, id.Key(), "manifest.json")
	before, _ := os.ReadFile(manifest)
	report, err := Inspect(context.Background(), dir, limits)
	if err != nil || report.Archives != 1 || len(report.Findings) != 1 {
		t.Fatalf("inspection did not locate corruption: %+v %v", report, err)
	}
	after, _ := os.ReadFile(manifest)
	segment, _ := os.ReadFile(file)
	if !bytes.Equal(before, after) || string(segment) != "bad gzip remains unchanged" {
		t.Fatal("read-only inspection modified evidence")
	}
}
