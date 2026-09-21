package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
)

// DefaultAPIURL mirrors the admin API's own default port. A kind install
// reached through `make kind-env` sets ORBITJOB_API instead, so the default
// only ever serves a bare local server.
const DefaultAPIURL = "http://localhost:8080"

// connFlags are the connection options every leaf command accepts. Flags win
// over the environment; the environment wins over the defaults.
type connFlags struct {
	apiURL string
	apiKey string
	json   bool
}

// addConnFlags registers the shared flags on one command's FlagSet, so both
// the URL and the key resolve the same way everywhere.
func (c *connFlags) add(fs *flag.FlagSet) {
	fs.StringVar(&c.apiURL, "api-url", "", "admin API base URL (default "+DefaultAPIURL+")")
	fs.StringVar(&c.apiKey, "api-key", "", "bearer key for the admin API")
	fs.BoolVar(&c.json, "json", false, "print the raw API response instead of a table")
}

// apiEnvVars are consulted in order for the base URL. ORBITJOB_API is the name
// `make kind-env` has always exported; ORBITJOB_API_URL is the documented
// canonical spelling and wins over it.
var apiEnvVars = []string{"ORBITJOB_API_URL", "ORBITJOB_API"}

const apiKeyEnvVar = "ORBITJOB_API_KEY"

// resolveURL applies the flag > environment > default precedence for the base
// URL alone. doctor uses it because its key may come from the cluster secret
// instead of the places resolve() checks.
func (c *connFlags) resolveURL() string {
	if c.apiURL != "" {
		return c.apiURL
	}
	for _, name := range apiEnvVars {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return DefaultAPIURL
}

// resolve builds the client configuration. A missing key is an error here for
// every command that talks to the API; doctor resolves the key through the
// cluster secret before calling this with the fallback already in place.
func (c *connFlags) resolve() (*apiClient, error) {
	rawURL := c.resolveURL()
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fatal("invalid admin API URL %q: want something like http://localhost:18080", rawURL)
	}

	key := c.apiKey
	if key == "" {
		key = os.Getenv(apiKeyEnvVar)
	}
	if key == "" {
		return nil, fatal("no admin API key: pass --api-key or set %s (make kind-env prints it)", apiKeyEnvVar)
	}

	return &apiClient{base: strings.TrimRight(rawURL, "/"), key: key, jsonOut: c.json}, nil
}

// keySource describes where doctor found the API key. The value itself never
// travels with it.
type keySource string

const (
	keyFromFlag   keySource = "flag --api-key"
	keyFromEnv    keySource = "environment " + apiKeyEnvVar
	keyFromSecret keySource = "cluster secret bootstrap-api-key"
)

// resolveKey names where a key came from without exposing the key. The
// resolution order is the same one resolve() applies.
func resolveKey(key string) keySource {
	switch {
	case key != "":
		return keyFromFlag
	case os.Getenv(apiKeyEnvVar) != "":
		return keyFromEnv
	default:
		return keyFromSecret
	}
}

// flagSet builds a FlagSet in the house style: continue on error so run() owns
// the exit code, and route usage output to stderr.
func flagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

// parseArgs parses one command's flags and turns a parse failure into the
// command's usage error. -h is not an error: flag already printed the options,
// so the command exits 0 through the sentinel below.
func parseArgs(fs *flag.FlagSet, args []string, usageLine string) (help bool, err error) {
	if perr := fs.Parse(args); perr != nil {
		if errors.Is(perr, flag.ErrHelp) {
			return true, nil
		}
		return false, usageFail(usageLine)
	}
	return false, nil
}

// positiveID parses one numeric resource id from argv. The API's ids are
// int64s; rejecting non-numeric input here keeps the 400 round trip away.
func positiveID(value, kind, usageLine string) (int64, error) {
	var id int64
	if _, err := fmt.Sscanf(value, "%d", &id); err != nil || id < 1 {
		return 0, usageFail(fmt.Sprintf("%s  (%s must be a positive integer, got %q)", usageLine, kind, value))
	}
	return id, nil
}

// stdoutWriter is the seam tests use to capture command output.
var stdoutWriter io.Writer = os.Stdout
