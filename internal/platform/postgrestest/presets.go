package postgrestest

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
)

// baselineName names the migration the preset seed is replayed from.
const baselineName = "db/migrations/0001_baseline.up.sql"

// SeedPresetPolicies replays the baseline's platform preset seed. The schema
// harness truncates every table after applying the migrations for a clean
// slate, which also wipes the presets -- but the presets are part of the
// schema's own contract: the bootstrap function foreign-keys one of them, and
// admin suites read and bind the others. The statement is extracted from the
// baseline at runtime rather than copied here, so the migration file stays the
// single authority on what a preset is.
func SeedPresetPolicies(ctx context.Context, db *sql.DB) error {
	path, err := findMigrationFile("db", "migrations", "0001_baseline.up.sql")
	if err != nil {
		return fmt.Errorf("locate %s: %w", baselineName, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	script := string(raw)
	const (
		insertMarker   = "INSERT INTO policies"
		conflictMarker = "ON CONFLICT (id) DO NOTHING;"
	)
	start := strings.Index(script, insertMarker)
	if start < 0 {
		return fmt.Errorf("preset seed not found in %s: the baseline moved", baselineName)
	}
	end := strings.Index(script[start:], conflictMarker)
	if end < 0 {
		return fmt.Errorf("preset seed terminator not found in %s: the baseline moved", baselineName)
	}
	if _, err = db.ExecContext(ctx, script[start:start+end+len(conflictMarker)]); err != nil {
		return err
	}
	return seedAccountState(ctx, db)
}

// seedAccountState restores migration-owned identity rows after fixture truncation.
// Account policies remain sourced from their migration, including their exact
// action lists. Older schema fixtures without account tables keep their scope.
func seedAccountState(ctx context.Context, db *sql.DB) error {
	var present bool
	if err := db.QueryRowContext(ctx, "SELECT to_regclass('account_installation') IS NOT NULL").Scan(&present); err != nil || !present {
		return err
	}
	path, err := findMigrationFile("db", "migrations", "0027_personal_accounts.up.sql")
	if err != nil {
		return fmt.Errorf("locate personal-account seed: %w", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	script := string(raw)
	const marker = "INSERT INTO policies("
	count := 0
	for {
		start := strings.Index(script, marker)
		if start < 0 {
			break
		}
		end := strings.Index(script[start:], ");")
		if end < 0 {
			return fmt.Errorf("personal-account preset terminator missing")
		}
		statement := script[start : start+end+1]
		if _, err := db.ExecContext(ctx, statement+" ON CONFLICT (id) DO NOTHING;"); err != nil {
			return fmt.Errorf("seed personal-account preset: %w", err)
		}
		count++
		script = script[start+end+2:]
	}
	if count != 2 {
		return fmt.Errorf("expected two migration-owned personal-account presets, got %d", count)
	}
	_, err = db.ExecContext(ctx, "INSERT INTO account_installation(singleton) VALUES(true) ON CONFLICT DO NOTHING")
	return err
}
