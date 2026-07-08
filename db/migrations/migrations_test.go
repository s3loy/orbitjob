package migrations

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestMigrationsFilesUseIdempotentPatterns fails if any *.up.sql file contains
// unguarded CREATE statements.
func TestMigrationsFilesUseIdempotentPatterns(t *testing.T) {
	files, err := filepath.Glob("*.up.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	sort.Strings(files)

	for _, f := range files {
		content, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		s := string(content)

		// Tables and indexes must use IF NOT EXISTS.
		if strings.Contains(s, "CREATE TABLE ") && !strings.Contains(s, "CREATE TABLE IF NOT EXISTS ") {
			t.Errorf("%s: CREATE TABLE without IF NOT EXISTS", f)
		}
		if strings.Contains(s, "CREATE INDEX ") && !strings.Contains(s, "CREATE INDEX IF NOT EXISTS ") {
			t.Errorf("%s: CREATE INDEX without IF NOT EXISTS", f)
		}

		// Triggers and policies must be preceded by DROP ... IF EXISTS.
		if strings.Contains(s, "CREATE TRIGGER ") && !strings.Contains(s, "DROP TRIGGER IF EXISTS ") {
			t.Errorf("%s: CREATE TRIGGER without DROP TRIGGER IF EXISTS", f)
		}
		if strings.Contains(s, "CREATE POLICY ") && !strings.Contains(s, "DROP POLICY IF EXISTS ") {
			t.Errorf("%s: CREATE POLICY without DROP POLICY IF EXISTS", f)
		}
	}
}
