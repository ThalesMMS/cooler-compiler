// SPDX-License-Identifier: AGPL-3.0-or-later

package reftools

import (
	"os/exec"
	"strings"
	"testing"
)

// TestShippedAppsDoNotLinkCourseTools checks that the shipped binaries and the
// public API never link tool discovery, the differential harnesses, or the
// lexer generator.
func TestShippedAppsDoNotLinkCourseTools(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go tool not available")
	}
	shipped := []string{"../../cmd/coolergui", "../../cmd/coolerc", "../../cmd/cooler-lsp", "../../cmd/cooler-dap", "../../compiler"}
	output, err := exec.CommandContext(t.Context(), "go", append([]string{"list", "-deps"}, shipped...)...).Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, line := range strings.Fields(string(output)) {
		for _, forbidden := range []string{"/internal/reftools", "/internal/conformance", "/internal/lexgen"} {
			if strings.HasSuffix(line, forbidden) {
				t.Errorf("a shipped package depends on %s", line)
			}
		}
	}
	wasm := exec.CommandContext(t.Context(), "go", "list", "-deps", "../../cmd/cooler-wasm")
	wasm.Env = append(wasm.Environ(), "GOOS=js", "GOARCH=wasm")
	output, err = wasm.Output()
	if err != nil {
		t.Fatalf("go list wasm: %v", err)
	}
	for _, line := range strings.Fields(string(output)) {
		if strings.HasSuffix(line, "/internal/reftools") || strings.HasSuffix(line, "/internal/conformance") {
			t.Errorf("the WebAssembly build depends on %s", line)
		}
	}
}
