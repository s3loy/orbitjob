package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

type runTenants struct {
	Profile      string   `json:"profile"`
	Seed         string   `json:"seed"`
	IDs          []string `json:"tenant_ids"`
	ConfigSHA256 string   `json:"config_sha256"`
}

// resolveRunTenants persists generated identities once. Every command then
// uses that record rather than generating a new mapping on each config load.
func resolveRunTenants(cfg Config, runRoot, runID string, create bool) (Config, error) {
	if err := ValidateCleanRunID(runID); err != nil {
		return Config{}, err
	}
	cfg.RunID = runID
	dir := filepath.Join(runRoot, runID)
	path := filepath.Join(dir, "tenant-ids.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) && create {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Config{}, err
		}
		data, err = json.Marshal(runTenants{Profile: cfg.Profile, Seed: cfg.Seed, IDs: cfg.Tenants, ConfigSHA256: configSHA256(cfg)})
		if err != nil {
			return Config{}, err
		}
		file, openErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if openErr == nil {
			_, writeErr := file.Write(append(data, '\n'))
			closeErr := file.Close()
			if writeErr != nil || closeErr != nil {
				_ = os.Remove(path)
				return Config{}, fmt.Errorf("persist run tenant identities: write=%v close=%v", writeErr, closeErr)
			}
		} else if !os.IsExist(openErr) {
			return Config{}, openErr
		}
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return Config{}, fmt.Errorf("read run tenant identities (generate this run first): %w", err)
	}
	var identity runTenants
	if err := json.Unmarshal(data, &identity); err != nil {
		return Config{}, fmt.Errorf("decode run tenant identities: %w", err)
	}
	if identity.Profile != cfg.Profile || identity.Seed != cfg.Seed || len(identity.IDs) != len(cfg.Tenants) {
		return Config{}, fmt.Errorf("run tenant identities differ from profile, seed or tenant count; use a new run ID")
	}
	if cfg.TenantCount == 0 && !slices.Equal(cfg.Tenants, identity.IDs) {
		return Config{}, fmt.Errorf("explicit tenant identities differ from the generated run; use a new run ID")
	}
	cfg.Tenants = identity.IDs
	if identity.ConfigSHA256 == "" || identity.ConfigSHA256 != configSHA256(cfg) {
		return Config{}, fmt.Errorf("execution config differs from the generated run or its fingerprint is missing; use a new run ID")
	}
	if err := ValidateTenants(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func ensureRunNotStarted(runRoot, runID string) error {
	if err := ValidateCleanRunID(runID); err != nil {
		return err
	}
	_, err := os.Stat(filepath.Join(runRoot, runID, "run-started.json"))
	if err == nil {
		return fmt.Errorf("run %q already started; preserve its evidence and use a new run ID", runID)
	}
	if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func claimRun(runDir, runID string) error {
	file, err := os.OpenFile(filepath.Join(runDir, "run-started.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("claim run %q (a run can only start once): %w", runID, err)
	}
	defer func() { _ = file.Close() }()
	return json.NewEncoder(file).Encode(map[string]string{"run_id": runID})
}
