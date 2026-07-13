package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/lib/pq"

	platformmigrate "orbitjob/internal/platform/migrate"
)

type migrationMode string

const (
	modeOwnerInit migrationMode = "owner-init"
	modeMigrate   migrationMode = "migrate"
)

type config struct {
	mode          migrationMode
	dsn           string
	migrationsDir string
	rolePasswords map[string]string
}

func loadConfig() (config, error) {
	cfg := config{
		mode:          migrationMode(os.Getenv("MIGRATION_MODE")),
		migrationsDir: os.Getenv("MIGRATIONS_DIR"),
		rolePasswords: map[string]string{
			"orbitjob_migrator": os.Getenv("MIGRATOR_PASSWORD"),
			"orbitjob_admin":    os.Getenv("ADMIN_PASSWORD"),
			"orbitjob_runtime":  os.Getenv("RUNTIME_PASSWORD"),
			"orbitjob_operator": os.Getenv("OPERATOR_PASSWORD"),
		},
	}
	if cfg.migrationsDir == "" {
		cfg.migrationsDir = "db/migrations"
	}

	switch cfg.mode {
	case modeOwnerInit:
		cfg.dsn = os.Getenv("BOOTSTRAP_OWNER_DSN")
		if cfg.dsn == "" {
			return config{}, fmt.Errorf("BOOTSTRAP_OWNER_DSN is required in owner-init mode")
		}
		for role, password := range cfg.rolePasswords {
			if password == "" {
				return config{}, fmt.Errorf("password for %s is required", role)
			}
		}
	case modeMigrate:
		if _, exists := os.LookupEnv("MIGRATIONS_BASELINE_VERSION"); exists {
			return config{}, fmt.Errorf("MIGRATIONS_BASELINE_VERSION has been removed; recreate pre-v0.2.0 development databases instead")
		}
		cfg.dsn = os.Getenv("MIGRATOR_DSN")
		if cfg.dsn == "" {
			return config{}, fmt.Errorf("MIGRATOR_DSN is required in migrate mode")
		}
	default:
		return config{}, fmt.Errorf("MIGRATION_MODE must be owner-init or migrate")
	}
	return cfg, nil
}

func run(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	db, err := sql.Open("postgres", cfg.dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = db.Close() }()

	switch cfg.mode {
	case modeOwnerInit:
		if err := platformmigrate.EnsureRoles(ctx, db); err != nil {
			return err
		}
		return platformmigrate.EnsureRolePasswords(ctx, db, cfg.rolePasswords)
	case modeMigrate:
		migrations, err := platformmigrate.Load(os.DirFS(cfg.migrationsDir), ".")
		if err != nil {
			return err
		}
		result, err := platformmigrate.Execute(ctx, db, migrations, platformmigrate.Options{Logger: log.Default()})
		if err != nil {
			return err
		}
		if result.Noop {
			log.Printf("schema is current at version %04d", result.CurrentVersion)
		} else {
			log.Printf("applied %d migration versions; current version %04d", len(result.Applied), result.CurrentVersion)
		}
		return nil
	default:
		return fmt.Errorf("unsupported migration mode %q", cfg.mode)
	}
}

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
