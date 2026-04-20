package main

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func spaHandler(distDir string) http.Handler {
	fs := http.FileServer(http.Dir(distDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		upath := path.Clean("/" + r.URL.Path)
		if strings.Contains(upath, "..") {
			http.NotFound(w, r)
			return
		}
		rel := strings.TrimPrefix(upath, "/")
		full := filepath.Join(distDir, filepath.FromSlash(rel))
		fi, err := os.Stat(full)
		if err == nil && !fi.IsDir() {
			fs.ServeHTTP(w, r)
			return
		}
		if err == nil && fi.IsDir() {
			index := filepath.Join(full, "index.html")
			if _, err := os.Stat(index); err == nil {
				http.ServeFile(w, r, index)
				return
			}
		}
		http.ServeFile(w, r, filepath.Join(distDir, "index.html"))
	})
}
