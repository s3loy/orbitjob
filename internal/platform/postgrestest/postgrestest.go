package postgrestest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"orbitjob/internal/platform/config"
)

const (
	sharedSchemaLockClassID  = 32117
	sharedSchemaLockObjectID = 260326
	advisoryLockWaitTimeout  = 5 * time.Minute
	testSchemaPrefix         = "ojtest_"
	maxIdentifierLength      = 63
)

// Run prepares the integration database before package tests execute.
func Run(m *testing.M) int {
	if err := config.LoadDotenv(); err != nil {
		fmt.Fprintf(os.Stderr, "load .env: %v\n", err)
		return 1
	}

	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		return m.Run()
	}

	packageDSN, _, err := packageDSN(dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scope test dsn: %v\n", err)
		return 1
	}
	if err := os.Setenv("TEST_DATABASE_DSN", packageDSN); err != nil {
		fmt.Fprintf(os.Stderr, "set TEST_DATABASE_DSN: %v\n", err)
		return 1
	}

	if err := withAdvisoryLock(
		packageDSN,
		sharedSchemaLockClassID,
		sharedSchemaLockObjectID,
		func(db *sql.DB) error {
			return applySchemaWithDB(packageDSN, db)
		},
	); err != nil {
		fmt.Fprintf(os.Stderr, "apply test schema: %v\n", err)
		return 1
	}

	return m.Run()
}

// DSN returns the test database DSN or skips the integration test package.
func DSN(t *testing.T) string {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}

	return dsn
}

// Open returns a PostgreSQL handle scoped to the current test name.
func Open(t *testing.T) *sql.DB {
	t.Helper()

	dsn, _, err := testDSN(DSN(t), t.Name())
	if err != nil {
		t.Fatalf("scope test dsn: %v", err)
	}

	db, err := open(dsn)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping test db: %v", err)
	}
	if err := withAdvisoryLock(
		dsn,
		sharedSchemaLockClassID,
		lockObjectID(dsn),
		func(_ *sql.DB) error {
			return applySchemaWithDB(dsn, db)
		},
	); err != nil {
		t.Fatalf("prepare test schema: %v", err)
	}

	return db
}

func open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	return db, nil
}

func resetTestData(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		TRUNCATE TABLE
			job_change_audits,
			job_instance_attempts,
			job_instances,
			workers,
			jobs
		RESTART IDENTITY CASCADE
	`)
	return err
}

func applySchemaWithDB(dsn string, db *sql.DB) error {
	if err := validateDSN(dsn); err != nil {
		return err
	}
	schemaName, err := schemaNameFromDSN(dsn)
	if err != nil {
		return err
	}

	path, err := findMigrationFile("db", "migrations", "0001_init.sql")
	if err != nil {
		return err
	}

	sqlBytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := ensureSchema(ctx, db, schemaName); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, string(sqlBytes)); err != nil {
		return err
	}

	return resetTestData(ctx, db)
}

// withAdvisoryLock serializes shared test-database access across go test processes.
func withAdvisoryLock(dsn string, classID, objectID int, fn func(db *sql.DB) error) (err error) {
	if err := validateDSN(dsn); err != nil {
		return err
	}

	db, err := open(dsn)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := db.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), advisoryLockWaitTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return err
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := conn.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1, $2)`, classID, objectID); err != nil {
		return err
	}

	locked := true
	defer func() {
		if !locked {
			return
		}

		unlockCtx, unlockCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer unlockCancel()
		if _, unlockErr := conn.ExecContext(
			unlockCtx,
			`SELECT pg_advisory_unlock($1, $2)`,
			classID,
			objectID,
		); err == nil && unlockErr != nil {
			err = unlockErr
		}
	}()

	err = fn(db)
	if err != nil {
		return err
	}

	locked = false
	unlockCtx, unlockCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer unlockCancel()
	if _, err := conn.ExecContext(unlockCtx, `SELECT pg_advisory_unlock($1, $2)`, classID, objectID); err != nil {
		return err
	}

	return nil
}

func validateDSN(dsn string) error {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return err
	}

	dbName := strings.TrimPrefix(parsed.Path, "/")
	if dbName == "" {
		return fmt.Errorf("test database name is required")
	}
	if strings.Contains(strings.ToLower(dbName), "test") {
		return nil
	}

	return fmt.Errorf("TEST_DATABASE_DSN must point to a dedicated test database, got %q", dbName)
}

func packageDSN(baseDSN string) (string, string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}

	goModPath, err := findMigrationFile("go.mod")
	if err != nil {
		return "", "", err
	}

	repoRoot := filepath.Dir(goModPath)
	relPath, err := filepath.Rel(repoRoot, wd)
	if err != nil {
		return "", "", err
	}

	if relPath == "." {
		return "", "", errors.New("postgrestest must run from a package directory")
	}

	schemaName := schemaNameForPackagePath(filepath.ToSlash(relPath))
	dsn, err := withSearchPath(baseDSN, schemaName)
	if err != nil {
		return "", "", err
	}

	return dsn, schemaName, nil
}

func testDSN(baseDSN, testName string) (string, string, error) {
	packageSchema, err := schemaNameFromDSN(baseDSN)
	if err != nil {
		return "", "", err
	}

	schemaName := schemaNameForPackagePath(packageSchema + "/" + testName)
	dsn, err := withSearchPath(baseDSN, schemaName)
	if err != nil {
		return "", "", err
	}

	return dsn, schemaName, nil
}

func schemaNameForPackagePath(pkgPath string) string {
	sanitized := sanitizeIdentifier(pkgPath)
	if sanitized == "" {
		sanitized = "pkg"
	}

	hash := fnv.New32a()
	_, _ = hash.Write([]byte(pkgPath))
	hashHex := fmt.Sprintf("%08x", hash.Sum32())

	maxPrefixLength := maxIdentifierLength - len(testSchemaPrefix) - 1 - len(hashHex)
	if maxPrefixLength < 1 {
		maxPrefixLength = 1
	}
	if len(sanitized) > maxPrefixLength {
		sanitized = sanitized[:maxPrefixLength]
		sanitized = strings.TrimRight(sanitized, "_")
	}

	return testSchemaPrefix + sanitized + "_" + hashHex
}

func sanitizeIdentifier(value string) string {
	var b strings.Builder
	b.Grow(len(value))

	lastUnderscore := false
	for _, r := range strings.ToLower(value) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastUnderscore = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if lastUnderscore {
				continue
			}
			b.WriteByte('_')
			lastUnderscore = true
		}
	}

	return strings.Trim(b.String(), "_")
}

func withSearchPath(dsn, schemaName string) (string, error) {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}

	query := parsed.Query()
	query.Set("search_path", schemaName+",public")
	parsed.RawQuery = query.Encode()

	return parsed.String(), nil
}

func schemaNameFromDSN(dsn string) (string, error) {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}

	searchPath := strings.TrimSpace(parsed.Query().Get("search_path"))
	if searchPath == "" {
		return "public", nil
	}

	schemaName, _, _ := strings.Cut(searchPath, ",")
	schemaName = strings.TrimSpace(schemaName)
	if schemaName == "" {
		return "", fmt.Errorf("search_path must include a schema name")
	}

	return schemaName, nil
}

func ensureSchema(ctx context.Context, db *sql.DB, schemaName string) error {
	if schemaName == "public" {
		return nil
	}

	_, err := db.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS `+quoteIdentifier(schemaName))
	return err
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func lockObjectID(value string) int {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(value))
	return int(hash.Sum32() & 0x7fffffff)
}

func findMigrationFile(parts ...string) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	dir := wd
	for {
		path := filepath.Join(append([]string{dir}, parts...)...)
		info, err := os.Stat(path)
		switch {
		case err == nil && !info.IsDir():
			return path, nil
		case err != nil && !os.IsNotExist(err):
			return "", err
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
