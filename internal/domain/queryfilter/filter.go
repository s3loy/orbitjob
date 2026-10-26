// Package queryfilter validates shared collection predicates before storage.
package queryfilter

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"orbitjob/internal/domain/validation"
)

// Search is a literal, case-insensitive substring, not a wildcard expression.
func Search(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.ContainsRune(value, 0) {
		return "", validation.New("search", "must not contain a NUL character")
	}
	if utf8.RuneCountInString(value) > 128 {
		return "", validation.New("search", "must be at most 128 characters")
	}
	return value, nil
}

var rfc3339 = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)$`)

// Range uses an inclusive lower bound and exclusive upper bound. UTC strings
// preserve subsecond precision; storage adapts bounds to its comparison grid.
func Range(from, before string) (string, string, error) {
	var lower, upper time.Time
	for _, bound := range []struct {
		field, value string
		target       *time.Time
	}{{"created_from", from, &lower}, {"created_before", before, &upper}} {
		if bound.value == "" {
			continue
		}
		value, err := time.Parse(time.RFC3339Nano, bound.value)
		if err != nil || !rfc3339.MatchString(bound.value) || value.UTC().Year() < 1 || value.UTC().Year() > 9999 {
			return "", "", validation.New(bound.field, "must be an RFC3339 timestamp with timezone and at most 9 fractional digits")
		}
		*bound.target = value
	}
	if from != "" && before != "" && !lower.Before(upper) {
		return "", "", validation.New("created_before", "must be after created_from")
	}
	if from != "" {
		from = lower.UTC().Format(time.RFC3339Nano)
	}
	if before != "" {
		before = upper.UTC().Format(time.RFC3339Nano)
	}
	return from, before, nil
}

// MicrosecondBound preserves inclusive-lower/exclusive-upper comparisons on a
// microsecond timestamp grid. PostgreSQL rounds textual nanoseconds; rounding
// either bound down would admit an extra lower-edge row or omit an upper-edge
// row. Ceil both bounds instead. Validation remains Range's responsibility;
// invalid input is retained so a direct storage caller still fails closed.
func MicrosecondBound(value string) string {
	if value == "" {
		return ""
	}
	instant, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	bound := instant.Truncate(time.Microsecond)
	if bound.Before(instant) {
		bound = bound.Add(time.Microsecond)
	}
	return bound.UTC().Format(time.RFC3339Nano)
}
