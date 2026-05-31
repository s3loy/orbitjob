package pkg_test

import (
	"testing"

	// Verify all pkg/ packages compile and can be imported.
	_ "orbitjob/pkg/admin/http"
	_ "orbitjob/pkg/admin/instance/command"
	_ "orbitjob/pkg/admin/instance/query"
	_ "orbitjob/pkg/admin/job/command"
	_ "orbitjob/pkg/admin/job/query"
	_ "orbitjob/pkg/admin/store/postgres"
	_ "orbitjob/pkg/dispatch"
	_ "orbitjob/pkg/domain"
	_ "orbitjob/pkg/domain/instance"
	_ "orbitjob/pkg/domain/job"
	_ "orbitjob/pkg/domain/resource"
	_ "orbitjob/pkg/domain/validation"
	_ "orbitjob/pkg/domain/worker"
	_ "orbitjob/pkg/execute"
	_ "orbitjob/pkg/execute/handler"
	_ "orbitjob/pkg/platform/config"
	_ "orbitjob/pkg/platform/health"
	_ "orbitjob/pkg/platform/logger"
	_ "orbitjob/pkg/schedule"
	_ "orbitjob/pkg/store/postgres"
)

// TestPackagesCompile verifies that all pkg/ packages compile successfully.
// This test has no assertions — a successful compilation is the passing condition.
func TestPackagesCompile(t *testing.T) {
	// All imports above are blank imports. If any pkg/ package fails to compile,
	// this test file will not compile and the test runner will report the error.
}
