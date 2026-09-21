package projection

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/check"
	"orbitjob/internal/core/domain/revision"
)

// ProbeImage is the container an http_health check executes in, pinned by
// digest so the image a probe runs can never drift underneath a stored
// revision. Digest looked up 2026-09-18 from registry-1.docker.io as the
// multi-arch manifest-list digest of the curlimages/curl "latest" tag.
const ProbeImage = "curlimages/curl@sha256:58adaa4e8dca9c988bae2aba4ab3434a0bb2da16bbe3f92dec39ec7785166777"

// probeContainerCommand runs the probe script through POSIX sh. The curl image's
// entrypoint is curl itself, so the command must be replaced to run the exit
// logic the check contract needs.
var probeContainerCommand = []string{"/bin/sh", "-c"}

// CheckJobSpec synthesizes the ScheduledJobSpec a check revision pins. The
// mapping (design section 2.1): the probe container runs curl with the check's
// url/method/timeout and exits 0 when the emitted HTTP status equals
// expected_status and 1 otherwise -- transport errors make curl fail on its
// own, which the comparison treats as any other mismatch. timeout_sec becomes
// the run deadline the operator projects onto the Kubernetes Job, and
// retry_limit (retries) becomes the platform attempt budget (attempts).
//
// Scheduling is deliberately absent: a check's cursor is checks.next_run_at,
// which the check scheduler owns; the synthesized spec has an empty Schedule so
// the job scheduler can never fire this revision.
func CheckJobSpec(c check.Snapshot) (v1alpha1.ScheduledJobSpec, error) {
	probe, err := check.NormalizeProbeConfig(c.CheckConfig)
	if err != nil {
		return v1alpha1.ScheduledJobSpec{}, fmt.Errorf("check %s: %w", check.CheckSourceUID(c.ID), err)
	}
	timeout := c.TimeoutSec
	if timeout < 1 {
		timeout = check.DefaultTimeoutSec
	}
	return v1alpha1.ScheduledJobSpec{
		TimeoutSeconds: int32(timeout),
		RetryPolicy:    v1alpha1.RetryPolicy{MaxAttempts: int32(c.RetryLimit + 1)},
		JobTemplate: v1alpha1.JobTemplateSpec{
			Image:   ProbeImage,
			Command: append([]string(nil), probeContainerCommand...),
			Args:    []string{probeScript(probe, timeout)},
		},
	}, nil
}

// probeScript renders the container's whole exit contract as one POSIX sh
// line: emit the response code, compare it to the expected status, and exit
// with the comparison. curl failures leave the capture empty, and an empty
// code matches nothing, so connection, TLS and timeout failures all land in
// the same non-zero exit a mismatch does.
func probeScript(probe check.ProbeConfig, timeoutSec int) string {
	return fmt.Sprintf(
		"code=$(curl -sS -o /dev/null -w '%%{http_code}' --max-time %d -X %s %s); [ \"$code\" = %d ]",
		timeoutSec, probe.Method, shSingleQuote(probe.URL), probe.ExpectedStatus,
	)
}

// shSingleQuote quotes s as a POSIX sh single-quoted literal, so a URL
// containing any shell metacharacter stays an argument instead of becoming
// script.
func shSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ApplyCheck materializes a check version as an immutable definition revision,
// the same store path a ScheduledJob CR projects through. The revision is
// keyed source_mode=check, source_uid=check-<id>, generation=checks.version,
// so re-projecting a version is idempotent and a version change makes a new
// revision while in-flight runs keep the one they were pinned to.
//
// The actor names the component that materializes the revision; like every
// revision writer, it must be a name the audit trail can attribute.
func (s Service) ApplyCheck(ctx context.Context, c check.Snapshot, namespace, tenantID, actor string) (int64, error) {
	if tenantID == "" {
		return 0, fmt.Errorf("tenant is required")
	}
	if namespace == "" {
		return 0, fmt.Errorf("namespace is required")
	}
	if s.Revisions == nil {
		return 0, fmt.Errorf("revision writer is required")
	}
	spec, err := CheckJobSpec(c)
	if err != nil {
		return 0, err
	}
	sourceUID := check.CheckSourceUID(c.ID)
	rev, err := revision.New(
		revision.Identity{
			SourceMode: check.SourceModeCheck,
			SourceUID:  sourceUID,
			Namespace:  namespace,
			Name:       sourceUID,
		},
		int64(c.Version),
		normalizeSpec(spec),
		actor,
		// The revision inherits the check row's group so the ledger carries the
		// scoping the check was created under; a run stamped from this revision
		// reports the same tier.
		c.ResourceGroupID,
		s.now(),
	)
	if err != nil {
		return 0, err
	}
	return s.Revisions.ApplyRevisionForTenant(ctx, tenantID, rev)
}

// CheckSpecJSON is CheckJobSpec rendered the way revisions store it. It exists
// so tests can pin the exact bytes a check version produces.
func CheckSpecJSON(c check.Snapshot) (string, error) {
	spec, err := CheckJobSpec(c)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		return "", fmt.Errorf("encode check spec: %w", err)
	}
	return string(raw), nil
}
