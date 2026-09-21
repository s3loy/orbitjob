package dbconfig

import (
	"bytes"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("random failed") }

func TestParseBootstrapDSN(t *testing.T) {
	installation, err := ParseBootstrapDSN("postgres://owner:p%40ss@db.example.com:5432/orbitjob?sslmode=require")
	if err != nil {
		t.Fatal(err)
	}
	if installation.Mode != ModeExternal || installation.Bootstrap.Username != "owner" || installation.Bootstrap.Password != "p@ss" {
		t.Fatalf("installation = %#v", installation)
	}
	if installation.Endpoint.User != nil || installation.Endpoint.Host != "db.example.com:5432" {
		t.Fatalf("endpoint = %s", installation.Endpoint)
	}
}

func TestParseBootstrapDSNRejectsInvalidInputs(t *testing.T) {
	for _, raw := range []string{"", "mysql://u:p@db/name", "postgres://u:p@/name", "postgres://db/name", "postgres://u@db/name", "postgres://u:p@db"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ParseBootstrapDSN(raw); err == nil {
				t.Fatalf("ParseBootstrapDSN(%q) succeeded", raw)
			}
		})
	}
}

func TestDeriveDSNEncodesCredentials(t *testing.T) {
	endpoint, _ := url.Parse("postgres://db:5432/orbitjob?sslmode=disable")
	got, err := DeriveDSN(endpoint, Credentials{Username: "orbitjob_runtime", Password: "p@:/?#% word"})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	password, ok := parsed.User.Password()
	if !ok || password != "p@:/?#% word" {
		t.Fatalf("password = %q, %v; dsn = %q", password, ok, got)
	}
}

func TestRedactDSN(t *testing.T) {
	got := RedactDSN("postgres://owner:secret@db:5432/orbitjob?sslmode=disable")
	if strings.Contains(got, "secret") || got != "postgres://owner:***@db:5432/orbitjob?sslmode=disable" {
		t.Fatalf("RedactDSN() = %q", got)
	}
	if got := RedactDSN("not a dsn"); got != "<invalid-dsn>" {
		t.Fatalf("invalid redaction = %q", got)
	}
}

func TestNewBundledUsesFixedRoles(t *testing.T) {
	installation, err := NewBundled("postgres://pg:5432/orbitjob?sslmode=disable", Credentials{Username: "orbitjob", Password: "owner"}, bytes.NewReader(bytes.Repeat([]byte{1}, 96)))
	if err != nil {
		t.Fatal(err)
	}
	if installation.Migrator.Username != "orbitjob_migrator" || installation.Admin.Username != "orbitjob_admin" || installation.Runtime.Username != "orbitjob_runtime" {
		t.Fatalf("roles = %#v", installation)
	}
	if installation.Migrator.Password == "" || installation.Admin.Password == "" || installation.Runtime.Password == "" {
		t.Fatal("generated passwords are empty")
	}
}

func TestNewBundledPropagatesRandomFailure(t *testing.T) {
	if _, err := NewBundled("postgres://pg:5432/orbitjob", Credentials{Username: "owner", Password: "password"}, failingReader{}); err == nil {
		t.Fatal("expected random error")
	}
}

func TestWriteAndLoadState(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".runtime", "database.json")
	installation, err := NewBundled("postgres://pg:5432/orbitjob", Credentials{Username: "owner", Password: "password"}, bytes.NewReader(bytes.Repeat([]byte{2}, 96)))
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteState(path, installation); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	loaded, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Runtime != installation.Runtime || loaded.Endpoint.String() != installation.Endpoint.String() {
		t.Fatalf("loaded = %#v", loaded)
	}
}

func TestDeriveDSNRejectsIncompleteInput(t *testing.T) {
	if _, err := DeriveDSN(nil, Credentials{}); err == nil {
		t.Fatal("expected endpoint error")
	}
	endpoint, _ := url.Parse("postgres://db/orbitjob")
	if _, err := DeriveDSN(endpoint, Credentials{}); err == nil {
		t.Fatal("expected credential error")
	}
}

func TestNewBundledRejectsInvalidEndpoint(t *testing.T) {
	if _, err := NewBundled("not-an-endpoint", Credentials{}, bytes.NewReader(nil)); err == nil {
		t.Fatal("expected endpoint error")
	}
}

func TestLoadStateErrors(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		if _, err := LoadState(filepath.Join(t.TempDir(), "missing")); err == nil {
			t.Fatal("expected read error")
		}
	})
	t.Run("invalid json", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state")
		_ = os.WriteFile(path, []byte("{"), 0o600)
		if _, err := LoadState(path); err == nil {
			t.Fatal("expected decode error")
		}
	})
	t.Run("invalid endpoint", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state")
		_ = os.WriteFile(path, []byte(`{"mode":"bundled","endpoint":"bad"}`), 0o600)
		if _, err := LoadState(path); err == nil {
			t.Fatal("expected endpoint error")
		}
	})
}

func TestWriteStateRequiresEndpoint(t *testing.T) {
	if err := WriteState(filepath.Join(t.TempDir(), "state"), Installation{}); err == nil {
		t.Fatal("expected endpoint error")
	}
}

func TestWriteStateReportsStatError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "parent", "child")
	if err := os.WriteFile(filepath.Dir(path), []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteState(path, Installation{Endpoint: &url.URL{Scheme: "postgres", Host: "db", Path: "/orbitjob"}}); err == nil {
		t.Fatal("expected stat error")
	}
}

func TestWriteStateRefusesOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.json")
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteState(path, Installation{}); err == nil {
		t.Fatal("expected overwrite error")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "keep" {
		t.Fatalf("existing state changed: %q", data)
	}
}
