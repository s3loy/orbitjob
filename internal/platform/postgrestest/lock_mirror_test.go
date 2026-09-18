package postgrestest

import (
	"testing"

	"orbitjob/internal/platform/migrate"
)

// placeExtension and migrate.EnsureRoles create the same extension on the
// same database from different go test processes. Each side takes the same
// two-key advisory lock; postgrestest imports migrate, so the mirror lives in
// migrate and this test fails if either copy drifts — a divergence would put
// two CREATE EXTENSION IF NOT EXISTS calls back into the race this lock
// exists to prevent.
func TestMigrateLockMirrorsTheHarnessExtensionLock(t *testing.T) {
	if migrate.RoleSetupLockClassID != sharedSchemaLockClassID {
		t.Fatalf("migrate.RoleSetupLockClassID = %d, want the harness class id %d",
			migrate.RoleSetupLockClassID, sharedSchemaLockClassID)
	}
	if migrate.RoleSetupLockObjectID != extensionLockObjectID {
		t.Fatalf("migrate.RoleSetupLockObjectID = %d, want the harness extension object id %d",
			migrate.RoleSetupLockObjectID, extensionLockObjectID)
	}
}
