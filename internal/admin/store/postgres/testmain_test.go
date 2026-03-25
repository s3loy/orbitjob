//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"orbitjob/internal/platform/config"
)

func TestMain(m *testing.M) {
	if err := config.LoadDotenv(); err != nil {
		fmt.Fprintf(os.Stderr, "load .env: %v\n", err)
		os.Exit(1)
	}

	if dsn := os.Getenv("TEST_DATABASE_DSN"); dsn != "" {
		if err := applyTestSchema(dsn); err != nil {
			fmt.Fprintf(os.Stderr, "apply test schema: %v\n", err)
			os.Exit(1)
		}
	}

	os.Exit(m.Run())
}

func applyTestSchema(dsn string) error {
	if err := validateTestDatabaseDSN(dsn); err != nil {
		return err
	}

	path, err := findMigrationFile("db", "migrations", "0001_init.sql")
	if err != nil {
		return err
	}

	sqlBytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	db, err := Open(dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, string(sqlBytes)); err != nil {
		return err
	}

	if err := resetTestData(ctx, db); err != nil {
		return err
	}

	return nil
}

func resetTestData(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		TRUNCATE TABLE
			job_change_audits,
			job_instance_attempts,
			job_instances,
			workers,
			jobs
		RESTART IDENTITY CASCADE
	`)
	return err
}

func validateTestDatabaseDSN(dsn string) error {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return err
	}

	dbName := strings.TrimPrefix(parsed.Path, "/")
	if dbName == "" {
		return fmt.Errorf("test database name is required")
	}

	if strings.Contains(strings.ToLower(dbName), "test") {
		return nil
	}

	return fmt.Errorf("TEST_DATABASE_DSN must point to a dedicated test database, got %q", dbName)
}

func findMigrationFile(parts ...string) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	dir := wd
	for {
		path := filepath.Join(append([]string{dir}, parts...)...)
		info, err := os.Stat(path)
		switch {
		case err == nil && !info.IsDir():
			return path, nil
		case err != nil && !os.IsNotExist(err):
			return "", err
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
