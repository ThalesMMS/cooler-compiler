// SPDX-License-Identifier: AGPL-3.0-or-later
package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFormatHandler(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status int
	}{{`{"source":"class Main{main():Object{0};};"}`, 200}, {`{"source":"class Main {"}`, 400}} {
		response := httptest.NewRecorder()
		formatHandler(response, httptest.NewRequest("POST", "/api/format", strings.NewReader(tc.body)))
		if response.Code != tc.status {
			t.Fatal(response.Code, response.Body.String())
		}
	}
}
