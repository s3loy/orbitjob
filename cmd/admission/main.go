package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	adminpostgres "orbitjob/internal/admin/store/postgres"
	"orbitjob/internal/admission"
	"orbitjob/internal/operator"
)

func main() {
	if err := run(); err != nil {
		slog.Error("workload admission stopped", "error", err)
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
	namespace := os.Getenv("POD_NAMESPACE")
	if namespace == "" {
		return fmt.Errorf("POD_NAMESPACE is required")
	}
	dsn := os.Getenv("RUNTIME_DSN")
	if dsn == "" {
		return fmt.Errorf("RUNTIME_DSN is required")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("open admission database")
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(5 * time.Minute)
	native := make(map[string]bool)
	for _, name := range strings.Split(os.Getenv("ADMISSION_NATIVE_WRITERS"), ",") {
		if name = strings.TrimSpace(name); name != "" {
			native[name] = true
		}
	}
	handler := admission.Server{Namespaces: mapping, DefinitionWriter: "system:serviceaccount:" + namespace + ":orbitjob-admin-api", NativeWriters: native, Definitions: adminpostgres.DefinitionOperations{DB: db}}
	server := &http.Server{Addr: ":8443", Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13}}
	health := http.NewServeMux()
	health.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	health.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		check, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if db.PingContext(check) != nil {
			http.Error(w, "admission database unavailable", http.StatusServiceUnavailable)
			return
		}
		if _, err := db.ExecContext(check, `SELECT admitted_uid,admission_nonce_hash FROM definition_operations LIMIT 0`); err != nil {
			http.Error(w, "admission schema unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	probe := &http.Server{Addr: ":8084", Handler: health, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	errorsCh := make(chan error, 2)
	go func() { errorsCh <- server.ListenAndServeTLS("/tls/server.crt", "/tls/server.key") }()
	go func() { errorsCh <- probe.ListenAndServe() }()
	select {
	case <-ctx.Done():
	case err = <-errorsCh:
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdown)
	_ = probe.Shutdown(shutdown)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
