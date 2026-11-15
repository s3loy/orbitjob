package main

import (
	"database/sql"
	adminhttp "orbitjob/internal/admin/http"
	"orbitjob/internal/logarchive"
	"os"
)

func wireArchiveLogs(h *adminhttp.Handler, db *sql.DB) error {
	endpoint := os.Getenv("LOG_ARCHIVE_URL")
	if endpoint == "" {
		return nil
	}
	client, err := logarchive.NewClient(endpoint, "/log-tls/reader.crt", "/log-tls/reader.key", "/log-tls/ca.crt")
	if err != nil {
		return err
	}
	h.SetArchiveBackend(logarchive.Repository{DB: db}, client)
	return nil
}
