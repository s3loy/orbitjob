package config

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/joho/godotenv"
)

var (
	loadDotenvOnce sync.Once
	loadDotenvErr  error
)

// LoadDotenv loads a local .env file for development and tests.
// Missing .env is ignored so CI and production can rely on real environment variables.
func LoadDotenv() error {
	loadDotenvOnce.Do(func() {
		path, err := findDotenv(".env")
		switch {
		case err == nil:
			loadDotenvErr = godotenv.Load(path)
		case errors.Is(err, os.ErrNotExist):
			loadDotenvErr = nil
		default:
			loadDotenvErr = err
		}
	})

	return loadDotenvErr
}

func findDotenv(name string) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	return findDotenvFrom(wd, name)
}

func findDotenvFrom(startDir, name string) (string, error) {
	dir := startDir
	for {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		switch {
		case err == nil && !info.IsDir():
			return path, nil
		case err != nil && !errors.Is(err, os.ErrNotExist):
			return "", err
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
