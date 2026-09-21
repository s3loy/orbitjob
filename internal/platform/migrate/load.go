package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var upNamePattern = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.up\.sql$`)

type Migration struct {
	Version  int
	Name     string
	SQL      string
	Checksum string
}

func Load(fsys fs.FS, dir string) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}

	migrations := make([]Migration, 0)
	seen := make(map[int]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".down.sql") {
			continue
		}
		match := upNamePattern.FindStringSubmatch(name)
		if match == nil {
			if strings.HasSuffix(name, ".up.sql") {
				return nil, fmt.Errorf("invalid migration filename %q", name)
			}
			continue
		}
		version, err := strconv.Atoi(match[1])
		if err != nil {
			return nil, fmt.Errorf("parse migration version %q: %w", entry.Name(), err)
		}
		if previous, ok := seen[version]; ok {
			return nil, fmt.Errorf("duplicate migration version %04d: %s and %s", version, previous, entry.Name())
		}
		data, err := fs.ReadFile(fsys, path.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		sum := sha256.Sum256(data)
		migrations = append(migrations, Migration{Version: version, Name: match[2], SQL: stripTransactionWrapper(string(data)), Checksum: hex.EncodeToString(sum[:])})
		seen[version] = entry.Name()
	}

	if len(migrations) == 0 {
		return nil, fmt.Errorf("no up migrations found in %q", dir)
	}

	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Version < migrations[j].Version })
	for i, migration := range migrations {
		expected := i + 1
		if migration.Version != expected {
			return nil, fmt.Errorf("migration version gap: expected %04d, got %04d", expected, migration.Version)
		}
	}
	return migrations, nil
}

func stripTransactionWrapper(sql string) string {
	lines := strings.Split(sql, "\n")
	filtered := lines[:0]
	for _, line := range lines {
		switch strings.TrimSpace(line) {
		case "BEGIN;", "COMMIT;":
			continue
		default:
			filtered = append(filtered, line)
		}
	}
	return strings.TrimSpace(strings.Join(filtered, "\n"))
}
