// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"net/http"
	"time"

	"github.com/ThalesMMS/cooler-compiler/internal/course"
)

// courseHandler serves the opt-in course-assignment artifacts (reference
// dumps, MIPS, embedded simulator runs). It never invokes external tools.
func courseHandler(w http.ResponseWriter, r *http.Request) {
	var request course.Request
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	response, err := course.BuildContext(ctx, request)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, response)
}
