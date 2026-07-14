package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"orbitjob/internal/platform/dbconfig"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	return runSetup(ctx, opts, defaultSetupDeps())
}

const (
	defaultStatePath       = ".runtime/database.json"
	defaultRuntimeEnvPath  = ".runtime/database.env"
	defaultBundledEndpoint = "postgres://pg:5432/orbitjob?sslmode=disable"
	defaultBundledOwner    = "orbitjob"
)

func parseOptions(args []string) (setupOptions, error) {
	if len(args) == 0 || args[0] != "setup" {
		return setupOptions{}, fmt.Errorf("usage: configure setup [options]")
	}
	set := flag.NewFlagSet("configure setup", flag.ContinueOnError)
	mode := set.String("mode", string(dbconfig.ModeBundled), "database mode: bundled or external")
	dsnFile := set.String("database-dsn-file", "", "file containing the external bootstrap DSN")
	state := set.String("state", defaultStatePath, "installation database state path")
	runtimeEnv := set.String("runtime-env", defaultRuntimeEnvPath, "generated runtime environment path")
	endpoint := set.String("bundled-endpoint", defaultBundledEndpoint, "bundled PostgreSQL endpoint")
	ownerUser := set.String("bundled-owner-user", defaultBundledOwner, "bundled PostgreSQL owner username")
	ownerPassword := set.String("bundled-owner-password", os.Getenv("PG_PASSWORD"), "bundled PostgreSQL owner password")
	reconfigure := set.Bool("reconfigure", false, "replace existing installation configuration")
	if err := set.Parse(args[1:]); err != nil {
		return setupOptions{}, err
	}
	parsedMode := dbconfig.Mode(*mode)
	if parsedMode != dbconfig.ModeBundled && parsedMode != dbconfig.ModeExternal {
		return setupOptions{}, fmt.Errorf("database mode must be bundled or external")
	}
	return setupOptions{
		Mode: parsedMode, BootstrapDSNFile: *dsnFile, StatePath: *state, RuntimeEnvPath: *runtimeEnv,
		BundledEndpoint: *endpoint, BundledOwner: dbconfig.Credentials{Username: *ownerUser, Password: *ownerPassword}, Reconfigure: *reconfigure,
	}, nil
}
