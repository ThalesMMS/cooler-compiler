// SPDX-License-Identifier: AGPL-3.0-or-later

package releaseartifact

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

const ManifestSchemaVersion = "1.0.0"

type Options struct {
	InputDirectory  string
	OutputDirectory string
	Version         string
	SourceCommit    string
	SourceDateEpoch int64
	BuildFlags      []string
	SkippedChecks   []string
	RequireMacOSApp bool
}

type Manifest struct {
	SchemaVersion   string            `json:"schemaVersion"`
	ManifestType    string            `json:"manifestType"`
	Project         string            `json:"project"`
	Version         string            `json:"version"`
	SourceCommit    string            `json:"sourceCommit"`
	SourceDateEpoch int64             `json:"sourceDateEpoch"`
	Toolchain       Toolchain         `json:"toolchain"`
	BuildFlags      []string          `json:"buildFlags"`
	Packages        []PackageManifest `json:"packages"`
	Validation      Validation        `json:"validation"`
}

type Toolchain struct {
	GoVersion string `json:"goVersion"`
	HostOS    string `json:"hostOS"`
	HostArch  string `json:"hostArch"`
}

type Target struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

type PackageManifest struct {
	Filename         string   `json:"filename"`
	ByteSize         int64    `json:"byteSize"`
	SHA256           string   `json:"sha256"`
	Target           Target   `json:"target"`
	Modes            []string `json:"modes"`
	ValidationStatus string   `json:"validationStatus"`
}

type Validation struct {
	BuildStatus      string   `json:"buildStatus"`
	RuntimeStatus    string   `json:"runtimeStatus"`
	SkippedChecks    []string `json:"skippedChecks"`
	EvidenceDocument string   `json:"evidenceDocument"`
}

type archiveEntry struct {
	Source string
	Name   string
	Mode   fs.FileMode
	IsDir  bool
}

type packageSpec struct {
	Filename string
	Target   Target
	Modes    []string
	Format   string
	Entries  []archiveEntry
}

func Build(options Options) (Manifest, error) {
	if strings.TrimSpace(options.Version) == "" || strings.TrimSpace(options.SourceCommit) == "" {
		return Manifest{}, errors.New("version and source commit are required")
	}
	if !regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._-]*$`).MatchString(options.Version) {
		return Manifest{}, errors.New("version must be a path-safe release identifier")
	}
	commitBytes, err := hex.DecodeString(options.SourceCommit)
	if err != nil || len(commitBytes) != 20 {
		return Manifest{}, errors.New("source commit must be a full 40-character hexadecimal Git commit")
	}
	if options.SourceDateEpoch <= 0 {
		return Manifest{}, errors.New("SOURCE_DATE_EPOCH must be positive")
	}
	input, err := filepath.Abs(options.InputDirectory)
	if err != nil {
		return Manifest{}, err
	}
	output, err := filepath.Abs(options.OutputDirectory)
	if err != nil {
		return Manifest{}, err
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return Manifest{}, err
	}

	specs, err := packageSpecs(input, options.Version, options.RequireMacOSApp)
	if err != nil {
		return Manifest{}, err
	}
	timestamp := time.Unix(options.SourceDateEpoch, 0).UTC()
	manifest := Manifest{
		SchemaVersion:   ManifestSchemaVersion,
		ManifestType:    "cooler-release",
		Project:         "cooler-compiler",
		Version:         options.Version,
		SourceCommit:    options.SourceCommit,
		SourceDateEpoch: options.SourceDateEpoch,
		Toolchain:       Toolchain{GoVersion: runtime.Version(), HostOS: runtime.GOOS, HostArch: runtime.GOARCH},
		BuildFlags:      append([]string(nil), options.BuildFlags...),
		Validation: Validation{
			BuildStatus:      "built",
			RuntimeStatus:    "see-evidence-document",
			SkippedChecks:    append([]string(nil), options.SkippedChecks...),
			EvidenceDocument: fmt.Sprintf("docs/release-validation-v%s.md", options.Version),
		},
	}

	for _, spec := range specs {
		if err := validateArchiveEntries(spec.Entries); err != nil {
			return Manifest{}, fmt.Errorf("validate %s: %w", spec.Filename, err)
		}
		path := filepath.Join(output, spec.Filename)
		switch spec.Format {
		case "tar.gz":
			err = writeTarGzip(path, spec.Entries, timestamp)
		case "zip":
			err = writeZIP(path, spec.Entries, timestamp)
		default:
			err = fmt.Errorf("unsupported package format %q", spec.Format)
		}
		if err != nil {
			return Manifest{}, fmt.Errorf("build %s: %w", spec.Filename, err)
		}
		size, digest, err := fileIdentity(path)
		if err != nil {
			return Manifest{}, err
		}
		manifest.Packages = append(manifest.Packages, PackageManifest{
			Filename: spec.Filename, ByteSize: size, SHA256: digest,
			Target: spec.Target, Modes: append([]string(nil), spec.Modes...), ValidationStatus: "built",
		})
	}
	if err := writeMetadataFiles(output, manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func packageSpecs(input, version string, requireMacApp bool) ([]packageSpec, error) {
	legal := []string{"LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md"}
	for _, name := range legal {
		if err := requireRegularFile(filepath.Join(input, name)); err != nil {
			return nil, err
		}
	}
	newEntries := func(root string, binaries map[string]string) ([]archiveEntry, error) {
		entries := make([]archiveEntry, 0, len(binaries)+len(legal))
		for source, destination := range binaries {
			path := filepath.Join(input, source)
			if err := requireRegularFile(path); err != nil {
				return nil, err
			}
			entries = append(entries, archiveEntry{Source: path, Name: root + "/" + destination, Mode: 0o755})
		}
		for _, name := range legal {
			entries = append(entries, archiveEntry{Source: filepath.Join(input, name), Name: root + "/" + name, Mode: 0o644})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
		return entries, nil
	}

	var specs []packageSpec
	for _, target := range []struct {
		os, arch, format string
		binaries         map[string]string
		modes            []string
	}{
		{"linux", "amd64", "tar.gz", map[string]string{"coolerc-linux-amd64": "coolerc", "coolergui-linux-amd64": "coolergui", "cooler-lsp-linux-amd64": "cooler-lsp"}, []string{"cli", "lsp", "browser-gui"}},
		{"linux", "arm64", "tar.gz", map[string]string{"coolerc-linux-arm64": "coolerc", "coolergui-linux-arm64": "coolergui", "cooler-lsp-linux-arm64": "cooler-lsp"}, []string{"cli", "lsp", "browser-gui"}},
		{"windows", "amd64", "zip", map[string]string{"coolerc-windows-amd64.exe": "coolerc.exe", "coolergui-windows-amd64.exe": "coolergui.exe", "cooler-lsp-windows-amd64.exe": "cooler-lsp.exe"}, []string{"cli", "lsp", "browser-gui"}},
	} {
		root := fmt.Sprintf("cooler-compiler-v%s-%s-%s", version, target.os, target.arch)
		entries, err := newEntries(root, target.binaries)
		if err != nil {
			return nil, err
		}
		if target.os == "windows" {
			native := filepath.Join(input, "coolergui-windows-amd64-native.exe")
			if info, statErr := os.Stat(native); statErr == nil && info.Mode().IsRegular() {
				entries = append(entries, archiveEntry{Source: native, Name: root + "/coolergui-native.exe", Mode: 0o755})
				target.modes = append(target.modes, "native-webview2-gui")
				sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
			}
		}
		specs = append(specs, packageSpec{
			Filename: root + "." + target.format, Target: Target{OS: target.os, Arch: target.arch},
			Modes: target.modes, Format: target.format, Entries: entries,
		})
	}

	root := fmt.Sprintf("cooler-compiler-v%s-macos-arm64", version)
	entries, err := newEntries(root, map[string]string{
		"coolerc-macos-arm64": "coolerc", "coolergui-macos-arm64-browser": "coolergui-browser", "cooler-lsp-macos-arm64": "cooler-lsp",
	})
	if err != nil {
		return nil, err
	}
	modes := []string{"cli", "lsp", "browser-gui"}
	appPath := filepath.Join(input, "Cooler Compiler.app")
	if info, statErr := os.Stat(appPath); statErr == nil && info.IsDir() {
		for _, relative := range []string{
			filepath.Join("Contents", "Info.plist"),
			filepath.Join("Contents", "MacOS", "coolergui"),
		} {
			if err := requireRegularFile(filepath.Join(appPath, relative)); err != nil {
				return nil, fmt.Errorf("macOS application bundle is incomplete: %w", err)
			}
		}
		appEntries, walkErr := collectTree(appPath, root+"/Cooler Compiler.app")
		if walkErr != nil {
			return nil, walkErr
		}
		entries = append(entries, appEntries...)
		modes = append(modes, "app-bundle")
	} else if requireMacApp {
		return nil, fmt.Errorf("required macOS application bundle is missing: %s", appPath)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	specs = append(specs, packageSpec{
		Filename: root + ".zip", Target: Target{OS: "macos", Arch: "arm64"},
		Modes: modes, Format: "zip", Entries: entries,
	})
	sort.Slice(specs, func(i, j int) bool { return specs[i].Filename < specs[j].Filename })
	return specs, nil
}

func collectTree(sourceRoot, archiveRoot string) ([]archiveEntry, error) {
	var entries []archiveEntry
	err := filepath.WalkDir(sourceRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == sourceRoot {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.Mode().IsRegular() && !info.IsDir()) {
			return fmt.Errorf("unsupported application bundle entry: %s", path)
		}
		relative, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		name := archiveRoot + "/" + filepath.ToSlash(relative)
		mode := fs.FileMode(0o644)
		if info.IsDir() {
			mode = 0o755
		} else if info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		entries = append(entries, archiveEntry{Source: path, Name: name, Mode: mode, IsDir: info.IsDir()})
		return nil
	})
	return entries, err
}

func writeTarGzip(path string, entries []archiveEntry, timestamp time.Time) (returnErr error) {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	gzipWriter, err := gzip.NewWriterLevel(file, gzip.BestCompression)
	if err != nil {
		return err
	}
	gzipWriter.Header.ModTime = timestamp
	gzipWriter.Header.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		if err := validateArchiveName(entry.Name); err != nil {
			return err
		}
		info, err := os.Stat(entry.Source)
		if err != nil {
			return err
		}
		name := entry.Name
		size := info.Size()
		typeFlag := byte(tar.TypeReg)
		if entry.IsDir {
			name = strings.TrimSuffix(name, "/") + "/"
			size = 0
			typeFlag = tar.TypeDir
		}
		header := &tar.Header{Name: name, Mode: int64(entry.Mode.Perm()), Size: size, ModTime: timestamp, Typeflag: typeFlag, Format: tar.FormatPAX}
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if !entry.IsDir {
			if err := copyFile(tarWriter, entry.Source); err != nil {
				return err
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		return err
	}
	return gzipWriter.Close()
}

func writeZIP(path string, entries []archiveEntry, timestamp time.Time) (returnErr error) {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	writer := zip.NewWriter(file)
	for _, entry := range entries {
		if err := validateArchiveName(entry.Name); err != nil {
			return err
		}
		name := entry.Name
		if entry.IsDir && !strings.HasSuffix(name, "/") {
			name += "/"
		}
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		mode := entry.Mode
		if entry.IsDir {
			mode |= fs.ModeDir
		}
		header.SetMode(mode)
		header.Modified = timestamp
		part, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		if !entry.IsDir {
			if err := copyFile(part, entry.Source); err != nil {
				return err
			}
		}
	}
	return writer.Close()
}

func validateArchiveName(name string) error {
	if name == "" || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || strings.ContainsRune(name, 0) {
		return fmt.Errorf("unsafe archive path %q", name)
	}
	for _, part := range strings.Split(strings.TrimSuffix(name, "/"), "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("unsafe archive path %q", name)
		}
	}
	return nil
}

func validateArchiveEntries(entries []archiveEntry) error {
	seen := map[string]struct{}{}
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name, "/")
		if err := validateArchiveName(name); err != nil {
			return err
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("duplicate archive path %q", name)
		}
		if tool := courseToolName(name); tool != "" {
			return fmt.Errorf("archive path %q looks like the course tool %s; shipped artifacts must not bundle spim, the reference compiler, Flex, Bison, or goyacc", name, tool)
		}
		seen[name] = struct{}{}
	}
	return nil
}

// courseTools are reference tools and generators that are not included in
// Cooler release archives. An archive containing one was packaged incorrectly.
var courseTools = map[string]struct{}{
	"spim": {}, "xspim": {}, "qtspim": {}, "trap.handler": {},
	"coolc": {}, "lexer": {}, "parser": {}, "semant": {}, "cgen": {},
	"flex": {}, "bison": {}, "yacc": {}, "goyacc": {},
}

func courseToolName(name string) string {
	base := strings.ToLower(name[strings.LastIndex(name, "/")+1:])
	base = strings.TrimSuffix(base, ".exe")
	if _, ok := courseTools[base]; ok {
		return base
	}
	return ""
}

func copyFile(destination io.Writer, source string) (returnErr error) {
	file, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	_, err = io.Copy(destination, file)
	return err
}

func requireRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("required release input %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("required release input is not a regular file: %s", path)
	}
	return nil
}

func fileIdentity(path string) (int64, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	closeErr := file.Close()
	if err = errors.Join(err, closeErr); err != nil {
		return 0, "", err
	}
	return size, hex.EncodeToString(hash.Sum(nil)), nil
}

func writeMetadataFiles(directory string, manifest Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	manifestPath := filepath.Join(directory, "release-manifest.json")
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		return err
	}

	names := make([]string, 0, len(manifest.Packages)+1)
	for _, pkg := range manifest.Packages {
		names = append(names, pkg.Filename)
	}
	names = append(names, "release-manifest.json")
	sort.Strings(names)
	var sums strings.Builder
	for _, name := range names {
		_, digest, err := fileIdentity(filepath.Join(directory, name))
		if err != nil {
			return err
		}
		fmt.Fprintf(&sums, "%s  %s\n", digest, name)
	}
	return os.WriteFile(filepath.Join(directory, "SHA256SUMS"), []byte(sums.String()), 0o644)
}
