package server

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:webdist
var webDist embed.FS

func spaFilter(next http.Handler) http.Handler {
	sub, err := fs.Sub(webDist, "webdist")
	if err != nil {
		return next
	}
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/media/") || r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if clean != "" && clean != "." {
			if f, err := sub.Open(clean); err == nil {
				stat, statErr := f.Stat()
				f.Close()
				if statErr == nil && !stat.IsDir() {
					files.ServeHTTP(w, r)
					return
				}
			}
		}
		index, err := fs.ReadFile(sub, "index.html")
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(index)
	})
}
