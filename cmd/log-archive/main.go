package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"orbitjob/internal/logarchive"
	"orbitjob/internal/operator"
)

func main() {
	if err := run(); err != nil {
		slog.Error("execution archive stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	mapping, err := operator.ParseNamespaceTenants(os.Getenv(operator.NamespaceTenantsTableEnv))
	if err != nil {
		return err
	}
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return err
	}
	cfg.Timeout = 10 * time.Second
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return err
	}
	dsn := os.Getenv("RUNTIME_DSN")
	if dsn == "" {
		return fmt.Errorf("RUNTIME_DSN is required")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = db.PingContext(check)
	cancel()
	if err != nil {
		return fmt.Errorf("archive database unavailable")
	}
	l := logarchive.DefaultLimits()
	for name, target := range map[string]*int64{"LOG_TOTAL_BYTES": &l.TotalBytes, "LOG_RUN_BYTES": &l.RunBytes, "LOG_HEAD_BYTES": &l.HeadBytes, "LOG_WORKFLOW_BYTES": &l.WorkflowBytes, "LOG_TENANT_BYTES": &l.TenantBytes, "LOG_DECLARED_BYTES": &l.DeclaredBytes, "LOG_RESERVE_BYTES": &l.ReserveBytes} {
		if raw := os.Getenv(name); raw != "" {
			v, e := strconv.ParseInt(raw, 10, 64)
			if e != nil {
				return fmt.Errorf("invalid %s", name)
			}
			*target = v
		}
	}
	directory := os.Getenv("LOG_DIRECTORY")
	if directory == "" {
		directory = "/data"
	}
	l, err = boundVolume(ctx, kube, l)
	if err != nil {
		return err
	}
	store, err := logarchive.Open(directory, l)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	observer := &logarchive.Observer{Store: store, Repository: logarchive.Repository{DB: db}, Kube: kube, Dynamic: dyn, Namespaces: mapping}
	ca, err := os.ReadFile("/tls/ca.crt")
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return fmt.Errorf("invalid collector CA")
	}
	server := &http.Server{Addr: ":8443", Handler: observer.Handler(), BaseContext: func(net.Listener) context.Context { return ctx }, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 * 1024, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert}}
	health := http.NewServeMux()
	health.Handle("/metrics", promhttp.Handler())
	health.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	health.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		c, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if db.PingContext(c) != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		if _, e := db.ExecContext(c, "SELECT 1 FROM execution_log_archives LIMIT 0"); e != nil {
			http.Error(w, "archive schema unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	metrics := &http.Server{Addr: ":8083", Handler: health, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "orbitjob_archive_storage_bytes", Help: "Allocated archive storage bytes."}, func() float64 { used, _ := store.Usage(); return float64(used) }))
	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "orbitjob_archive_budget_bytes", Help: "Configured archive allocation ceiling."}, func() float64 { _, budget := store.Usage(); return float64(budget) }))
	var workers sync.WaitGroup
	workers.Add(1)
	go func() { defer workers.Done(); observer.Run(ctx) }()
	defer func() { stop(); workers.Wait() }()
	errors := make(chan error, 2)
	go func() { errors <- server.ListenAndServeTLS("/tls/server.crt", "/tls/server.key") }()
	go func() { errors <- metrics.ListenAndServe() }()
	select {
	case <-ctx.Done():
	case err = <-errors:
	}
	stop()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdown)
	_ = metrics.Shutdown(shutdown)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
