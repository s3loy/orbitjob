package logarchive

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	kyaml "k8s.io/apimachinery/pkg/util/yaml"
)

// This test launches only disposable, resource-limited containers. It is opt-in
// because ordinary unit tests must not access a daemon or a working cluster.
func TestPinnedCollectorContainerRetriesAndRestoresNodeState(t *testing.T) {
	if os.Getenv("ORBITJOB_COLLECTOR_CONTAINER_TEST") != "1" {
		t.Skip("set ORBITJOB_COLLECTOR_CONTAINER_TEST=1 for isolated collector interoperability")
	}
	const image = "cr.fluentbit.io/fluent/fluent-bit:4.2.3@sha256:a5761fa961cb22dd0875883a4d446b1acd99d4935d77358aa9f50ee177e44fe2"
	if output, err := exec.Command("docker", "image", "inspect", image, "--format", "{{.Id}}").CombinedOutput(); err != nil {
		t.Fatalf("pinned image must already be available: %v %s", err, output)
	}
	now := time.Now()
	s := openTest(t, &now)
	id := identity(601)
	pod := bindCollectorSource(t, s, id)
	dir := t.TempDir()
	logs := filepath.Join(dir, "podlogs")
	state := filepath.Join(dir, "state")
	certs := filepath.Join(dir, "tls")
	for _, p := range []string{logs, state, certs} {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	pair, roots := collectorTestCertificates(t, certs)
	var attempts atomic.Int32
	o := &Observer{Store: s}
	handler := o.Handler()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			http.Error(w, "temporary archive outage", http.StatusServiceUnavailable)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots}
	server.StartTLS()
	defer server.Close()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "https://"))
	if err != nil {
		t.Fatal(err)
	}
	// Render the maintained collector configuration, replacing only the isolated
	// destination. The source shape, parser, persistence and retry settings stay.
	rendered, err := exec.Command("helm", "template", "collector-test", "../../charts/orbitjob", "--namespace", "own", "--set", "operator.namespaceTenants=own=tenant", "--show-only", "templates/log-collector.yaml").CombinedOutput()
	if err != nil {
		t.Fatalf("render collector: %v %s", err, rendered)
	}
	decoder := kyaml.NewYAMLOrJSONDecoder(bytes.NewReader(rendered), 4096)
	var config string
	for {
		var doc struct {
			Kind string            `json:"kind"`
			Data map[string]string `json:"data"`
		}
		err = decoder.Decode(&doc)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if doc.Kind == "ConfigMap" {
			config = doc.Data["fluent-bit.conf"]
		}
	}
	if config == "" {
		t.Fatal("rendered collector ConfigMap is absent")
	}
	for pattern, replacement := range map[string]string{`(?m)^\s+Host .*`: "    Host host.docker.internal", `(?m)^\s+Port 8443$`: "    Port " + port, `(?m)^\s+tls.vhost .*`: "    tls.vhost localhost"} {
		config = regexp.MustCompile(pattern).ReplaceAllString(config, replacement)
	}
	configPath := filepath.Join(dir, "fluent-bit.conf")
	if err = os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(logs, "own_pod_"+pod, "task", "0.log")
	if err = os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	line := func(text string) []byte {
		return []byte(time.Now().UTC().Format(time.RFC3339Nano) + " stdout F " + text + "\n")
	}
	if err = os.WriteFile(file, append(line("first durable line"), line("second durable line")...), 0600); err != nil {
		t.Fatal(err)
	}
	start := func() func() {
		name := "orbitjob-collector-test-" + uuid.NewString()
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		args := []string{"run", "--rm", "--pull", "never", "--name", name, "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--memory", "128m", "--cpus", "0.5", "--pids-limit", "64", "--user", strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid()), "--mount", "type=bind,src=" + logs + ",dst=/var/log/pods,readonly", "--mount", "type=bind,src=" + state + ",dst=/state", "--mount", "type=bind,src=" + certs + ",dst=/tls,readonly", "--mount", "type=bind,src=" + configPath + ",dst=/config/fluent-bit.conf,readonly", image, "-c", "/config/fluent-bit.conf"}
		cmd := exec.CommandContext(ctx, "docker", args...)
		var output bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &output
		if err := cmd.Start(); err != nil {
			cancel()
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		stopped := false
		stop := func() {
			if stopped {
				return
			}
			stopped = true
			_ = exec.Command("docker", "rm", "--force", name).Run()
			cancel()
			select {
			case err := <-done:
				var exit *exec.ExitError
				// Removing the owned container interrupts docker run. Other
				// process-wait failures still invalidate cleanup.
				if err != nil && !errors.As(err, &exit) && !errors.Is(err, context.Canceled) {
					t.Errorf("wait for test collector: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Errorf("test collector process did not exit")
			}
		}
		t.Cleanup(stop)
		deadline := time.NewTimer(25 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		expect := 2
		if attempts.Load() > 1 {
			expect = 3
		}
		for {
			select {
			case err := <-done:
				stopped = true
				cancel()
				t.Fatalf("test collector exited: %v %s", err, output.String())
			case <-deadline.C:
				stop()
				t.Fatalf("collector did not deliver %d lines; attempts=%d %s", expect, attempts.Load(), output.String())
			case <-tick.C:
				p, err := s.Read(id, Query{})
				if err == nil && len(p.Records) == expect {
					return stop
				}
			}
		}
	}
	stop := start()
	stop()
	if attempts.Load() < 2 {
		t.Fatal("collector did not retry HTTP503")
	}
	if info, err := os.Stat(filepath.Join(state, "tail.db")); err != nil || info.Size() == 0 {
		t.Fatalf("tail offsets did not persist: %v", err)
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Write(line("third durable line"))
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	stop = start()
	stop()
	page, err := s.Read(id, Query{})
	if err != nil || len(page.Records) != 3 {
		t.Fatalf("collector restart lost/duplicated evidence: records=%d %v", len(page.Records), err)
	}
	t.Logf("pinned collector delivered three distinct lines after HTTP503 and container replacement; image=%s", image)
}

func collectorTestCertificates(t *testing.T, dir string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	newKey := func() *ecdsa.PrivateKey {
		k, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if e != nil {
			t.Fatal(e)
		}
		return k
	}
	caKey := newKey()
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "isolated collector test CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	if err = os.WriteFile(filepath.Join(dir, "ca.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	var server tls.Certificate
	for index, name := range []string{"server", "collector"} {
		key := newKey()
		usage := x509.ExtKeyUsageServerAuth
		cn := "localhost"
		if name == "collector" {
			usage = x509.ExtKeyUsageClientAuth
			cn = "orbitjob-log-collector"
		}
		cert := &x509.Certificate{SerialNumber: big.NewInt(int64(index + 2)), Subject: pkix.Name{CommonName: cn}, DNSNames: []string{"localhost"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}}
		data, e := x509.CreateCertificate(rand.Reader, cert, ca, &key.PublicKey, caKey)
		if e != nil {
			t.Fatal(e)
		}
		priv, e := x509.MarshalECPrivateKey(key)
		if e != nil {
			t.Fatal(e)
		}
		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: data})
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: priv})
		if e = os.WriteFile(filepath.Join(dir, name+".crt"), certPEM, 0600); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(dir, name+".key"), keyPEM, 0600); e != nil {
			t.Fatal(e)
		}
		if name == "server" {
			server, e = tls.X509KeyPair(certPEM, keyPEM)
			if e != nil {
				t.Fatal(e)
			}
		}
	}
	return server, roots
}
