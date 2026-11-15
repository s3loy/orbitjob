package executionpolicy

import (
	"strings"
	"testing"
)

func TestLegacyOmissionAndExplicitV1DefaultsAreDistinct(t *testing.T) {
	legacy, err := Normalize(0, false, 4, 3, nil)
	if err != nil || legacy != nil {
		t.Fatalf("legacy changed: resources=%+v err=%v", legacy, err)
	}
	current, err := Normalize(Version1, false, 0, 0, nil)
	if err != nil || len(current.Requests) != 3 || len(current.Limits) != 3 || current.Requests["memory"] != "128Mi" || current.Limits["memory"] != "256Mi" {
		t.Fatalf("v1 defaults not resolved: %+v err=%v", current, err)
	}
	current.Requests["memory"] = "1Gi"
	fresh, err := Normalize(Version1, false, 0, 1, nil)
	if err != nil || fresh.Requests["memory"] != "128Mi" {
		t.Fatal("one normalized snapshot mutated installation defaults")
	}
}

func TestRetriesRequireExplicitRepeatSafetyOnlyForV1(t *testing.T) {
	for _, values := range [][2]int32{{1, 1}, {0, 2}} {
		if _, err := Normalize(Version1, false, values[0], values[1], nil); err == nil {
			t.Fatal("repeated execution enabled without retrySafe")
		}
		if _, err := Normalize(Version1, true, values[0], values[1], nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Normalize(2, true, 0, 1, nil); err == nil {
		t.Fatal("unknown policy version accepted")
	}
}

func TestResourcesRejectUnsupportedInvalidOrExcessiveQuantities(t *testing.T) {
	for name, input := range map[string]*Resources{
		"unsupported":           {Limits: map[string]string{"nvidia.com/gpu": "1"}},
		"negative":              {Limits: map[string]string{"cpu": "-1"}},
		"zero":                  {Requests: map[string]string{"cpu": "0"}},
		"malformed":             {Limits: map[string]string{"memory": "big"}},
		"oversized quantity":    {Limits: map[string]string{"cpu": strings.Repeat("0", 64) + "1"}},
		"cpu precision":         {Requests: map[string]string{"cpu": "0.1m"}},
		"fractional bytes":      {Requests: map[string]string{"memory": "400m"}},
		"installation ceiling":  {Limits: map[string]string{"memory": "65Gi"}},
		"request exceeds limit": {Requests: map[string]string{"memory": "2Gi"}, Limits: map[string]string{"memory": "1Gi"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Normalize(Version1, false, 0, 1, input); err == nil {
				t.Fatalf("invalid resources accepted: %+v", input)
			}
		})
	}
}

func TestCustomProfileIsResolvedAndInputIsNotMutated(t *testing.T) {
	profile := StandardProfile()
	profile.Defaults.Requests["memory"] = "512Mi"
	profile.Defaults.Limits["memory"] = "1Gi"
	profile.Maximum["memory"] = "2Gi"
	input := &Resources{Requests: map[string]string{"cpu": "0.2"}}
	got, err := NormalizeWithProfile(Version1, false, 0, 1, input, profile)
	if err != nil || got.Requests["cpu"] != "200m" || got.Requests["memory"] != "512Mi" || input.Requests["cpu"] != "0.2" {
		t.Fatalf("profile resolution=%+v input=%+v err=%v", got, input, err)
	}
	if _, err := NormalizeWithProfile(Version1, false, 0, 1, &Resources{Limits: map[string]string{"memory": "3Gi"}}, profile); err == nil {
		t.Fatal("custom installation ceiling ignored")
	}
}

func TestResolvedPolicyPreservesAcceptedResourcesWithoutCurrentDefaults(t *testing.T) {
	r, err := Normalize(Version1, false, 0, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Limits["memory"] = "128Gi"
	if err := ValidateResolved(Version1, false, 0, 1, r); err != nil {
		t.Fatalf("accepted resources were compared with today's profile: %v", err)
	}
	delete(r.Requests, "memory")
	if err := ValidateResolved(Version1, false, 0, 1, r); err == nil {
		t.Fatal("missing accepted resource was filled from today's defaults")
	}
}
