// Package webui serves the React UI built into dist/ by `make ui-build`, embedded in
// the hostisan binary so the panel ships as one file.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// all: keeps dist/.keep, so the package builds before the UI has been built once.
//
//go:embed all:dist
var embedded embed.FS

// Handler serves the embedded UI.
func Handler() http.Handler {
	dist, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err) // the embed pattern guarantees dist exists
	}
	return NewHandler(dist)
}

const notBuilt = `<!doctype html><meta charset="utf-8"><title>Hostisan</title>
<p>The Hostisan UI isn't built into this binary. Run <code>make ui-build</code>, then rebuild.</p>`

// NewHandler serves files from fsys. Paths that aren't files get index.html, so the
// app's own routes (/web, /dns/…) load the single-page app; /api/ is never the app.
func NewHandler(fsys fs.FS) http.Handler {
	files := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if strings.HasPrefix(p, "api/") || p == "api" {
			http.NotFound(w, r)
			return
		}
		if p != "" && p != "index.html" {
			if info, err := fs.Stat(fsys, p); err == nil && !info.IsDir() {
				// Vite puts a content hash in asset names, so they never change.
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
			if strings.HasPrefix(p, "assets/") {
				http.NotFound(w, r)
				return
			}
		}
		index, err := fs.ReadFile(fsys, "index.html")
		if err != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(notBuilt))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	})
}
