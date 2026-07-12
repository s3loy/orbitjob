// Command env-init creates or validates OrbitJob's local .env file.
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

type Values struct {
	PGPassword       string
	MigratorPassword string
	AdminPassword    string
	RuntimePassword  string
	OperatorPassword string
	GrafanaPassword  string
	BootstrapAPIKey  string
}

func main() {
	check := flag.Bool("check", false, "validate an existing environment file")
	path := flag.String("path", ".env", "environment file path")
	flag.Parse()

	var err error
	if *check {
		err = validateEnv(*path)
	} else {
		err = ensureEnv(*path)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func ensureEnv(path string) error {
	if _, err := os.Stat(path); err == nil {
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("set %s permissions: %w", path, err)
		}
		return validateEnv(path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	values, err := generateValues()
	if err != nil {
		return err
	}
	return writeEnv(path, values)
}

func generateValues() (Values, error) {
	pg, err := randomHex(32)
	if err != nil {
		return Values{}, err
	}
	grafana, err := randomHex(32)
	if err != nil {
		return Values{}, err
	}
	key, err := randomHex(24)
	if err != nil {
		return Values{}, err
	}
	passwords := make([]string, 4)
	for i := range passwords {
		passwords[i], err = randomHex(32)
		if err != nil {
			return Values{}, err
		}
	}
	return Values{
		PGPassword: pg, MigratorPassword: passwords[0], AdminPassword: passwords[1],
		RuntimePassword: passwords[2], OperatorPassword: passwords[3],
		GrafanaPassword: grafana, BootstrapAPIKey: "otj_" + key,
	}, nil
}

func randomHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random value: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func writeEnv(path string, values Values) error {
	content := fmt.Sprintf("PG_PASSWORD=%s\nMIGRATOR_PASSWORD=%s\nADMIN_PASSWORD=%s\nRUNTIME_PASSWORD=%s\nOPERATOR_PASSWORD=%s\nGRAFANA_USER=admin\nGRAFANA_PASSWORD=%s\nADMIN_BOOTSTRAP_API_KEY=%s\n", values.PGPassword, values.MigratorPassword, values.AdminPassword, values.RuntimePassword, values.OperatorPassword, values.GrafanaPassword, values.BootstrapAPIKey)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s already exists; refusing to overwrite", path)
		}
		return fmt.Errorf("create %s: %w", path, err)
	}
	if _, err = file.WriteString(content); err != nil {
		_ = file.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err = file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}

func validateEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	for _, key := range []string{"PG_PASSWORD", "MIGRATOR_PASSWORD", "ADMIN_PASSWORD", "RUNTIME_PASSWORD", "OPERATOR_PASSWORD", "GRAFANA_PASSWORD", "ADMIN_BOOTSTRAP_API_KEY"} {
		if values[key] == "" {
			return fmt.Errorf("%s is required in %s", key, path)
		}
	}
	if len(values["ADMIN_BOOTSTRAP_API_KEY"]) < 12 {
		return fmt.Errorf("ADMIN_BOOTSTRAP_API_KEY must be at least 12 characters")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s permissions must be 0600", path)
	}
	return nil
}
