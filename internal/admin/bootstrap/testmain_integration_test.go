//go:build integration

package bootstrap

import (
	"os"
	"testing"

	"orbitjob/internal/platform/postgrestest"
)

func TestMain(m *testing.M) {
	os.Exit(postgrestest.Run(m))
}
