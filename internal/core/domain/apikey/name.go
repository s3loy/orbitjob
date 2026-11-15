package apikey

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"orbitjob/internal/domain/validation"
)

// ValidateName validates optional human-readable metadata. An empty name
// preserves legacy creation and clears an existing alias when renamed.
func ValidateName(name string) error {
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 128 {
		return validation.New("name", "must contain at most 128 Unicode characters")
	}
	if strings.TrimSpace(name) != name || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return validation.New("name", "must not contain control characters or surrounding whitespace")
	}
	return nil
}
