// SPDX-License-Identifier: AGPL-3.0-or-later

// Command cooler-doc-check validates the repository's maintained documentation.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/ThalesMMS/cooler-compiler/internal/conformance"
)

var (
	markdownLink = regexp.MustCompile(`!?\[[^\]]*\]\(([^\s)]+)(?:\s+[^)]*)?\)`)
	headingLine  = regexp.MustCompile(`^#{1,6}\s+(.+?)\s*#*\s*$`)
	diagnostic   = regexp.MustCompile(`COL[0-9]{4}`)
)

const (
	diagnosticStart = "<!-- BEGIN GENERATED DIAGNOSTIC CODES -->"
	diagnosticEnd   = "<!-- END GENERATED DIAGNOSTIC CODES -->"
)

var requiredDocuments = []string{
	"ROADMAP.md", "CHANGELOG.md", "CONTRIBUTING.md", "SECURITY.md",
	"docs/diagnostics.md", "docs/stack-ir.md", "docs/vm-runtime.md",
	"docs/debugger-protocol.md", "docs/artifact-contract.md",
	"docs/multifile.md", "docs/conformance.md", "docs/lsp.md",
	"docs/conformance-summary.md",
	"docs/release-notes-v1.0.0.md", "docs/release-validation-v0.1.1.md",
	"docs/release-validation-v1.0.0.md", "docs/v1.0-completion-contract.md",
	"docs/adversarial-audit-v1.0.0.md",
}

func main() {
	root, err := repositoryRoot()
	if err == nil {
		err = checkRepository(root)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("documentation consistency check passed")
}

func repositoryRoot() (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", errors.New("could not find repository root containing go.mod")
		}
		directory = parent
	}
}

func checkRepository(root string) error {
	var problems []string
	for _, name := range requiredDocuments {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(name))); err != nil {
			problems = append(problems, fmt.Sprintf("required document %s is missing", name))
		}
	}
	markdownFiles, err := collectMarkdown(root)
	if err != nil {
		return err
	}
	for _, path := range markdownFiles {
		problems = append(problems, checkMarkdown(root, path)...)
	}
	problems = append(problems, checkReadmeIndex(root)...)
	problems = append(problems, checkSchemas(root)...)
	problems = append(problems, checkDiagnosticInventory(root)...)
	problems = append(problems, checkConformanceSummary(root)...)
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("documentation consistency check failed:\n- %s", strings.Join(problems, "\n- "))
}

func checkConformanceSummary(root string) []string {
	corpus := filepath.Join(root, "internal", "compiler", "testdata", "conformance")
	matrix, fixtures, err := conformance.Load(corpus, root)
	if err != nil {
		return []string{fmt.Sprintf("conformance summary source is invalid: %v", err)}
	}
	path := filepath.Join(root, "docs", "conformance-summary.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{"docs/conformance-summary.md could not be read"}
	}
	if string(data) != conformance.RenderSummary(matrix, fixtures) {
		return []string{"docs/conformance-summary.md is stale; run make conformance"}
	}
	return nil
}

func collectMarkdown(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && (entry.Name() == ".git" || entry.Name() == "bin" || entry.Name() == "dist") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".md") {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

func checkMarkdown(root, path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{fmt.Sprintf("%s: %v", relative(root, path), err)}
	}
	anchors := markdownAnchors(string(data))
	var problems []string
	for _, match := range markdownLink.FindAllStringSubmatch(string(data), -1) {
		destination := strings.Trim(match[1], "<>")
		parsed, err := url.Parse(destination)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: invalid link %q", relative(root, path), destination))
			continue
		}
		if parsed.Scheme != "" || parsed.Host != "" || strings.HasPrefix(destination, "/") {
			continue
		}
		targetPath := path
		if parsed.Path != "" {
			decoded, decodeErr := url.PathUnescape(parsed.Path)
			if decodeErr != nil {
				problems = append(problems, fmt.Sprintf("%s: invalid escaped path %q", relative(root, path), parsed.Path))
				continue
			}
			targetPath = filepath.Clean(filepath.Join(filepath.Dir(path), filepath.FromSlash(decoded)))
			if !inside(root, targetPath) {
				problems = append(problems, fmt.Sprintf("%s: link escapes repository: %q", relative(root, path), destination))
				continue
			}
			if _, statErr := os.Stat(targetPath); statErr != nil {
				problems = append(problems, fmt.Sprintf("%s: missing link target %q", relative(root, path), destination))
				continue
			}
		}
		if parsed.Fragment != "" && strings.EqualFold(filepath.Ext(targetPath), ".md") {
			targetAnchors := anchors
			if targetPath != path {
				targetData, readErr := os.ReadFile(targetPath)
				if readErr != nil {
					continue
				}
				targetAnchors = markdownAnchors(string(targetData))
			}
			fragment, _ := url.PathUnescape(parsed.Fragment)
			if !targetAnchors[strings.ToLower(fragment)] {
				problems = append(problems, fmt.Sprintf("%s: missing heading for link %q", relative(root, path), destination))
			}
		}
	}
	return problems
}

func markdownAnchors(document string) map[string]bool {
	anchors := map[string]bool{}
	counts := map[string]int{}
	inFence := false
	for _, line := range strings.Split(document, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		match := headingLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		slug := headingSlug(match[1])
		if count := counts[slug]; count > 0 {
			slug = fmt.Sprintf("%s-%d", slug, count)
		}
		counts[headingSlug(match[1])]++
		anchors[slug] = true
	}
	return anchors
}

func headingSlug(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	var result strings.Builder
	for _, r := range text {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-':
			result.WriteRune(r)
		case unicode.IsSpace(r):
			result.WriteByte('-')
		}
	}
	return strings.Trim(result.String(), "-")
}

func checkReadmeIndex(root string) []string {
	data, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		return []string{"README.md could not be read"}
	}
	text := string(data)
	var problems []string
	for _, required := range requiredDocuments {
		if !strings.Contains(text, required) {
			problems = append(problems, fmt.Sprintf("README.md does not link to %s", required))
		}
	}
	return problems
}

func checkSchemas(root string) []string {
	paths, err := filepath.Glob(filepath.Join(root, "schemas", "*.json"))
	if err != nil || len(paths) == 0 {
		return []string{"schemas/: no JSON schemas found"}
	}
	var problems []string
	for _, path := range paths {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", relative(root, path), readErr))
			continue
		}
		var schema map[string]any
		if decodeErr := json.Unmarshal(data, &schema); decodeErr != nil {
			problems = append(problems, fmt.Sprintf("%s: invalid JSON: %v", relative(root, path), decodeErr))
			continue
		}
		id, _ := schema["$id"].(string)
		if id == "" || !strings.HasSuffix(id, filepath.Base(path)) {
			problems = append(problems, fmt.Sprintf("%s: $id must end with its filename", relative(root, path)))
		}
	}
	return problems
}

func checkDiagnosticInventory(root string) []string {
	codes := map[string]bool{}
	for _, directory := range []string{"internal/compiler", "cmd/coolerc"} {
		paths, _ := filepath.Glob(filepath.Join(root, directory, "*.go"))
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				return []string{fmt.Sprintf("%s: %v", relative(root, path), err)}
			}
			for _, code := range diagnostic.FindAllString(string(data), -1) {
				codes[code] = true
			}
		}
	}
	want := make([]string, 0, len(codes))
	for code := range codes {
		want = append(want, code)
	}
	sort.Strings(want)
	path := filepath.Join(root, "docs", "diagnostics.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{"docs/diagnostics.md: could not read generated inventory"}
	}
	text := string(data)
	start := strings.Index(text, diagnosticStart)
	end := strings.Index(text, diagnosticEnd)
	if start < 0 || end <= start {
		return []string{"docs/diagnostics.md: generated inventory markers are missing or reversed"}
	}
	section := text[start+len(diagnosticStart) : end]
	got := diagnostic.FindAllString(section, -1)
	if !equalStrings(got, want) {
		return []string{fmt.Sprintf("docs/diagnostics.md: generated inventory is stale (got %d codes, want %d)", len(got), len(want))}
	}
	return nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func relative(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}
