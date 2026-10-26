package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// SecretWriter persists bootstrap secrets to a backend such as a local file
// or a Kubernetes Secret.
type SecretWriter interface {
	// Write stores data under name. For local files name becomes a directory;
	// for Kubernetes it becomes the Secret name.
	//
	// Implementations are not required to be atomic across multiple keys in
	// data: if writing one key fails, keys written earlier may remain in the
	// backend. Callers that need atomic multi-key updates must ensure data
	// contains only one key or provide an atomic implementation.
	Write(ctx context.Context, name string, data map[string]string) error
}

// LocalFileSecretWriter writes secrets as files under Root.
// The produced layout matches a Kubernetes Secret volume mount:
//
//	Root/<name>/<key>
//
// Write is not atomic across multiple keys: if writing any key fails, keys
// written before the failure remain on disk.
type LocalFileSecretWriter struct {
	Root string
}

func (w *LocalFileSecretWriter) Write(ctx context.Context, name string, data map[string]string) error {
	dir := filepath.Join(w.Root, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create secret dir: %w", err)
	}
	for k, v := range data {
		path := filepath.Join(dir, k)
		if existing, err := os.ReadFile(path); err == nil {
			if string(existing) != v {
				return fmt.Errorf("existing secret file %s has conflicting data", path)
			}
			continue
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("read secret file %s: %w", path, err)
		}
		if err := writeSecretFile(path, v); err != nil {
			return fmt.Errorf("write secret file %s: %w", path, err)
		}
	}
	return nil
}

func writeSecretFile(path, value string) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".bootstrap-")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(temp.Name()) }()
	if _, err := temp.WriteString(value); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}
