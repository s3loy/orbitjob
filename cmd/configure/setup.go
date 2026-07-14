package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/lib/pq"

	"orbitjob/internal/platform/dbconfig"
	platformmigrate "orbitjob/internal/platform/migrate"
)

type setupOptions struct {
	Mode             dbconfig.Mode
	BootstrapDSNFile string
	StatePath        string
	RuntimeEnvPath   string
	BundledEndpoint  string
	BundledOwner     dbconfig.Credentials
	Reconfigure      bool
}

type setupDeps struct {
	OpenDB func(string) (*sql.DB, error)
	Random io.Reader
	Out    io.Writer
}

func defaultSetupDeps() setupDeps {
	return setupDeps{OpenDB: func(dsn string) (*sql.DB, error) { return sql.Open("postgres", dsn) }, Random: rand.Reader, Out: os.Stdout}
}

const databasePingTimeout = 5 * time.Second

func runSetup(ctx context.Context, opts setupOptions, deps setupDeps) error {
	if deps.OpenDB == nil || deps.Random == nil {
		return fmt.Errorf("setup dependencies are incomplete")
	}
	if deps.Out == nil {
		deps.Out = io.Discard
	}
	release, err := acquireSetupLock(opts.StatePath)
	if err != nil {
		return err
	}
	defer release()
	if opts.Mode == dbconfig.ModeExternal && opts.Reconfigure {
		return fmt.Errorf("external database reconfiguration is not supported; use the credential rotation workflow")
	}
	if _, err := os.Stat(opts.StatePath); err == nil && !opts.Reconfigure {
		installation, err := dbconfig.LoadState(opts.StatePath)
		if err != nil {
			return err
		}
		bootstrapDSN, err := dbconfig.DeriveDSN(installation.Endpoint, installation.Bootstrap)
		if err != nil {
			return err
		}
		if err := replaceRuntimeEnv(opts.RuntimeEnvPath, installation, bootstrapDSN); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(deps.Out, "Existing database configuration is valid. Runtime configuration refreshed.")
		return nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect database state: %w", err)
	}

	installation, bootstrapDSN, err := buildInstallation(opts, deps.Random)
	if err != nil {
		return err
	}
	if opts.Mode == dbconfig.ModeExternal {
		_, _ = fmt.Fprintf(deps.Out, "Checking PostgreSQL at %s...\n", dbconfig.RedactDSN(bootstrapDSN))
		if err := initializeExternalDatabase(ctx, deps, installation, bootstrapDSN); err != nil {
			return err
		}
	}
	if opts.Reconfigure {
		if err := os.Remove(opts.StatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove existing database state: %w", err)
		}
		if err := os.Remove(opts.RuntimeEnvPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove existing runtime database environment: %w", err)
		}
	}
	if err := dbconfig.WriteState(opts.StatePath, installation); err != nil {
		return err
	}
	if err := writeRuntimeEnv(opts.RuntimeEnvPath, installation, bootstrapDSN); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(deps.Out, "Database configuration completed.")
	return nil
}

func acquireSetupLock(statePath string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		return nil, fmt.Errorf("create database state directory: %w", err)
	}
	lockPath := statePath + ".lock"
	file, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("database setup is already running")
		}
		return nil, fmt.Errorf("acquire database setup lock: %w", err)
	}
	_ = file.Close()
	return func() { _ = os.Remove(lockPath) }, nil
}

func pingDatabase(ctx context.Context, db *sql.DB) error {
	pingCtx, cancel := context.WithTimeout(ctx, databasePingTimeout)
	defer cancel()
	return db.PingContext(pingCtx)
}

func initializeExternalDatabase(ctx context.Context, deps setupDeps, installation dbconfig.Installation, bootstrapDSN string) error {
	bootstrapDB, err := deps.OpenDB(bootstrapDSN)
	if err != nil {
		return fmt.Errorf("open bootstrap database: %w", err)
	}
	defer func() { _ = bootstrapDB.Close() }()
	if err := pingDatabase(ctx, bootstrapDB); err != nil {
		return fmt.Errorf("check bootstrap database: %w", err)
	}
	if err := platformmigrate.EnsureRoles(ctx, bootstrapDB); err != nil {
		return err
	}
	passwords := map[string]string{
		platformmigrate.RoleMigrator: installation.Migrator.Password,
		platformmigrate.RoleAdmin:    installation.Admin.Password,
		platformmigrate.RoleRuntime:  installation.Runtime.Password,
	}
	if err := platformmigrate.EnsureRolePasswords(ctx, bootstrapDB, passwords); err != nil {
		return err
	}
	for _, credential := range []dbconfig.Credentials{installation.Migrator, installation.Admin, installation.Runtime} {
		dsn, err := dbconfig.DeriveDSN(installation.Endpoint, credential)
		if err != nil {
			return err
		}
		db, err := deps.OpenDB(dsn)
		if err != nil {
			return fmt.Errorf("open database as %s: %w", credential.Username, err)
		}
		if err := pingDatabase(ctx, db); err != nil {
			_ = db.Close()
			return fmt.Errorf("check database as %s: %w", credential.Username, err)
		}
		_ = db.Close()
	}
	return nil
}

func validatePrivateFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect bootstrap DSN file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("bootstrap DSN path must be a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("bootstrap DSN file permissions must be 0600")
	}
	return nil
}

func buildInstallation(opts setupOptions, random io.Reader) (dbconfig.Installation, string, error) {
	if opts.Mode == dbconfig.ModeExternal {
		if opts.BootstrapDSNFile == "" {
			return dbconfig.Installation{}, "", fmt.Errorf("--database-dsn-file is required in external mode")
		}
		if err := validatePrivateFile(opts.BootstrapDSNFile); err != nil {
			return dbconfig.Installation{}, "", err
		}
		content, err := os.ReadFile(opts.BootstrapDSNFile)
		if err != nil {
			return dbconfig.Installation{}, "", fmt.Errorf("read bootstrap DSN file: %w", err)
		}
		raw := strings.TrimSpace(string(content))
		installation, err := dbconfig.ParseBootstrapDSN(raw)
		if err != nil {
			return dbconfig.Installation{}, "", err
		}
		generated, err := dbconfig.NewBundled(installation.Endpoint.String(), installation.Bootstrap, random)
		if err != nil {
			return dbconfig.Installation{}, "", err
		}
		generated.Mode = dbconfig.ModeExternal
		return generated, raw, nil
	}
	if opts.BundledOwner.Username == "" || opts.BundledOwner.Password == "" {
		return dbconfig.Installation{}, "", fmt.Errorf("bundled owner username and PG_PASSWORD are required")
	}
	installation, err := dbconfig.NewBundled(opts.BundledEndpoint, opts.BundledOwner, random)
	if err != nil {
		return dbconfig.Installation{}, "", err
	}
	bootstrapDSN, err := dbconfig.DeriveDSN(installation.Endpoint, installation.Bootstrap)
	return installation, bootstrapDSN, err
}

func replaceRuntimeEnv(path string, installation dbconfig.Installation, bootstrapDSN string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale runtime database environment: %w", err)
	}
	return writeRuntimeEnv(path, installation, bootstrapDSN)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func writeRuntimeEnv(path string, installation dbconfig.Installation, bootstrapDSN string) error {
	migrator, err := dbconfig.DeriveDSN(installation.Endpoint, installation.Migrator)
	if err != nil {
		return err
	}
	admin, err := dbconfig.DeriveDSN(installation.Endpoint, installation.Admin)
	if err != nil {
		return err
	}
	runtimeDSN, err := dbconfig.DeriveDSN(installation.Endpoint, installation.Runtime)
	if err != nil {
		return err
	}
	content := fmt.Sprintf("# Generated by: make setup\n# Do not edit this file manually.\nBOOTSTRAP_OWNER_DSN=%s\nMIGRATOR_DSN=%s\nADMIN_DSN=%s\nRUNTIME_DSN=%s\nMIGRATOR_PASSWORD=%s\nADMIN_PASSWORD=%s\nRUNTIME_PASSWORD=%s\n", shellQuote(bootstrapDSN), shellQuote(migrator), shellQuote(admin), shellQuote(runtimeDSN), shellQuote(installation.Migrator.Password), shellQuote(installation.Admin.Password), shellQuote(installation.Runtime.Password))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create runtime config directory: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".database-env-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary runtime database environment: %w", err)
	}
	temporaryPath := file.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("set runtime database environment permissions: %w", err)
	}
	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		return fmt.Errorf("write runtime database environment: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync runtime database environment: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close runtime database environment: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("install runtime database environment: %w", err)
	}
	return nil
}
