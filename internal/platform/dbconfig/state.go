package dbconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
)

type diskState struct {
	Mode      Mode        `json:"mode"`
	Endpoint  string      `json:"endpoint"`
	Bootstrap Credentials `json:"bootstrap"`
	Migrator  Credentials `json:"migrator"`
	Admin     Credentials `json:"admin"`
	Runtime   Credentials `json:"runtime"`
}

func validateInstallation(installation Installation) error {
	if installation.Mode != ModeBundled && installation.Mode != ModeExternal {
		return fmt.Errorf("database mode is invalid")
	}
	if installation.Endpoint == nil || installation.Endpoint.Scheme == "" || installation.Endpoint.Host == "" {
		return fmt.Errorf("database endpoint is invalid")
	}
	for name, credential := range map[string]Credentials{
		"bootstrap": installation.Bootstrap, "migrator": installation.Migrator,
		"admin": installation.Admin, "runtime": installation.Runtime,
	} {
		if credential.Username == "" || credential.Password == "" {
			return fmt.Errorf("%s database credential is incomplete", name)
		}
	}
	return nil
}

func WriteState(path string, installation Installation) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists; refusing to overwrite", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat database state: %w", err)
	}
	if err := validateInstallation(installation); err != nil {
		return err
	}
	content, err := json.MarshalIndent(diskState{
		Mode: installation.Mode, Endpoint: installation.Endpoint.String(), Bootstrap: installation.Bootstrap,
		Migrator: installation.Migrator, Admin: installation.Admin, Runtime: installation.Runtime,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode database state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create database state directory: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".database-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary database state: %w", err)
	}
	temporaryPath := file.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("set database state permissions: %w", err)
	}
	if _, err := file.Write(append(content, '\n')); err != nil {
		_ = file.Close()
		return fmt.Errorf("write database state: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync database state: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close database state: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("install database state: %w", err)
	}
	return nil
}

func LoadState(path string) (Installation, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Installation{}, fmt.Errorf("read database state: %w", err)
	}
	var state diskState
	if err := json.Unmarshal(content, &state); err != nil {
		return Installation{}, fmt.Errorf("decode database state: %w", err)
	}
	endpoint, err := url.Parse(state.Endpoint)
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
		return Installation{}, fmt.Errorf("decode database state: invalid endpoint")
	}
	installation := Installation{
		Mode: state.Mode, Endpoint: endpoint, Bootstrap: state.Bootstrap,
		Migrator: state.Migrator, Admin: state.Admin, Runtime: state.Runtime,
	}
	if err := validateInstallation(installation); err != nil {
		return Installation{}, fmt.Errorf("decode database state: %w", err)
	}
	return installation, nil
}
