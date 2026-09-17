package projection

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"orbitjob/internal/core/domain/check"
	"orbitjob/internal/core/domain/revision"
)

func checkFixture() check.Snapshot {
	cron := "*/5 * * * *"
	return check.Snapshot{
		ID:          42,
		Name:        "api-health",
		TenantID:    "00000000000000000000000001",
		CheckType:   check.CheckTypeHTTPHealth,
		CheckConfig: map[string]any{"url": "http://api.local:9000/health", "method": "GET", "expected_status": 200},
		CronExpr:    &cron,
		Version:     3,
		TimeoutSec:  30,
		RetryLimit:  2,
	}
}

// TestCheckSpecGoldenJSON pins the exact spec bytes a check version produces.
// The spec hash is what makes re-projection idempotent, so any change to the
// rendering is a deliberate revision-rolling act and must land here.
func TestCheckSpecGoldenJSON(t *testing.T) {
	got, err := CheckSpecJSON(checkFixture())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `{"schedule":"","history":{},"timeoutSeconds":30,"jobTemplate":{"image":"curlimages/curl@sha256:58adaa4e8dca9c988bae2aba4ab3434a0bb2da16bbe3f92dec39ec7785166777",` +
		`"command":["/bin/sh","-c"],` +
		`"args":["code=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 30 -X GET 'http://api.local:9000/health'); [ \"$code\" = 200 ]"]},` +
		`"retryPolicy":{"maxAttempts":3}}`
	if got != want {
		t.Fatalf("check spec json =\n%s\nwant\n%s", got, want)
	}
}

// TestCheckJobSpecDefaults covers the fields v1 does not map: no Schedule (the
// job scheduler must never fire a check revision), no suspend, and the
// retry-to-attempt translation.
func TestCheckJobSpecDefaults(t *testing.T) {
	c := checkFixture()
	spec, err := CheckJobSpec(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Schedule != "" {
		t.Fatalf("schedule = %q, want empty: checks are fired from next_run_at, not from the spec", spec.Schedule)
	}
	if spec.Suspend {
		t.Fatal("suspend = true, want false")
	}
	if got, want := spec.RetryPolicy.EffectiveMaxAttempts(), c.RetryLimit+1; got != want {
		t.Fatalf("attempts = %d, want retry_limit + 1 = %d", got, want)
	}
	if spec.TimeoutSeconds != int32(c.TimeoutSec) {
		t.Fatalf("timeout = %d, want %d", spec.TimeoutSeconds, c.TimeoutSec)
	}
	if spec.JobTemplate.Image != ProbeImage {
		t.Fatalf("image = %q, want the pinned probe image", spec.JobTemplate.Image)
	}
}

// TestCheckJobSpecRejectsStoredGarbage proves a stored config that predates
// boundary validation fails at synthesis: a check whose config cannot render
// must be a loud firing error, never a Job that fails opaquely at runtime.
func TestCheckJobSpecRejectsStoredGarbage(t *testing.T) {
	c := checkFixture()
	c.CheckConfig = map[string]any{"expected_status": "200 OK"}
	if _, err := CheckJobSpec(c); err == nil {
		t.Fatal("expected error for stored config without a url")
	}
}

type fakeRevisionWriter struct {
	calls    []revision.Revision
	tenant   []string
	response int64
}

func (f *fakeRevisionWriter) ApplyRevisionForTenant(_ context.Context, tenantID string, rev revision.Revision) (int64, error) {
	f.calls = append(f.calls, rev)
	f.tenant = append(f.tenant, tenantID)
	return f.response, nil
}

// TestApplyCheckIdentity pins the revision identity a check materializes
// under: mode, uid, name and generation all derive from the check row, and
// the spec hash is the sha256 of the normalized spec.
func TestApplyCheckIdentity(t *testing.T) {
	now := time.Date(2026, 9, 18, 4, 0, 0, 0, time.UTC)
	writer := &fakeRevisionWriter{response: 7}
	svc := Service{Revisions: writer, Now: func() time.Time { return now }}

	c := checkFixture()
	id, err := svc.ApplyCheck(context.Background(), c, "tasks", "00000000000000000000000001", "orbitjob-check-scheduler")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 7 {
		t.Fatalf("id = %d, want writer response 7", id)
	}
	if len(writer.calls) != 1 || writer.tenant[0] != "00000000000000000000000001" {
		t.Fatalf("writer calls = %d, tenant = %q", len(writer.calls), writer.tenant)
	}
	rev := writer.calls[0]
	if rev.Identity.SourceMode != check.SourceModeCheck {
		t.Fatalf("source_mode = %q, want %q", rev.Identity.SourceMode, check.SourceModeCheck)
	}
	if rev.Identity.SourceUID != "check-42" || rev.Identity.Name != "check-42" {
		t.Fatalf("uid/name = %q/%q, want check-42 twice", rev.Identity.SourceUID, rev.Identity.Name)
	}
	if rev.Identity.Namespace != "tasks" {
		t.Fatalf("namespace = %q, want tasks", rev.Identity.Namespace)
	}
	if rev.Generation != 3 {
		t.Fatalf("generation = %d, want the check version 3", rev.Generation)
	}
	if rev.Actor != "orbitjob-check-scheduler" {
		t.Fatalf("actor = %q, want orbitjob-check-scheduler", rev.Actor)
	}
	spec, err := CheckSpecJSON(c)
	if err != nil {
		t.Fatalf("spec json: %v", err)
	}
	sum := sha256.Sum256([]byte(spec))
	if rev.SpecHash != hex.EncodeToString(sum[:]) {
		t.Fatal("spec hash is not the sha256 of the normalized spec")
	}
}

// TestApplyCheckDeterministicSpec pins that the same check version projects
// the same bytes twice: re-projection must be a no-op conflict, not a new
// hash.
func TestApplyCheckDeterministicSpec(t *testing.T) {
	now := time.Date(2026, 9, 18, 4, 0, 0, 0, time.UTC)
	writer := &fakeRevisionWriter{}
	svc := Service{Revisions: writer, Now: func() time.Time { return now }}
	c := checkFixture()

	for i := 0; i < 2; i++ {
		if _, err := svc.ApplyCheck(context.Background(), c, "tasks", "00000000000000000000000001", "orbitjob-check-scheduler"); err != nil {
			t.Fatalf("pass %d: %v", i, err)
		}
	}
	if writer.calls[0].SpecHash != writer.calls[1].SpecHash {
		t.Fatalf("hashes diverge: %s vs %s", writer.calls[0].SpecHash, writer.calls[1].SpecHash)
	}
}

// TestApplyCheckValidation refuses the inputs the revision layer cannot
// express.
func TestApplyCheckValidation(t *testing.T) {
	now := time.Date(2026, 9, 18, 4, 0, 0, 0, time.UTC)
	svc := Service{Revisions: &fakeRevisionWriter{}, Now: func() time.Time { return now }}
	c := checkFixture()

	if _, err := svc.ApplyCheck(context.Background(), c, "tasks", "", "a"); err == nil {
		t.Fatal("expected error for empty tenant")
	}
	if _, err := svc.ApplyCheck(context.Background(), c, "", "00000000000000000000000001", "a"); err == nil {
		t.Fatal("expected error for empty namespace")
	}
	if _, err := (Service{Now: func() time.Time { return now }}).ApplyCheck(context.Background(), c, "tasks", "00000000000000000000000001", "a"); err == nil {
		t.Fatal("expected error for missing revision writer")
	}
	if _, err := svc.ApplyCheck(context.Background(), check.Snapshot{Version: 0}, "tasks", "00000000000000000000000001", "a"); err == nil {
		t.Fatal("expected revision error for version 0")
	}
}
