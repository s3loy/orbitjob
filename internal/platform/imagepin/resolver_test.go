package imagepin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRegistryResolutionPinsExactBytesAndLimitsTokenScope(t *testing.T) {
	body := `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{},"layers":[]}`
	var base string
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/token" {
			if r.URL.Query().Get("scope") != "repository:platform/report:pull" {
				t.Error("token scope expanded")
			}
			_, _ = fmt.Fprint(w, `{"token":"test-only-pull-token"}`)
			return
		}
		if r.URL.Path != "/v2/platform/report/manifests/release" {
			t.Errorf("unexpected manifest path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+base+`/token",service="test-registry",scope="repository:untrusted:push"`)
			w.WriteHeader(401)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-only-pull-token" {
			t.Error("pull token was not scoped to the manifest request")
		}
		_, _ = fmt.Fprint(w, body)
	}))
	defer server.Close()
	base = server.URL
	u, _ := url.Parse(base)
	resolver := New([]string{u.Host}, nil)
	client := server.Client()
	client.CheckRedirect = resolver.Client.CheckRedirect
	resolver.Client = client
	pinned, err := resolver.Pin(context.Background(), u.Host+"/platform/report:release")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(body))
	want := u.Host + "/platform/report@sha256:" + hex.EncodeToString(sum[:])
	if pinned != want || calls != 3 {
		t.Fatalf("pin=%s want=%s calls=%d", pinned, want, calls)
	}
}

func TestRegistryRejectsDigestMismatchAndAuthenticationRedirect(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", "sha256:"+strings.Repeat("a", 64))
		_, _ = fmt.Fprint(w, `{"schemaVersion":2}`)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	resolver := New([]string{u.Host}, nil)
	resolver.Client = server.Client()
	if _, err := resolver.Pin(context.Background(), u.Host+"/platform/report:release"); err == nil {
		t.Fatal("accepted a digest that differs from the manifest bytes")
	}
	if _, err := resolver.token(context.Background(), "ghcr.io", "platform/report", `Bearer realm="https://169.254.169.254/token"`); err == nil {
		t.Fatal("registry selected an arbitrary token endpoint")
	}
	if _, err := bearerParameters(`Bearer realm="https://ghcr.io/token",realm="https://other/token"`); err == nil {
		t.Fatal("accepted duplicate authentication parameters")
	}
}

func TestPinnedImagesNeedNoNetworkAndResolutionRejectsInternalAddresses(t *testing.T) {
	resolver := New(nil, nil)
	input := "ghcr.io/platform/report@sha256:" + strings.Repeat("a", 64)
	actual, err := resolver.Pin(context.Background(), input)
	if err != nil || actual != input {
		t.Fatalf("explicit digest changed: %s %v", actual, err)
	}
	if _, err := resolver.Pin(context.Background(), "unapproved.example/platform/report:release"); err == nil {
		t.Fatal("resolved an unapproved registry")
	}
	transport := resolver.Client.Transport.(*http.Transport)
	if conn, err := transport.DialContext(context.Background(), "tcp", "127.0.0.1:443"); err == nil {
		if conn != nil {
			_ = conn.Close()
		}
		t.Fatal("resolution connected to loopback")
	} else if !strings.Contains(err.Error(), "outside the approved network") {
		t.Fatal(err)
	}
}
