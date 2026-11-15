// Command orbitjob-identity issues out-of-band, single-use installation setup or
// recovery capabilities using the installation's narrowly scoped Admin DSN.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"orbitjob/internal/admin/app/account"
	"orbitjob/internal/admin/store/postgres"
	"orbitjob/internal/platform/config"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	action := flag.String("action", "setup", "Capability purpose: setup or recovery")
	username := flag.String("username", "", "Optional account username to bind the capability to")
	origin := flag.String("origin", "", "Console origin for the one-time link; defaults to CONSOLE_PUBLIC_ORIGIN")
	lifetime := flag.Duration("expires-in", 15*time.Minute, "Capability lifetime, at most one hour")
	flag.Parse()
	if *action != "setup" && *action != "recovery" {
		return fmt.Errorf("action must be setup or recovery")
	}
	if err := config.LoadDotenv(); err != nil {
		return fmt.Errorf("load environment: %w", err)
	}
	originValue := *origin
	if originValue == "" {
		originValue = os.Getenv("CONSOLE_PUBLIC_ORIGIN")
	}
	validatedOrigin, err := consoleOrigin(originValue)
	if err != nil {
		return err
	}
	dsn := os.Getenv("ADMIN_DSN")
	if dsn == "" {
		return fmt.Errorf("ADMIN_DSN is required")
	}
	db, err := postgres.Open(dsn)
	if err != nil {
		return fmt.Errorf("open identity database failed")
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var role string
	if err = db.QueryRowContext(ctx, "SELECT current_user").Scan(&role); err != nil {
		return fmt.Errorf("connect identity database failed")
	}
	if role != "orbitjob_admin" {
		return fmt.Errorf("ADMIN_DSN must use the orbitjob_admin role")
	}
	capability, err := account.NewService(postgres.NewAccountRepository(db)).IssueInstallationCapability(ctx, *action, *username, *lifetime)
	if err != nil {
		return err
	}
	output := struct {
		account.Capability
		URL string `json:"url,omitempty"`
	}{Capability: capability}
	if validatedOrigin != "" {
		output.URL = validatedOrigin + "/console/connection#setup=" + capability.Token
	}
	return json.NewEncoder(os.Stdout).Encode(output)
}

// consoleOrigin accepts only an origin. In particular, credentials and capability
// fragments from a pasted link cannot be echoed as a newly issued link.
func consoleOrigin(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	invalid := fmt.Errorf("origin must be HTTPS or loopback HTTP without credentials, path, query or fragment")
	parsed, err := url.Parse(value)
	if err != nil {
		return "", invalid
	}
	if parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" || parsed.Hostname() == "" || parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || strings.ContainsAny(value, "?#") || parsed.Fragment != "" {
		return "", invalid
	}
	if strings.HasSuffix(parsed.Host, ":") {
		return "", invalid
	}
	if port := parsed.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return "", invalid
		}
	}
	if parsed.Scheme != "https" {
		if parsed.Scheme != "http" {
			return "", invalid
		}
		hostname := parsed.Hostname()
		ip := net.ParseIP(hostname)
		if !strings.EqualFold(hostname, "localhost") && (ip == nil || !ip.IsLoopback()) {
			return "", invalid
		}
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}
