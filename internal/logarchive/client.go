package logarchive

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type Client struct {
	URL  string
	HTTP *http.Client
}

func NewClient(endpoint, certificate, key, ca string) (*Client, error) {
	u, e := url.Parse(endpoint)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("archive endpoint must be an HTTPS origin")
	}
	pair, e := tls.LoadX509KeyPair(certificate, key)
	if e != nil {
		return nil, fmt.Errorf("archive client certificate: %w", e)
	}
	data, e := os.ReadFile(ca)
	if e != nil {
		return nil, e
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("invalid archive CA")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{pair}}, MaxIdleConns: 8, IdleConnTimeout: 30 * time.Second}
	return &Client{URL: strings.TrimRight(endpoint, "/"), HTTP: &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *Client) Read(ctx context.Context, id Identity, q Query) (Page, error) {
	body, e := json.Marshal(ReadRequest{Identity: id, Query: q})
	if e != nil {
		return Page{}, e
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+"/v1/read", bytes.NewReader(body))
	if e != nil {
		return Page{}, e
	}
	req.Header.Set("Content-Type", "application/json")
	res, e := c.HTTP.Do(req)
	if e != nil {
		return Page{}, fmt.Errorf("archive service unavailable")
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != 200 {
		return Page{}, fmt.Errorf("archive service returned %d", res.StatusCode)
	}
	var p Page
	e = json.NewDecoder(io.LimitReader(res.Body, 2*MiB)).Decode(&p)
	return p, e
}
