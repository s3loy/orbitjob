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
	Write(ctx context.Context, name string, data map[string]string) error
}

// LocalFileSecretWriter writes secrets as files under Root.
// The produced layout matches a Kubernetes Secret volume mount:
//
//	Root/<name>/<key>
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
		if err := os.WriteFile(path, []byte(v), 0o600); err != nil {
			return fmt.Errorf("write secret file %s: %w", path, err)
		}
	}
	return nil
}
