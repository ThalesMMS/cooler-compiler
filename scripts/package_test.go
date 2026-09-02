// SPDX-License-Identifier: AGPL-3.0-or-later

package scripts_test

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMakefileInvokesShellScriptsThroughBash(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Errorf("close Makefile: %v", err)
		}
	}()

	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		text := scanner.Text()
		if strings.HasPrefix(text, "\t") && strings.Contains(text, "./scripts/") && !strings.Contains(text, "bash ./scripts/") {
			t.Errorf("Makefile:%d executes a shell script through its shebang; use explicit bash so paths with spaces work: %s", line, text)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestBuildReleaseAvoidsWindowsFindShadowing(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "build-release.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	if strings.Contains(text, "find \"") {
		t.Fatal("build-release.sh invokes an ambiguous find executable; PowerShell-launched Bash can resolve FIND.EXE on Windows")
	}
	for _, want := range []string{
		`rm -rf -- "$work_dir"`,
		`output_entries=("$OUTPUT"/*)`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("build-release.sh does not contain portable release-directory handling %q", want)
		}
	}
}

func TestMacOSPackagingInstallsApplicationResources(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("macOS packaging script requires POSIX shell and PATH semantics")
	}

	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "build-macos-app.sh"))
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	for _, path := range []string{"scripts", "assets", "cmd/coolergui"} {
		if err := os.MkdirAll(filepath.Join(root, path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "scripts", "build-macos-app.sh"), script, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "AppIcon.icns"), []byte("test icon"), 0o644); err != nil {
		t.Fatal(err)
	}
	legalFiles := map[string]string{
		"LICENSE":                "test license",
		"NOTICE":                 "test notice",
		"THIRD_PARTY_NOTICES.md": "test third-party notices",
	}
	for name, contents := range legalFiles {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	bin := filepath.Join(root, "test-bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(bin, "uname"), "#!/bin/sh\necho Darwin\n")
	writeExecutable(t, filepath.Join(bin, "go"), `#!/bin/sh
set -eu
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    shift
    mkdir -p "$(dirname "$1")"
    printf 'test executable' > "$1"
    exit 0
  fi
  shift
done
exit 1
`)
	writeExecutable(t, filepath.Join(bin, "codesign"), "#!/bin/sh\nexit 0\n")

	command := exec.CommandContext(context.Background(), "bash", filepath.Join(root, "scripts", "build-macos-app.sh"), "9.8.7")
	command.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("packaging failed: %v\n%s", err, output)
	}

	app := filepath.Join(root, "dist", "Cooler Compiler.app")
	icon, err := os.ReadFile(filepath.Join(app, "Contents", "Resources", "AppIcon.icns"))
	if err != nil {
		t.Fatalf("AppIcon.icns was not installed: %v", err)
	}
	if string(icon) != "test icon" {
		t.Fatalf("installed icon = %q, want test icon", icon)
	}
	for name, want := range legalFiles {
		contents, err := os.ReadFile(filepath.Join(app, "Contents", "Resources", name))
		if err != nil {
			t.Fatalf("%s was not installed: %v", name, err)
		}
		if string(contents) != want {
			t.Fatalf("installed %s = %q, want %q", name, contents, want)
		}
	}

	plist, err := os.ReadFile(filepath.Join(app, "Contents", "Info.plist"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<key>CFBundleIconFile</key><string>AppIcon</string>",
		"<key>CFBundleDisplayName</key><string>Cooler Compiler</string>",
		"<key>CFBundleName</key><string>Cooler Compiler</string>",
		"<key>CFBundleShortVersionString</key><string>9.8.7</string>",
	} {
		if !strings.Contains(string(plist), want) {
			t.Fatalf("Info.plist does not contain %q", want)
		}
	}
}

func TestMacOSNativeMenuDefinesCommandQ(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(repoRoot, "cmd", "coolergui", "window_webview.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`initWithTitle:@"Quit Cooler Compiler"`,
		`action:@selector(terminate:)`,
		`keyEquivalent:@"q"`,
	} {
		if !strings.Contains(string(source), want) {
			t.Errorf("native macOS menu does not contain %q", want)
		}
	}
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
}
