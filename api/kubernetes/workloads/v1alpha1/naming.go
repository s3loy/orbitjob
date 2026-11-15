package v1alpha1

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// RunObjectName retains the full occurrence digest. The definition prefix is
// descriptive only; uniqueness is established by the complete immutable spec.
func RunObjectName(definitionName, occurrenceKey string) string {
	digest := occurrenceKey
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != sha256.Size {
		sum := sha256.Sum256([]byte(occurrenceKey))
		digest = hex.EncodeToString(sum[:])
	}
	prefix := definitionName
	if len(prefix) > 188 {
		prefix = prefix[:188]
	}
	prefix = strings.Trim(prefix, "-.")
	if prefix == "" {
		prefix = "run"
	}
	return prefix + "-" + strings.ToLower(digest)
}

// LegacyRunObjectName is lookup-only compatibility for objects already returned
// to callers. Existing objects are never renamed or replaced during a repair.
func LegacyRunObjectName(definitionName, occurrenceKey string) string {
	suffix := occurrenceKey
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	if suffix == "" {
		suffix = "unknown"
	}
	return definitionName + "-" + suffix
}

func RunNameCandidates(definitionName, occurrenceKey string) []string {
	modern, legacy := RunObjectName(definitionName, occurrenceKey), LegacyRunObjectName(definitionName, occurrenceKey)
	if modern == legacy || len(legacy) > 253 {
		return []string{modern}
	}
	return []string{modern, legacy}
}

func WorkflowRunObjectName(definitionName, occurrenceKey string) string {
	return RunObjectName(definitionName, occurrenceKey)
}
