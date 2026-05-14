//go:build etcd

package discovery

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"go.etcd.io/etcd/client/v3"
)

var cleanupClient *clientv3.Client

func TestMain(m *testing.M) {
	// Silence slog / log noise so benchmark output is clean.
	// etcd-client warn logs are redirected via the bench script.
	log.SetOutput(io.Discard)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	endpoints := getEndpoints()

	var err error
	benchReg, err = NewEtcd(endpoints)
	if err != nil {
		fmt.Fprintf(os.Stderr, "benchmark: connect to etcd: %v\n", err)
		os.Exit(1)
	}

	cleanupClient, err = clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "benchmark: create cleanup client: %v\n", err)
		os.Exit(1)
	}

	cleanupKeys = func(prefix string) {
		if cleanupClient == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = cleanupClient.Delete(ctx, "/orbitjob/services/"+prefix, clientv3.WithPrefix())
	}

	code := m.Run()

	// Let background goroutines finish before closing connections.
	time.Sleep(200 * time.Millisecond)

	_ = benchReg.Close()
	_ = cleanupClient.Close()
	os.Exit(code)
}

func getEndpoints() []string {
	if v := os.Getenv("ETCD_ENDPOINTS"); v != "" {
		return strings.Split(v, ",")
	}
	return []string{"localhost:2379"}
}
