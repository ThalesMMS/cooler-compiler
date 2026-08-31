// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"embed"
	"io/fs"
	"net/http"
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

func Handler() http.Handler {
	return http.FileServer(http.FS(staticContent))
}
