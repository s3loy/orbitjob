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
)

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

	schemaName := schemaNameForPath(filepath.ToSlash(relPath))
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

	schemaName := schemaNameForPath(packageSchema + "/" + testName)
	dsn, err := withSearchPath(baseDSN, schemaName)
	if err != nil {
		return "", "", err
	}

	return dsn, schemaName, nil
}

func schemaNameForPath(path string) string {
	sanitized := sanitizeIdentifier(path)
	if sanitized == "" {
		sanitized = "pkg"
	}

	hash := fnv.New32a()
	_, _ = hash.Write([]byte(path))
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

func dropSchema(ctx context.Context, db *sql.DB, schemaName string) error {
	if schemaName == "public" {
		return nil
	}

	_, err := db.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+quoteIdentifier(schemaName)+` CASCADE`)
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
