// SPDX-License-Identifier: AGPL-3.0-or-later

package scripts_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRuntimeSmokeScriptsCoverReleaseScenarios(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}

	scripts := []string{"runtime-smoke.sh", "runtime-smoke.ps1"}
	required := []string{
		"SHA256",
		"package archive",
		"success.cl",
		"semantic-error.cl",
		"runtime-error.cl",
		"helper.cl",
		"main.cl",
		"input.txt",
		"tokens",
		"ast",
		"symbols",
		"cfg",
		"ir",
		"assembly",
		"report",
		"cooler-lsp",
		"Cooler Compiler Lab",
		"MANUAL",
	}

	for _, name := range scripts {
		t.Run(name, func(t *testing.T) {
			contents, err := os.ReadFile(filepath.Join(repoRoot, "scripts", name))
			if err != nil {
				t.Fatal(err)
			}
			text := string(contents)
			for _, marker := range required {
				if !strings.Contains(text, marker) {
					t.Errorf("%s does not cover %q", name, marker)
				}
			}

			checksum := strings.Index(strings.ToLower(text), "checksum")
			execution := strings.Index(text, "success.cl")
			if checksum < 0 || execution < 0 || checksum > execution {
				t.Errorf("%s must verify checksums before creating or executing fixtures", name)
			}
		})
	}
	if contents, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "runtime-smoke.sh")); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(string(contents), "tar -xzf") || !strings.Contains(string(contents), "unzip -q") {
		t.Error("POSIX smoke must extract both distributed archive formats")
	}
	if contents, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "runtime-smoke.ps1")); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(string(contents), "Expand-Archive") {
		t.Error("Windows smoke must extract the distributed ZIP")
	}
}

func TestPOSIXRuntimeSmokeScriptParses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash syntax check is not required on Windows")
	}
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(context.Background(), "bash", "-n", filepath.Join(repoRoot, "scripts", "runtime-smoke.sh"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("runtime-smoke.sh syntax check failed: %v\n%s", err, output)
	}
}
