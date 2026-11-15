package function

import (
	"encoding/json"
	"math"
	"strings"
	"unicode/utf8"

	"orbitjob/internal/domain/executionpolicy"
	"orbitjob/internal/domain/validation"
)

// ManagedWrite is an authenticated, normalized configuration mutation.
// Expected version is Definition.Version; create assigns version one.
type ManagedWrite struct {
	Definition     Definition
	ActorID        string
	IdempotencyKey string
	PayloadHash    string
	CallerGroupID  string
}

// NormalizeManagedDefinition validates execution fields before persistence.
// Numeric bounds also prevent overflow when projection renders int32 fields.
func NormalizeManagedDefinition(d Definition) (Definition, error) {
	d.TenantID = strings.TrimSpace(d.TenantID)
	d.Name = strings.TrimSpace(d.Name)
	d.Image = strings.TrimSpace(d.Image)
	for field, value := range map[string]string{"name": d.Name, "description": d.Description, "image": d.Image} {
		if strings.ContainsRune(value, '\x00') {
			return Definition{}, validation.New(field, "must not contain null characters")
		}
	}
	if len(d.TenantID) != 26 {
		return Definition{}, validation.New("tenant_id", "must be a 26-character tenant id")
	}
	if d.ResourceGroupID != "" && len(d.ResourceGroupID) != 26 {
		return Definition{}, validation.New("resource_group_id", "must be a 26-character group id")
	}
	if d.Name == "" || utf8.RuneCountInString(d.Name) > 128 {
		return Definition{}, validation.New("name", "must contain between 1 and 128 characters")
	}
	if d.Image == "" || utf8.RuneCountInString(d.Image) > 255 {
		return Definition{}, validation.New("image", "must contain between 1 and 255 characters")
	}
	if d.Status != StatusActive && d.Status != StatusPaused {
		return Definition{}, validation.New("status", "must be active or paused")
	}
	if d.TimeoutSeconds < 1 || d.TimeoutSeconds > math.MaxInt32 {
		return Definition{}, validation.New("timeout_seconds", "must be between 1 and 2147483647")
	}
	for field, value := range map[string]int{"retry_limit": d.RetryLimit, "history_success": d.HistorySuccess, "history_failed": d.HistoryFailed} {
		if value < 0 || value >= math.MaxInt32 {
			return Definition{}, validation.New(field, "must be between 0 and 2147483646")
		}
	}
	resources, err := executionpolicy.Normalize(d.ExecutionPolicyVersion, d.RetrySafe, 0, int32(d.RetryLimit+1), d.Resources)
	if err != nil {
		return Definition{}, validation.New("execution_policy", err.Error())
	}
	d.Resources = resources
	if d.Command == nil {
		d.Command = []string{}
	}
	if d.Args == nil {
		d.Args = []string{}
	}
	if d.Labels == nil {
		d.Labels = map[string]any{}
	}
	encodedLabels, err := json.Marshal(d.Labels)
	if err != nil {
		return Definition{}, validation.New("labels", "must be a JSON object")
	}
	var decodedLabels any
	if err := json.Unmarshal(encodedLabels, &decodedLabels); err != nil || hasNullJSONText(decodedLabels) {
		return Definition{}, validation.New("labels", "must not contain null characters")
	}
	for field, values := range map[string][]string{"command": d.Command, "args": d.Args} {
		for _, value := range values {
			if strings.ContainsRune(value, '\x00') {
				return Definition{}, validation.New(field, "must not contain null characters")
			}
		}
	}
	return d, nil
}

func hasNullJSONText(value any) bool {
	switch typed := value.(type) {
	case string:
		return strings.ContainsRune(typed, '\x00')
	case map[string]any:
		for key, child := range typed {
			if strings.ContainsRune(key, '\x00') || hasNullJSONText(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if hasNullJSONText(child) {
				return true
			}
		}
	}
	return false
}
