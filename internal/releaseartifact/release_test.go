// SPDX-License-Identifier: AGPL-3.0-or-later

package releaseartifact

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestBuildProducesDeterministicSafePackagesAndVerifiedMetadata(t *testing.T) {
	input := makeReleaseInputs(t)
	first, second := filepath.Join(t.TempDir(), "first"), filepath.Join(t.TempDir(), "second")
	options := Options{
		InputDirectory: input, Version: "0.1.1", SourceCommit: strings.Repeat("a", 40),
		SourceDateEpoch: 1_788_134_400, BuildFlags: []string{"-trimpath"},
		SkippedChecks: []string{"Windows native build unavailable"}, RequireMacOSApp: true,
	}
	options.OutputDirectory = first
	firstManifest, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	options.OutputDirectory = second
	secondManifest, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstManifest.Packages) != 4 || len(secondManifest.Packages) != 4 {
		t.Fatalf("package count = %d and %d", len(firstManifest.Packages), len(secondManifest.Packages))
	}

	for _, name := range append(packageNames(firstManifest), "release-manifest.json", "SHA256SUMS") {
		left, err := os.ReadFile(filepath.Join(first, name))
		if err != nil {
			t.Fatal(err)
		}
		right, err := os.ReadFile(filepath.Join(second, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(left) != string(right) {
			t.Fatalf("repeated release output differs: %s", name)
		}
	}

	manifestData, err := os.ReadFile(filepath.Join(first, "release-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded Manifest
	if err := json.Unmarshal(manifestData, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != ManifestSchemaVersion || decoded.SourceCommit != options.SourceCommit || decoded.Validation.BuildStatus != "built" {
		t.Fatalf("unexpected release manifest: %+v", decoded)
	}
	if decoded.Validation.EvidenceDocument != "docs/release-validation-v0.1.1.md" {
		t.Fatalf("evidence document = %q", decoded.Validation.EvidenceDocument)
	}
	assertReleaseSchemaFields(t, filepath.Join("..", "..", "schemas", "release-manifest-v1.schema.json"), manifestData)
	for _, pkg := range decoded.Packages {
		size, digest, err := fileIdentity(filepath.Join(first, pkg.Filename))
		if err != nil {
			t.Fatal(err)
		}
		if size != pkg.ByteSize || digest != pkg.SHA256 || pkg.ValidationStatus != "built" {
			t.Fatalf("package identity mismatch: %+v", pkg)
		}
		assertArchiveSafe(t, filepath.Join(first, pkg.Filename))
	}
	macArchive := ""
	for _, pkg := range decoded.Packages {
		if pkg.Target.OS == "macos" {
			macArchive = filepath.Join(first, pkg.Filename)
			if !containsString(pkg.Modes, "app-bundle") {
				t.Fatalf("macOS package modes = %v, want app-bundle", pkg.Modes)
			}
		}
	}
	if macArchive == "" {
		t.Fatal("release manifest omitted macOS package")
	}
	assertArchiveContains(t, macArchive, "Cooler Compiler.app/Contents/MacOS/coolergui")
	assertChecksumFile(t, first)
}

func TestBuildDerivesEvidenceDocumentFromVersion(t *testing.T) {
	options := Options{
		InputDirectory: makeReleaseInputs(t), OutputDirectory: t.TempDir(),
		Version: "1.0.0", SourceCommit: strings.Repeat("a", 40), SourceDateEpoch: 1,
	}
	manifest, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Validation.EvidenceDocument != "docs/release-validation-v1.0.0.md" {
		t.Fatalf("evidence document = %q", manifest.Validation.EvidenceDocument)
	}
}

func assertReleaseSchemaFields(t *testing.T, path string, document []byte) {
	t.Helper()
	schemaData, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schemaData, &schema); err != nil {
		t.Fatalf("invalid JSON Schema: %v", err)
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(document, &value); err != nil {
		t.Fatal(err)
	}
	for _, field := range schema.Required {
		if _, exists := value[field]; !exists {
			t.Errorf("release manifest does not satisfy required schema field %q", field)
		}
	}
}

func TestArchivePathValidationRejectsTraversal(t *testing.T) {
	for _, name := range []string{"", "/absolute", "../escape", "a/../escape", "a\\windows"} {
		if err := validateArchiveName(name); err == nil {
			t.Fatalf("unsafe path was accepted: %q", name)
		}
	}
	if err := validateArchiveEntries([]archiveEntry{{Name: "same"}, {Name: "same"}}); err == nil {
		t.Fatal("duplicate archive paths were accepted")
	}
}

func TestWriteTarGzipSupportsDirectoryEntries(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "bundle")
	file := filepath.Join(directory, "tool")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("tool\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "bundle.tar.gz")
	entries := []archiveEntry{
		{Source: directory, Name: "root/bundle", Mode: 0o755, IsDir: true},
		{Source: file, Name: "root/bundle/tool", Mode: 0o755},
	}
	if err := writeTarGzip(archive, entries, time.Unix(1, 0).UTC()); err != nil {
		t.Fatal(err)
	}

	archiveFile, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer closeReleaseTest(t, archiveFile)
	compressed, err := gzip.NewReader(archiveFile)
	if err != nil {
		t.Fatal(err)
	}
	defer closeReleaseTest(t, compressed)
	reader := tar.NewReader(compressed)
	directoryHeader, err := reader.Next()
	if err != nil {
		t.Fatal(err)
	}
	if directoryHeader.Name != "root/bundle/" || directoryHeader.Typeflag != tar.TypeDir || directoryHeader.Size != 0 {
		t.Fatalf("unexpected directory header: %+v", directoryHeader)
	}
}

func TestBuildRejectsAmbiguousSourceIdentity(t *testing.T) {
	base := Options{InputDirectory: t.TempDir(), OutputDirectory: t.TempDir(), Version: "0.1.1", SourceCommit: strings.Repeat("a", 40), SourceDateEpoch: 1}
	invalidCommit := base
	invalidCommit.SourceCommit = "abc123"
	if _, err := Build(invalidCommit); err == nil || !strings.Contains(err.Error(), "full 40-character") {
		t.Fatalf("short source commit error = %v", err)
	}
	invalidVersion := base
	invalidVersion.Version = "../escape"
	if _, err := Build(invalidVersion); err == nil || !strings.Contains(err.Error(), "path-safe") {
		t.Fatalf("unsafe version error = %v", err)
	}
}

func makeReleaseInputs(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := []string{
		"LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md",
		"coolerc-linux-amd64", "coolergui-linux-amd64",
		"cooler-lsp-linux-amd64",
		"coolerc-linux-arm64", "coolergui-linux-arm64",
		"cooler-lsp-linux-arm64",
		"coolerc-windows-amd64.exe", "coolergui-windows-amd64.exe", "coolergui-windows-amd64-native.exe",
		"cooler-lsp-windows-amd64.exe",
		"coolerc-macos-arm64", "coolergui-macos-arm64-browser",
		"cooler-lsp-macos-arm64",
	}
	for _, name := range files {
		mode := os.FileMode(0o644)
		if strings.HasPrefix(name, "cooler") {
			mode = 0o755
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte("fixture:"+name+"\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
	appExecutable := filepath.Join(root, "Cooler Compiler.app", "Contents", "MacOS", "coolergui")
	if err := os.MkdirAll(filepath.Dir(appExecutable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(appExecutable, []byte("native-app\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Cooler Compiler.app", "Contents", "Info.plist"), []byte("plist\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func assertArchiveContains(t *testing.T, path, suffix string) {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeReleaseTest(t, reader)
	for _, entry := range reader.File {
		if strings.HasSuffix(strings.TrimSuffix(entry.Name, "/"), "/"+suffix) {
			return
		}
	}
	t.Fatalf("archive %s does not contain %s", path, suffix)
}

func packageNames(manifest Manifest) []string {
	names := make([]string, len(manifest.Packages))
	for index, pkg := range manifest.Packages {
		names[index] = pkg.Filename
	}
	return names
}

func assertArchiveSafe(t *testing.T, path string) {
	t.Helper()
	var names []string
	switch {
	case strings.HasSuffix(path, ".tar.gz"):
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer closeReleaseTest(t, file)
		compressed, err := gzip.NewReader(file)
		if err != nil {
			t.Fatal(err)
		}
		defer closeReleaseTest(t, compressed)
		reader := tar.NewReader(compressed)
		for {
			header, err := reader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			names = append(names, header.Name)
		}
	case strings.HasSuffix(path, ".zip"):
		reader, err := zip.OpenReader(path)
		if err != nil {
			t.Fatal(err)
		}
		defer closeReleaseTest(t, reader)
		for _, file := range reader.File {
			names = append(names, strings.TrimSuffix(file.Name, "/"))
		}
	default:
		t.Fatalf("unknown archive format: %s", path)
	}
	seen := map[string]struct{}{}
	for _, name := range names {
		if err := validateArchiveName(name); err != nil {
			t.Fatal(err)
		}
		if _, exists := seen[name]; exists {
			t.Fatalf("duplicate archive entry: %s", name)
		}
		seen[name] = struct{}{}
	}
}

func closeReleaseTest(t *testing.T, resource io.Closer) {
	t.Helper()
	if err := resource.Close(); err != nil {
		t.Errorf("close release fixture: %v", err)
	}
}

func assertChecksumFile(t *testing.T, directory string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(directory, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 || len(parts[0]) != 64 {
			t.Fatalf("invalid checksum line: %q", line)
		}
		_, digest, err := fileIdentity(filepath.Join(directory, parts[1]))
		if err != nil {
			t.Fatal(err)
		}
		if digest != parts[0] {
			t.Fatalf("checksum mismatch for %s", parts[1])
		}
		names = append(names, parts[1])
	}
	if !sort.StringsAreSorted(names) {
		t.Fatalf("checksums are not sorted: %v", names)
	}
}

func TestArchiveRejectsCourseTools(t *testing.T) {
	for _, name := range []string{"bin/spim", "cooler/coolc.exe", "lib/trap.handler", "goyacc", "x/Bison"} {
		if err := validateArchiveEntries([]archiveEntry{{Name: name}}); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
	if err := validateArchiveEntries([]archiveEntry{{Name: "cooler/coolerc"}, {Name: "cooler/coolergui.exe"}, {Name: "cooler/docs/parser.md"}}); err != nil {
		t.Fatal(err)
	}
}
