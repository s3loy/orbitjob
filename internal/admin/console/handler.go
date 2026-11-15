// Package console serves the compiled browser client without a Go build-time
// dependency on Node or generated frontend assets.
package console

import (
	"bytes"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"
)

const (
	// Prefix is the console mount point and the frontend's public asset base.
	Prefix = "/console/"
	// AssetDirectory is populated by the admin image's frontend build stage.
	AssetDirectory = "/console"
	contentPolicy  = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'"
)

// NewHandler loads the entry once. A missing bundle does not stop API-only
// development; image checks must reject a release lacking these assets.
func NewHandler(assets fs.FS) http.Handler {
	index, entryErr := fs.ReadFile(assets, "index.html")
	if entryErr != nil || len(index) == 0 {
		slog.Warn("console bundle unavailable", "component", "console")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", contentPolicy)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-cache")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !strings.HasPrefix(r.URL.Path, Prefix) {
			http.NotFound(w, r)
			return
		}
		if entryErr != nil || len(index) == 0 {
			http.Error(w, "Console bundle unavailable; use the admin image or the frontend development server.", http.StatusServiceUnavailable)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, Prefix)
		if name == "" || name == "index.html" {
			http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
			return
		}
		if !fs.ValidPath(name) {
			http.NotFound(w, r)
			return
		}
		info, err := fs.Stat(assets, name)
		if err == nil && !info.IsDir() {
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			http.StripPrefix(Prefix, http.FileServerFS(assets)).ServeHTTP(w, r)
			return
		}
		// Never turn missing scripts/styles or directory requests into HTML.
		if err != nil && name != "assets" && !strings.HasPrefix(name, "assets/") && path.Ext(name) == "" && strings.Contains(r.Header.Get("Accept"), "text/html") {
			http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
			return
		}
		http.NotFound(w, r)
	})
}
