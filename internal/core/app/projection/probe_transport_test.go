package projection

import (
	"context"
	"net/http"
	"net/http/httptest"
	"orbitjob/internal/core/domain/check"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestProbeRejectsTransportFailureAfterExpectedHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("x"))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer server.Close()
	spec, e := CheckJobSpec(check.Snapshot{ID: 1, TimeoutSec: 1, CheckConfig: map[string]any{"url": server.URL}})
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	out, e := exec.CommandContext(ctx, "/bin/sh", append([]string{"-c"}, spec.JobTemplate.Args...)...).CombinedOutput()
	if e == nil {
		t.Fatalf("probe accepted curl timeout: %s", out)
	}
	if !strings.Contains(string(out), "curl: (28)") {
		t.Fatalf("expected curl timeout: %s", out)
	}
	t.Logf("transport failure rejected: %s", out)
}
