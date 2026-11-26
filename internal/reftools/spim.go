// SPDX-License-Identifier: AGPL-3.0-or-later

package reftools

import (
	"os"
	"path/filepath"
	"strings"
)

// SPIMTools locates a user-supplied real SPIM and the Cool runtime's
// trap.handler for CI/dev comparisons only. The app never uses them.
type SPIMTools struct {
	SPIM        string
	TrapHandler string
}

// FindSPIM searches COOLER_REFERENCE_RUNNER (or COOLER_REFERENCE_SPIM),
// PATH, and the Cool root for spim, and COOLER_TRAP_HANDLER or the Cool
// root's lib/trap.handler for the runtime. SPIM is "" when absent.
func FindSPIM() SPIMTools {
	explicit := os.Getenv("COOLER_REFERENCE_RUNNER")
	if explicit == "" {
		explicit = os.Getenv("COOLER_REFERENCE_SPIM")
	}
	root := os.Getenv("COOLER_COOL_ROOT")
	tools := SPIMTools{SPIM: LocateReferenceTool(explicit, "spim", root)}
	tools.TrapHandler = os.Getenv("COOLER_TRAP_HANDLER")
	if tools.TrapHandler == "" {
		if root == "" {
			root = DefaultCoolRoot
		}
		candidate := filepath.Join(root, "lib", "trap.handler")
		if _, err := os.Stat(candidate); err == nil {
			tools.TrapHandler = candidate
		}
	}
	return tools
}

// Args returns the spim arguments that run program with the Cool runtime.
// Without an explicit trap.handler the spim on PATH is expected to be the
// course wrapper that already loads it.
func (t SPIMTools) Args(program string) []string {
	if t.TrapHandler != "" {
		return []string{"-exception_file", t.TrapHandler, "-file", program}
	}
	return []string{"-file", program}
}

// NormalizeSPIMOutput removes SPIM's banner lines (version, copyright, and
// "Loaded:" notices) so program output can be compared.
func NormalizeSPIMOutput(output string) string {
	lines := strings.SplitAfter(output, "\n")
	kept := lines[:0]
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "SPIM Version"),
			strings.HasPrefix(trimmed, "Copyright"),
			strings.HasPrefix(trimmed, "All Rights Reserved"),
			strings.HasPrefix(trimmed, "See the file README"),
			strings.HasPrefix(trimmed, "Loaded:"):
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "")
}
