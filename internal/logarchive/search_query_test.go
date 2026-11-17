package logarchive

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestArchiveSearchUsesUnicodeCharacterLimit(t *testing.T) {
	now := time.Now()
	store := openTest(t, &now)
	id := identity(886)
	if err := store.Register(id); err != nil {
		t.Fatal(err)
	}
	handler := (&Observer{Store: store}).Handler()
	for _, sample := range []struct {
		name, search string
		valid        bool
	}{
		{"ascii boundary", strings.Repeat("a", 256), true},
		{"Chinese byte boundary", strings.Repeat("\u4e2d", 86), true},
		{"Chinese character boundary", strings.Repeat("\u4e2d", 256), true},
		{"emoji character boundary", strings.Repeat("\U0001F600", 256), true},
		{"ASCII over boundary", strings.Repeat("a", 257), false},
		{"Chinese over boundary", strings.Repeat("\u4e2d", 257), false},
		{"emoji over boundary", strings.Repeat("\U0001F600", 257), false},
	} {
		t.Run(sample.name, func(t *testing.T) {
			query := Query{Search: sample.search}
			_, err := store.Read(id, query)
			if sample.valid && err != nil {
				t.Fatalf("valid Unicode search: %v", err)
			}
			if !sample.valid && !errors.Is(err, ErrInvalidQuery) {
				t.Fatalf("oversized search error = %v, want ErrInvalidQuery", err)
			}
			body, err := json.Marshal(ReadRequest{Identity: id, Query: query})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/v1/read", bytes.NewReader(body))
			cert := &x509.Certificate{Subject: pkix.Name{CommonName: "orbitjob-log-reader"}}
			request.TLS = &tls.ConnectionState{
				PeerCertificates: []*x509.Certificate{cert},
				VerifiedChains:   [][]*x509.Certificate{{cert}},
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			want := http.StatusOK
			if !sample.valid {
				want = http.StatusBadRequest
			}
			if response.Code != want {
				t.Fatalf("HTTP %d, want %d: %s", response.Code, want, response.Body.String())
			}
			if !sample.valid && response.Header().Get("Retry-After") != "" {
				t.Fatal("invalid query advertised a retryable archive outage")
			}
		})
	}
}
