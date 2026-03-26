//go:build integration

package postgres

import (
	"os"
	"testing"

	"orbitjob/internal/platform/postgrestest"
)

func TestMain(m *testing.M) {
	os.Exit(postgrestest.Run(m))
}
