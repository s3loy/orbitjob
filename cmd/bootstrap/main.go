// cmd/bootstrap is a standalone CLI to ensure the default tenant and initial
// API key exist. It prints the full key to stdout only when a new key is
// created, making it suitable for scripted first-time setup.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"orbitjob/internal/admin/bootstrap"
	adminpostgres "orbitjob/internal/admin/store/postgres"
)

func main() {
	dsn := resolveDSN(os.Args)
	if dsn == "" {
		log.Fatal("DSN required: pass as first argument or set DATABASE_DSN / ADMIN_DSN")
	}

	db, err := adminpostgres.Open(dsn)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer func() { _ = db.Close() }()

	key := os.Getenv("ADMIN_BOOTSTRAP_API_KEY")
	if key == "" {
		key = bootstrap.DefaultAPIKey
	}
	opts := bootstrap.Options{
		APIKey: key,
	}
	res, err := bootstrap.EnsureDefault(context.Background(), db, opts)
	if err != nil {
		log.Fatalf("bootstrap: %v", err)
	}

	if res.KeyCreated {
		fmt.Fprintln(os.Stderr, "Bootstrap API key created.")
		fmt.Println(key)
		return
	}

	fmt.Fprintln(os.Stderr, "Default API key already exists.")
}

func resolveDSN(args []string) string {
	if len(args) > 1 && args[1] != "" {
		return args[1]
	}
	if d := os.Getenv("DATABASE_DSN"); d != "" {
		return d
	}
	return os.Getenv("ADMIN_DSN")
}
