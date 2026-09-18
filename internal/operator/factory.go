package operator

import (
	"net"
	"net/http"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// apiHTTPClient builds the one HTTP client both the typed and the dynamic
// client share for apiserver traffic, with dead-connection detection turned on.
// Without it a watch riding a connection the network has gone silent on
// receives nothing and logs nothing: the reflector's re-watch hangs on the
// poisoned connection, and every reconcile that still needs an apiserver read
// hangs behind it. TCP keepalive cleans up dead peers at the socket layer;
// HTTP/2 read-idle pings catch the subtler case of a live socket whose streams
// are no longer answered. Both turn a silent stall into a failed request that
// client-go retries on a fresh connection.
func apiHTTPClient(c *rest.Config) (*http.Client, error) {
	tls, err := rest.TLSConfigFor(c)
	if err != nil {
		return nil, err
	}
	base := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:     tls,
		TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout:     90 * time.Second,
		MaxIdleConnsPerHost: 25,
		// Custom TLS material disables net/http's automatic HTTP/2; the
		// apiserver serves watches over HTTP/2, so it must stay on.
		ForceAttemptHTTP2: true,
		HTTP2: &http.HTTP2Config{
			// Ping a connection that has received no frame for this long, and
			// tear it down when no pong answers within PingTimeout.
			SendPingTimeout: 30 * time.Second,
			PingTimeout:     15 * time.Second,
		},
	}
	// transport.New refuses a custom transport while the config still carries
	// TLS options; the TLS material now lives in the transport itself, and the
	// bearer token is layered on top by client-go as before.
	sanitized := *c
	sanitized.TLSClientConfig = rest.TLSClientConfig{}
	sanitized.Transport = base
	return rest.HTTPClientFor(&sanitized)
}

func NewInCluster(cfg Config) (Controller, error) {
	c, err := rest.InClusterConfig()
	if err != nil {
		return Controller{}, err
	}
	// client-go's silent default is five requests per second, which the bursty
	// control plane exceeds on ordinary work: applying twelve hundred
	// declarations is twelve hundred status writes. 50 with a burst of 150
	// keeps the queue draining on reconcile work rather than on the rate
	// limiter, and stays well under what the apiserver serves per client.
	c.QPS = 50
	c.Burst = 150
	httpClient, err := apiHTTPClient(c)
	if err != nil {
		return Controller{}, err
	}
	k, err := kubernetes.NewForConfigAndClient(c, httpClient)
	if err != nil {
		return Controller{}, err
	}
	d, err := dynamic.NewForConfigAndClient(c, httpClient)
	if err != nil {
		return Controller{}, err
	}
	return Controller{Kubernetes: k, Dynamic: d, Config: cfg}, nil
}
