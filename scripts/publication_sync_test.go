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

func TestPublicationSyncExportsAndComparesSanitizedSnapshot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("publication sync script requires POSIX shell tools")
	}

	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "publication-sync.sh"))
	if err != nil {
		t.Fatal(err)
	}

	testRoot := t.TempDir()
	development := filepath.Join(testRoot, "development")
	public := filepath.Join(testRoot, "public")
	for _, directory := range []string{
		filepath.Join(development, "docs"),
		filepath.Join(development, "examples", "stack-machine"),
		filepath.Join(development, "scripts"),
		public,
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(development, "README.md"), "public contents\n", 0o644)
	writeFile(t, filepath.Join(development, "examples", "13_stack_machine.cl"), "unlicensed teaching material\n", 0o644)
	writeFile(t, filepath.Join(development, "examples", "stack-machine", "atoi.cl"), "unlicensed teaching material\n", 0o644)
	writeFile(t, filepath.Join(development, "examples", "stack-machine", "stack.test"), "unlicensed teaching material\n", 0o644)
	writeFile(t, filepath.Join(development, "docs", "development-publication.md"), "private docs\n", 0o644)
	writeFile(t, filepath.Join(development, "scripts", "publication-sync.sh"), string(script), 0o755)
	writeFile(t, filepath.Join(development, "scripts", "publication_sync_test.go"), "private test\n", 0o644)
	git(t, development, "init", "-q")
	git(t, development, "config", "user.name", "Publication Test")
	git(t, development, "config", "user.email", "publication@example.invalid")
	git(t, development, "add", ".")
	git(t, development, "commit", "-qm", "development snapshot")
	developmentCommit := strings.TrimSpace(git(t, development, "rev-parse", "HEAD"))

	candidate := filepath.Join(testRoot, "candidate")
	output := run(t, development, "bash", "scripts/publication-sync.sh", "export", "HEAD", candidate)
	if !strings.Contains(output, developmentCommit) {
		t.Fatalf("export output does not identify source commit %s:\n%s", developmentCommit, output)
	}
	if _, err := os.Stat(filepath.Join(candidate, "README.md")); err != nil {
		t.Fatalf("public file was not exported: %v", err)
	}
	for _, path := range []string{
		filepath.Join(candidate, ".git"),
		filepath.Join(candidate, "docs", "development-publication.md"),
		filepath.Join(candidate, "scripts", "publication-sync.sh"),
		filepath.Join(candidate, "scripts", "publication_sync_test.go"),
		filepath.Join(candidate, "examples", "13_stack_machine.cl"),
		filepath.Join(candidate, "examples", "stack-machine", "atoi.cl"),
		filepath.Join(candidate, "examples", "stack-machine", "stack.test"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("internal path was exported: %s", path)
		}
	}
	provenance, err := os.ReadFile(candidate + ".publication-source")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(provenance), "source_commit="+developmentCommit) {
		t.Fatalf("provenance does not identify source commit:\n%s", provenance)
	}

	writeFile(t, filepath.Join(public, "README.md"), "public contents\n", 0o644)
	git(t, public, "init", "-q")
	git(t, public, "config", "user.name", "Publication Test")
	git(t, public, "config", "user.email", "publication@example.invalid")
	git(t, public, "add", ".")
	git(t, public, "commit", "-qm", "public snapshot")
	compareOutput := run(t, development, "bash", "scripts/publication-sync.sh", "compare", "HEAD", public, "HEAD")
	if !strings.Contains(compareOutput, "Public snapshot matches exactly.") {
		t.Fatalf("unexpected compare output:\n%s", compareOutput)
	}

	if err := os.Chmod(filepath.Join(public, "README.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, public, "add", ".")
	git(t, public, "commit", "-qm", "introduce mode drift")
	command := exec.CommandContext(context.Background(), "bash", "scripts/publication-sync.sh", "compare", "HEAD", public, "HEAD")
	command.Dir = development
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("compare accepted mode-only drift:\n%s", output)
	}
	if err := os.Chmod(filepath.Join(public, "README.md"), 0o644); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(public, "unexpected.txt"), "drift\n", 0o644)
	git(t, public, "add", ".")
	git(t, public, "commit", "-qm", "introduce drift")
	command = exec.CommandContext(context.Background(), "bash", "scripts/publication-sync.sh", "compare", "HEAD", public, "HEAD")
	command.Dir = development
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("compare accepted a divergent public snapshot:\n%s", output)
	}
}

func git(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.CommandContext(context.Background(), "git", args...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func run(t *testing.T, directory string, name string, args ...string) string {
	t.Helper()
	command := exec.CommandContext(context.Background(), name, args...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed: %v\n%s", name, err, output)
	}
	return string(output)
}

func writeFile(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
}
