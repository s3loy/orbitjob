package dbconfig

import (
	"fmt"
	"net/url"
	"strings"
)

func ParseBootstrapDSN(raw string) (Installation, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" {
		return Installation{}, fmt.Errorf("parse bootstrap DSN: invalid URL")
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return Installation{}, fmt.Errorf("parse bootstrap DSN: scheme must be postgres or postgresql")
	}
	if parsed.Host == "" {
		return Installation{}, fmt.Errorf("parse bootstrap DSN: host is required")
	}
	if strings.Trim(parsed.Path, "/") == "" {
		return Installation{}, fmt.Errorf("parse bootstrap DSN: database name is required")
	}
	if parsed.User == nil || parsed.User.Username() == "" {
		return Installation{}, fmt.Errorf("parse bootstrap DSN: username is required")
	}
	password, ok := parsed.User.Password()
	if !ok || password == "" {
		return Installation{}, fmt.Errorf("parse bootstrap DSN: password is required")
	}
	endpoint := credentialFreeEndpoint(parsed)
	return Installation{
		Mode:      ModeExternal,
		Endpoint:  endpoint,
		Bootstrap: Credentials{Username: parsed.User.Username(), Password: password},
	}, nil
}

func DeriveDSN(endpoint *url.URL, credential Credentials) (string, error) {
	if endpoint == nil || endpoint.Scheme == "" || endpoint.Host == "" || strings.Trim(endpoint.Path, "/") == "" {
		return "", fmt.Errorf("derive DSN: endpoint is incomplete")
	}
	if credential.Username == "" || credential.Password == "" {
		return "", fmt.Errorf("derive DSN: username and password are required")
	}
	derived := credentialFreeEndpoint(endpoint)
	derived.User = url.UserPassword(credential.Username, credential.Password)
	return derived.String(), nil
}

func RedactDSN(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User == nil {
		return "<invalid-dsn>"
	}
	parsed.User = url.UserPassword(parsed.User.Username(), "***")
	query := parsed.Query()
	for key := range query {
		if strings.HasSuffix(strings.ToLower(key), "password") {
			query.Set(key, "***")
		}
	}
	parsed.RawQuery = query.Encode()
	return strings.Replace(parsed.String(), "%2A%2A%2A", "***", 1)
}

// The driver also accepts credential options in the query string. They cannot
// survive derivation, including passfile/service indirection to another login.
func credentialFreeEndpoint(endpoint *url.URL) *url.URL {
	clean := cloneURL(endpoint)
	clean.User = nil
	query := clean.Query()
	for key := range query {
		switch strings.ToLower(key) {
		case "user", "password", "passfile", "service", "servicefile":
			query.Del(key)
		}
	}
	clean.RawQuery = query.Encode()
	return clean
}

func cloneURL(source *url.URL) *url.URL {
	clone := *source
	return &clone
}
