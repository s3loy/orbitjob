// Package definition limits online authoring to the two workload definition kinds.
package definition

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	v1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/domain/validation"
	"orbitjob/internal/platform/strictjson"
)

// CanonicalSpec rejects unknown fields, trailing values and arbitrary Kubernetes
// metadata, then marshals the typed value without a float64 intermediate.
func CanonicalSpec(kind, input string) (string, string, error) {
	if err := strictjson.Validate([]byte(input)); err != nil {
		return "", "", validation.New("spec_json", err.Error())
	}
	var value any
	switch kind {
	case "jobs":
		value = &v1.ScheduledJobSpec{}
	case "workflows":
		value = &v1.WorkflowJobSpec{}
	default:
		return "", "", validation.New("kind", "unsupported definition kind")
	}
	d := json.NewDecoder(bytes.NewBufferString(input))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return "", "", validation.New("spec_json", "invalid typed spec: "+err.Error())
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return "", "", validation.New("spec_json", "exactly one spec object is required")
	}
	switch s := value.(type) {
	case *v1.ScheduledJobSpec:
		if err := s.NormalizeExecutionPolicy(); err != nil {
			return "", "", validation.New("executionPolicyVersion", err.Error())
		}
		if s.JobTemplate.Image == "" || s.TimeoutSeconds < 0 || s.JobTemplate.BackoffLimit < 0 || s.RetryPolicy.MaxAttempts < 0 {
			return "", "", validation.New("spec_json", "image required and numeric limits must be nonnegative")
		}
	case *v1.WorkflowJobSpec:
		if err := s.Validate(); err != nil {
			return "", "", validation.New("spec_json", err.Error())
		}
	}
	b, err := json.Marshal(value)
	if err != nil {
		return "", "", fmt.Errorf("marshal typed spec: %w", err)
	}
	sum := sha256.Sum256(append([]byte(kind+"\n"), b...))
	return string(b), hex.EncodeToString(sum[:]), nil
}

type ApplyRequest struct {
	AdmissionProof          string `json:"-"`
	RequestID               string `json:"request_id" binding:"required"`
	ExpectedUID             string `json:"expected_uid"`
	ExpectedResourceVersion string `json:"expected_resource_version"`
	SpecJSON                string `json:"spec_json" binding:"required"`
	DryRun                  bool   `json:"dry_run"`
	ManifestDigest          string `json:"manifest_digest,omitempty"`
}

type Result struct {
	Kind               string `json:"kind"`
	Name               string `json:"name"`
	Namespace          string `json:"namespace"`
	UID                string `json:"uid"`
	ResourceVersion    string `json:"resource_version"`
	Generation         int64  `json:"generation"`
	SpecJSON           string `json:"spec_json"`
	ManifestDigest     string `json:"manifest_digest"`
	LastRequestID      string `json:"last_request_id"`
	LastManifestDigest string `json:"last_manifest_digest"`
	RequestID          string `json:"request_id,omitempty"`
	State              string `json:"state,omitempty"`
}
