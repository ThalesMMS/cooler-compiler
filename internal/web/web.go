// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"embed"
	"html"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed static/*
var staticFS embed.FS

var staticContent = must(fs.Sub(staticFS, "static"))

func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}

func Handler(capabilities ...string) http.Handler {
	capability := ""
	if len(capabilities) > 0 {
		capability = capabilities[0]
	}
	files := http.FileServer(http.FS(staticContent))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			if strings.HasSuffix(r.URL.Path, ".mjs") {
				w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			}
			files.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		contents, err := fs.ReadFile(staticContent, "index.html")
		if err != nil {
			http.Error(w, "interface unavailable", http.StatusInternalServerError)
			return
		}
		page := strings.Replace(string(contents), "{{COOLER_CAPABILITY}}", html.EscapeString(capability), 1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodHead {
			_, _ = w.Write([]byte(page))
		}
	})
}
