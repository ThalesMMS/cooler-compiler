// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGUIWarningSeverityAndSelection(t *testing.T) {
	server := httptest.NewServer(testHandler())
	defer server.Close()
	for _, rules := range []string{"all", "none"} {
		response := postJSON(t, server.URL+"/api/compile", map[string]any{"source": `class Main {main():Object {let unused:Int in 0};};`, "warningRules": rules})
		var result compiler.Result
		err := json.NewDecoder(response.Body).Decode(&result)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if rules == "none" {
			if len(result.Diagnostics) != 0 {
				t.Fatal("GUI suppression ignored")
			}
			continue
		}
		if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "COL3180" || result.Diagnostics[0].Severity != compiler.SeverityWarning || result.Diagnostics[0].Span.EndOffset-result.Diagnostics[0].Span.StartOffset != 6 {
			t.Fatalf("GUI warning: %+v", result.Diagnostics)
		}
	}
}

func TestGUIRejectsInvalidWarningRules(t *testing.T) {
	server := httptest.NewServer(testHandler())
	defer server.Close()
	for _, path := range []string{"/api/compile", "/api/pipeline/start", "/api/vm/start"} {
		response := postJSON(t, server.URL+path, map[string]any{"source": apiProgram, "warningRules": "COL9999"})
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s status=%d", path, response.StatusCode)
		}
	}
}
