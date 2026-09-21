package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoadUpMigrations(t *testing.T) {
	fsys := fstest.MapFS{
		"0001_init.up.sql":   {Data: []byte("SELECT 1;")},
		"0001_init.down.sql": {Data: []byte("DROP TABLE x;")},
		"0002_more.up.sql":   {Data: []byte("SELECT 2;")},
		"README.md":          {Data: []byte("ignored")},
	}

	got, err := Load(fsys, ".")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 up migrations, got %d", len(got))
	}
	if got[0].Version != 1 || got[0].Name != "init" || got[1].Version != 2 {
		t.Fatalf("unexpected migrations: %+v", got)
	}
	if got[0].Checksum == "" {
		t.Fatal("expected checksum")
	}
}

func TestLoadStripsTransactionWrapper(t *testing.T) {
	fsys := fstest.MapFS{
		"0001_init.up.sql": {Data: []byte("BEGIN;\nSELECT 1;\nCOMMIT;\nSELECT 2;\n")},
	}
	got, err := Load(fsys, ".")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got[0].SQL != "SELECT 1;\nSELECT 2;" {
		t.Fatalf("unexpected SQL: %q", got[0].SQL)
	}
}

func TestLoadRejectsVersionGap(t *testing.T) {
	fsys := fstest.MapFS{
		"0001_init.up.sql": {Data: []byte("SELECT 1;")},
		"0003_gap.up.sql":  {Data: []byte("SELECT 3;")},
	}
	if _, err := Load(fsys, "."); err == nil {
		t.Fatal("expected version gap error")
	}
}

func TestLoadRejectsDuplicateVersion(t *testing.T) {
	fsys := fstest.MapFS{
		"0001_init.up.sql":  {Data: []byte("SELECT 1;")},
		"0001_other.up.sql": {Data: []byte("SELECT 1;")},
	}
	if _, err := Load(fsys, "."); err == nil {
		t.Fatal("expected duplicate version error")
	}
}

func TestLoadRejectsMalformedUpMigration(t *testing.T) {
	fsys := fstest.MapFS{
		"0001_init.up.sql": {Data: []byte("SELECT 1;")},
		"2_bad.up.sql":     {Data: []byte("SELECT 2;")},
	}

	_, err := Load(fsys, ".")
	if err == nil || !strings.Contains(err.Error(), `invalid migration filename "2_bad.up.sql"`) {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadRejectsEmptyDirectory(t *testing.T) {
	_, err := Load(fstest.MapFS{"README.md": {Data: []byte("ignored")}}, ".")
	if err == nil || !strings.Contains(err.Error(), "no up migrations") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadIgnoresIrreversibleDownMigration(t *testing.T) {
	fsys := fstest.MapFS{
		"0001_init.up.sql":                 {Data: []byte("SELECT 1;")},
		"0002_roles_irreversible.down.sql": {Data: []byte("SELECT 2;")},
		"migration-notes.txt":              {Data: []byte("ignored")},
	}

	got, err := Load(fsys, ".")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one migration, got %d", len(got))
	}
}

func TestLoadComputesChecksumFromSource(t *testing.T) {
	const source = "BEGIN;\nSELECT 1;\nCOMMIT;\n"
	fsys := fstest.MapFS{"0001_init.up.sql": {Data: []byte(source)}}

	got, err := Load(fsys, ".")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	wantSum := sha256.Sum256([]byte(source))
	if got[0].Checksum != hex.EncodeToString(wantSum[:]) {
		t.Fatalf("Checksum = %q, want %q", got[0].Checksum, hex.EncodeToString(wantSum[:]))
	}
	if got[0].SQL != "SELECT 1;" {
		t.Fatalf("SQL = %q", got[0].SQL)
	}
}
