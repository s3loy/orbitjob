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
	_, err = db.ExecContext(ctx, script[start:start+end+len(conflictMarker)])
	return err
}
