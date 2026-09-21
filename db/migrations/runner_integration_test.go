//go:build integration

package migrations

import (
	"context"
	"os"
	"testing"
	"time"

	platformmigrate "orbitjob/internal/platform/migrate"
)

func TestMigrationsApplyAndThenNoop(t *testing.T) {
	applyAllMigrations(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	migrations, err := platformmigrate.Load(os.DirFS("."), ".")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	migrator := openMigrationDB(t, testRoleDSN(t, "orbitjob_migrator"))
	result, err := platformmigrate.Execute(ctx, migrator, migrations, platformmigrate.Options{})
	if err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}
	expectedVersion := migrations[len(migrations)-1].Version
	if result.CurrentVersion != expectedVersion || !result.Noop || len(result.Applied) != 0 {
		t.Fatalf("second result = %#v", result)
	}
}
