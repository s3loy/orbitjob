package operator

import (
	"net/http"
	"testing"
	"time"

	"k8s.io/client-go/rest"
)

// TestAPIHTTPClientDetectsDeadConnections pins the transport wiring the
// stalled-watch incident demanded: the shared apiserver client must carry TCP
// keepalive and HTTP/2 read-idle pings, so a connection the apiserver stopped
// answering fails and is retried on a fresh connection instead of hanging
// every watch and every reconcile that rides it, silently, until the pod
// restarts. Without the health checks an operator in that state reconciles
// nothing and logs nothing.
func TestAPIHTTPClientDetectsDeadConnections(t *testing.T) {
	cfg := &rest.Config{
		Host:            "https://apiserver:6443",
		BearerToken:     "token",
		TLSClientConfig: rest.TLSClientConfig{CAData: []byte(testCACertPEM)},
	}
	client, err := apiHTTPClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// client-go layers the bearer token over the base transport; unwrap the
	// wrappers to reach the http.Transport the wiring was set on.
	var base *http.Transport
	for rt := client.Transport; rt != nil && base == nil; {
		if direct, ok := rt.(*http.Transport); ok {
			base = direct
			break
		}
		wrapped, ok := rt.(interface{ WrappedRoundTripper() http.RoundTripper })
		if !ok {
			t.Fatalf("transport %T cannot be unwrapped", rt)
		}
		rt = wrapped.WrappedRoundTripper()
	}
	if base == nil {
		t.Fatalf("transport = %T, unwrapped to no *http.Transport", client.Transport)
	}
	if base.DialContext == nil {
		t.Fatal("the transport must dial through a keepalive dialer")
	}
	if base.TLSClientConfig == nil {
		t.Fatal("the transport must carry the config's TLS material")
	}
	if !base.ForceAttemptHTTP2 {
		t.Fatal("custom TLS material disables HTTP/2 unless ForceAttemptHTTP2 is set")
	}
	if base.HTTP2 == nil {
		t.Fatal("the transport must configure HTTP/2 health checks")
	}
	if base.HTTP2.SendPingTimeout != 30*time.Second {
		t.Fatalf("HTTP2.SendPingTimeout = %s, want 30s", base.HTTP2.SendPingTimeout)
	}
	if base.HTTP2.PingTimeout != 15*time.Second {
		t.Fatalf("HTTP2.PingTimeout = %s, want 15s", base.HTTP2.PingTimeout)
	}
}

// TestAPIHTTPClientMovesTLSOutOfTheConfig pins the client-go constraint the
// implementation navigates: transport.New refuses a custom transport while the
// config still carries TLS options, so apiHTTPClient must move the TLS
// material into the transport and strip it from the config copy it hands
// forward -- the call errors otherwise.
func TestAPIHTTPClientMovesTLSOutOfTheConfig(t *testing.T) {
	cfg := &rest.Config{
		Host:            "https://apiserver:6443",
		BearerToken:     "token",
		TLSClientConfig: rest.TLSClientConfig{CAData: []byte(testCACertPEM)},
	}
	if _, err := apiHTTPClient(cfg); err != nil {
		t.Fatalf("a config with TLS material and a custom transport must be accepted: %v", err)
	}
}

// testCACertPEM is a throwaway self-signed certificate: valid enough for the
// config loader to parse, trusted by nothing.
const testCACertPEM = `-----BEGIN CERTIFICATE-----
MIIDATCCAemgAwIBAgIUBBqyNdABwmku2QYbfGY+bxQx8mMwDQYJKoZIhvcNAQEL
BQAwEDEOMAwGA1UEAwwFcHJvYmUwHhcNMjYwOTE4MTYyMjA1WhcNMjYwOTE5MTYy
MjA1WjAQMQ4wDAYDVQQDDAVwcm9iZTCCASIwDQYJKoZIhvcNAQEBBQADggEPADCC
AQoCggEBAKAvxJysCl60rrGkZ1eJxJK9ntwlfFYkRaE5ecCrKlY8rYZ6M3fBR5tO
bJ65SlqQZTdIqw8PbubG0zX7N0ZJRvguoeG3ZWrBXtqTEy3HpM1Ox4vFDrwB3cu+
V/ED0T6BEblhuJI/7xsPrL8pzgnCvenD1qaZmpPM5yhxA0pGB+dYfKPx3SXnunTI
KWh/u8FR+h5pTTX4XMDfIR29HwlUFkWiQpMa/2RRi78zUe51uGE4qgOJgG9TkTwg
F1sRtAQW5JMj7XCThyTXSSXi7K6uDREHIN1cRoZ7+4W/Za8Vh8FFGic8SUv3yrnb
Tg52i7pPNzQdV8hcvxEQv0/ZYzjcVr0CAwEAAaNTMFEwHQYDVR0OBBYEFPJ4fGhZ
+73G3i2HNBhNjB4W2HNFMB8GA1UdIwQYMBaAFPJ4fGhZ+73G3i2HNBhNjB4W2HNF
MA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEBACy69QI6b8XBImTW
xUoq74EmdDMbvTbIhD5tGcLfs7VHCCeg5Nc7K9xCiCpKe51zNYE5d6veK5QLjeCA
npJj1P15J9MRjvAWbJZurZXMCnWLr/b235jupJLg/SGeUrVDea4SICPw43CC51fs
FX0Wf1Aguz1TWf58GWCxKmLpN6kYMYkZHC/wXHJVYbjH9bNdVkU5rqChitjOIhLP
O9jsb79+7R89BpzcI05FotPpe534/w88pAUXseQdUS+4bFYBYSGNeoxTkKaTPt2D
O1p1eKpyUIociBSdsEEWtkcjccBbXtFhJgk5+DRGu8mn1Y+Lrwi9BXuBQyWX2HqY
t75GBCQ=
-----END CERTIFICATE-----
`
