// SPDX-License-Identifier: AGPL-3.0-or-later

// Package reftools discovers user-supplied course tools (lexer, parser,
// semant, coolc, spim, trap.handler) for CI/dev comparisons. Only tests and
// cmd/cooler-differential import it; the shipped apps never link it.
package reftools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DefaultCoolRoot is the classic course installation root. It is only a
// search location for user-supplied tools; nothing is downloaded or bundled.
const DefaultCoolRoot = "/var/tmp/cool"

// FindReferenceTool locates a user-supplied course tool (lexer, parser,
// semant, coolc, spim) for CI/dev comparisons. Search order: the
// COOLER_REFERENCE_<NAME> environment variable, PATH, then the Cool root
// (COOLER_COOL_ROOT or /var/tmp/cool) in the bin/ and lib/.<arch> layouts.
// It returns "" when the tool is absent. Shipped apps never call this.
func FindReferenceTool(name string) string {
	return LocateReferenceTool(os.Getenv("COOLER_REFERENCE_"+strings.ToUpper(name)), name, os.Getenv("COOLER_COOL_ROOT"))
}

// LocateReferenceTool resolves an explicit path (when non-empty) or searches
// PATH and then the Cool root (default /var/tmp/cool) for name.
func LocateReferenceTool(explicit, name, root string) string {
	if explicit != "" {
		if path, err := exec.LookPath(explicit); err == nil {
			return path
		}
		return ""
	}
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	if root == "" {
		root = DefaultCoolRoot
	}
	for _, dir := range []string{"bin", "lib/.x86_64", "lib/.i686", "bin/.x86_64", "bin/.i686"} {
		candidate := filepath.Join(root, filepath.FromSlash(dir), name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate
		}
	}
	return ""
}

// StrictReferenceTools reports whether missing tools must fail instead of skip.
func StrictReferenceTools() bool {
	for _, name := range []string{"COOLER_REFERENCE_STRICT", "STRICT"} {
		switch strings.ToLower(os.Getenv(name)) {
		case "1", "true", "yes":
			return true
		}
	}
	return false
}
