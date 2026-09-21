package config

import (
	"fmt"
	"os"
	"strings"
)

// ResolveDatabaseDSN returns the first non-empty database DSN from keys.
// Callers list variables from most specific to legacy fallback.
func ResolveDatabaseDSN(keys ...string) (dsn string, source string, err error) {
	if len(keys) == 0 {
		return "", "", fmt.Errorf("database DSN variable list is empty")
	}
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value, key, nil
		}
	}
	return "", "", fmt.Errorf("%s is required", strings.Join(keys, " or "))
}
