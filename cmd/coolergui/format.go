// SPDX-License-Identifier: AGPL-3.0-or-later
package main

import (
	"github.com/ThalesMMS/cooler-compiler/internal/formatter"
	"net/http"
)

func formatHandler(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Source string `json:"source"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	source, err := formatter.Format(request.Source)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"source": source})
}
