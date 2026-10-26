package dbconfig

import (
	"bytes"
	"crypto/rand"
	"net/url"
	"strings"
	"testing"

	"github.com/lib/pq"
)

func TestDerivedDSNOverridesEveryCredentialCarrier(t *testing.T) {
	endpoint := &url.URL{Scheme: "postgres", Host: "db.invalid", Path: "/orbitjob"}
	account, password := "owner_"+rand.Text(), rand.Text()
	endpoint.User = url.UserPassword(account, password)
	endpoint.RawQuery = url.Values{
		"user": {account}, "password": {password},
		"passfile": {"/owner-pass"}, "sslmode": {"require"},
	}.Encode()
	installation, err := ParseBootstrapDSN(endpoint.String())
	if err != nil {
		t.Fatal(err)
	}
	generated, err := NewBundled(installation.Endpoint.String(), installation.Bootstrap, bytes.NewReader(bytes.Repeat([]byte{1}, 96)))
	if err != nil {
		t.Fatal(err)
	}
	dsn, err := DeriveDSN(generated.Endpoint, generated.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	config, err := pq.NewConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if config.User != generated.Runtime.Username || config.Password != generated.Runtime.Password {
		t.Fatalf("derived DSN did not enforce the intended runtime identity: user=%q", config.User)
	}
	if strings.Contains(dsn, "passfile=") || !strings.Contains(dsn, "sslmode=require") {
		t.Fatal("derived DSN must remove credential overrides and preserve TLS options")
	}
}

func TestRedactDSNQueryPasswords(t *testing.T) {
	redacted := RedactDSN("postgres://owner:userinfo-secret@db/name?password=query-secret&sslpassword=key-secret&sslmode=require")
	for _, secret := range []string{"userinfo-secret", "query-secret", "key-secret"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("redacted DSN contains a credential: %s", secret)
		}
	}
}
