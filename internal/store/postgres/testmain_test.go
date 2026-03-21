package postgres

import (
	"fmt"
	"os"
	"testing"

	"orbitjob/internal/config"
)

func TestMain(m *testing.M) {
	if err := config.LoadDotenv(); err != nil {
		fmt.Fprintf(os.Stderr, "load .env: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}
