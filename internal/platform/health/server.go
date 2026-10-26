package health

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// StartComponentHealthServer runs a minimal HTTP server with /healthz and /readyz.
// Both endpoints return JSON (suitable for automation).
// It blocks until ctx is cancelled, then shuts down gracefully.
func StartComponentHealthServer(ctx context.Context, db *sql.DB, port, component string) {
	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		slog.Error(component+" health server error", "error", err)
		return
	}
	serveComponentHealthServer(ctx, db, listener, component)
}

func serveComponentHealthServer(ctx context.Context, db *sql.DB, listener net.Listener, component string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "component": component})
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		pingCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		w.Header().Set("Content-Type", "application/json")
		if err := db.PingContext(pingCtx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "not ready", "error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready", "component": component})
	})

	srv := &http.Server{Addr: listener.Addr().String(), Handler: mux}
	slog.Info(component+" health server listening", "addr", srv.Addr)

	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
		case <-stopped:
			return
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error(component+" health server shutdown error", "error", err)
		}
	}()

	if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
		slog.Error(component+" health server error", "error", err)
	}
}
