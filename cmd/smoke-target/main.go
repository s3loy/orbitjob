package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
)

const signatureHeader = "X-OrbitJob-Signature"

func main() {
	secret := os.Getenv("SMOKE_WEBHOOK_SECRET")
	if secret == "" {
		slog.Error("SMOKE_WEBHOOK_SECRET is required")
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /echo", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("POST /webhook", webhookHandler(secret))

	addr := ":8081"
	slog.Info("smoke target listening", "addr", addr)
	if err := http.ListenAndServe(addr, requestLogger(mux)); err != nil {
		slog.Error("smoke target stopped", "error", err.Error())
		os.Exit(1)
	}
}

func webhookHandler(secret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 4097))
		if err != nil || len(body) > 4096 {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if r.Header.Get("X-Smoke-Test") != "orbitjob" {
			http.Error(w, "missing smoke header", http.StatusBadRequest)
			return
		}
		want := sign(secret, string(body))
		got := strings.TrimPrefix(r.Header.Get(signatureHeader), "sha256=")
		gotBytes, err := hex.DecodeString(got)
		if err != nil || !hmac.Equal(gotBytes, want) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func sign(secret, body string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(body))
	return mac.Sum(nil)
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Info("smoke target request", "method", r.Method, "path", r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
