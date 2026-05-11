package config

import (
	"fmt"
	"os"
	"strconv"
)

// LoadPositiveIntEnv reads a positive integer from the environment variable named by key.
// Returns defaultValue when key is not set or empty. Returns an error when the value
// is not a valid integer or is < 1.
func LoadPositiveIntEnv(key string, defaultValue int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return defaultValue, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	if value < 1 {
		return 0, fmt.Errorf("%s must be >= 1", key)
	}

	return value, nil
}
