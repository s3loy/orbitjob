package postgres

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"orbitjob/internal/config"
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

	return nil
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
