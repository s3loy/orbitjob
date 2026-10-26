package logarchive

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAuditCollectorRecreationAndRegroupedReplay(t *testing.T) {
	now := time.Now()
	s := openTest(t, &now)
	id := identity(101)
	if e := s.Register(id); e != nil {
		t.Fatal(e)
	}
	podUID := uuid.NewString()
	if e := s.Bind(id, Source{Namespace: "own", PodName: "pod", PodUID: podUID, JobUID: uuid.NewString(), Attempt: 1, Container: "task"}); e != nil {
		t.Fatal(e)
	}
	o := &Observer{Store: s}
	var records []Input
	for i := 0; i < 1800; i++ {
		in := sourceInput(podUID, fmt.Sprintf("source record %d", i))
		in.Offset = int64(i * 128)
		records = append(records, in)
	}
	send := func(in []Input) {
		var body bytes.Buffer
		for _, v := range in {
			if e := json.NewEncoder(&body).Encode(v); e != nil {
				t.Fatal(e)
			}
		}
		req := collectorRequest(in[0])
		req.Body = ioAuditBody(&body)
		out := httptest.NewRecorder()
		o.ingestHTTP(out, req)
		if out.Code != 200 || strings.Contains(out.Body.String(), `"rejected":1`) {
			t.Fatalf("ingest failed: %d %s", out.Code, out.Body.String())
		}
	}
	// First collector chunks at 300 records; replacement collector rereads
	// identical source content with entirely new UUIDs and 17-record chunks.
	for i := 0; i < len(records); i += 300 {
		end := i + 300
		if end > len(records) {
			end = len(records)
		}
		send(records[i:end])
	}
	before, e := s.Snapshot(id)
	if e != nil {
		t.Fatal(e)
	}
	for i := range records {
		records[i].ID = uuid.NewString()
	}
	for i := 0; i < len(records); i += 17 {
		end := i + 17
		if end > len(records) {
			end = len(records)
		}
		send(records[i:end])
	}
	after, e := s.Snapshot(id)
	if e != nil {
		t.Fatal(e)
	}
	if before.OutputBytes != after.OutputBytes || before.Next != after.Next {
		t.Fatalf("collector recreation/rebatch duplicated output: before bytes=%d seq=%d after bytes=%d seq=%d", before.OutputBytes, before.Next, after.OutputBytes, after.Next)
	}
}

type auditReadCloser struct{ *bytes.Buffer }

func (auditReadCloser) Close() error              { return nil }
func ioAuditBody(b *bytes.Buffer) auditReadCloser { return auditReadCloser{b} }
