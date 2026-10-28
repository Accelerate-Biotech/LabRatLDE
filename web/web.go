// Package web serves the LabRat UI from assets embedded in the binary. The
// build output lives in web/dist; if it has not been built, the server still
// starts and / explains how to build it.
package web

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var dist embed.FS

const notBuilt = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>LabRat</title></head>
<body><p>LabRat UI is not built. Run <code>make ui</code>, then rebuild the server.</p></body>
</html>
`

const csp = "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'self'"

// Handler serves / (the app shell) and /assets/<file> (hashed build output).
// Every other path is a 404, so the file server never lists directories.
// connectSrc lists extra origins the UI may fetch from: the OIDC issuer, for
// discovery and the token endpoint.
func Handler(connectSrc ...string) http.Handler {
	policy := csp
	if len(connectSrc) > 0 {
		policy += "; connect-src 'self' " + strings.Join(connectSrc, " ")
	}
	site, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	index, err := fs.ReadFile(site, "index.html")
	built := err == nil
	assets := http.FileServerFS(site)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", policy)
		h.Set("X-Content-Type-Options", "nosniff")

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			h.Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		switch {
		case r.URL.Path == "/":
			h.Set("Content-Type", "text/html; charset=utf-8")
			h.Set("Cache-Control", "no-cache")
			if !built {
				_, _ = io.WriteString(w, notBuilt)
				return
			}
			_, _ = w.Write(index)
		case strings.HasPrefix(r.URL.Path, "/assets/") && isFile(site, r.URL.Path[1:]):
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
			assets.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

func isFile(fsys fs.FS, name string) bool {
	info, err := fs.Stat(fsys, name)
	return err == nil && !info.IsDir()
}
