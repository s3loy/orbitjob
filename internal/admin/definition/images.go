package definition

import (
	"context"
	"encoding/json"

	v1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/domain/validation"
)

type ImageResolver interface {
	Pin(context.Context, string) (string, error)
}
type legacyAuthoringKey struct{}

func policyVersion(spec string) (int32, error) {
	var policy struct {
		Version int32 `json:"executionPolicyVersion"`
	}
	if err := json.Unmarshal([]byte(spec), &policy); err != nil {
		return 0, err
	}
	return policy.Version, nil
}

func (s Service) prepareImage(ctx context.Context, kind, spec string) (string, error) {
	version, err := policyVersion(spec)
	if err != nil {
		return "", err
	}
	if version != 1 || kind != "jobs" {
		return spec, nil
	}
	if s.Images == nil {
		return "", validation.New("jobTemplate.image", "immutable image resolution is unavailable; keep the draft and retry later")
	}
	var desired v1.ScheduledJobSpec
	if err = json.Unmarshal([]byte(spec), &desired); err != nil {
		return "", err
	}
	desired.JobTemplate.Image, err = s.Images.Pin(ctx, desired.JobTemplate.Image)
	if err != nil {
		return "", validation.New("jobTemplate.image", err.Error())
	}
	encoded, err := json.Marshal(desired)
	if err != nil {
		return "", err
	}
	canonical, _, err := CanonicalSpec(kind, string(encoded))
	return canonical, err
}
