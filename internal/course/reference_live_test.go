// SPDX-License-Identifier: AGPL-3.0-or-later

package course

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
	"github.com/ThalesMMS/cooler-compiler/internal/reftools"
)

// requireReferenceTool returns a user-supplied course tool or skips (fails
// under STRICT=1 / COOLER_REFERENCE_STRICT=1) when it is absent.
func requireReferenceTool(t *testing.T, name string) string {
	t.Helper()
	path := reftools.FindReferenceTool(name)
	if path == "" {
		if reftools.StrictReferenceTools() {
			t.Fatalf("reference %s not found (strict mode); set COOLER_REFERENCE_%s or COOLER_COOL_ROOT", name, name)
		}
		t.Skipf("reference %s not found; live comparison skipped", name)
	}
	return path
}

func phaseFixtures(t *testing.T, pattern string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join("testdata", "phases", pattern))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no fixtures for %s: %v", pattern, err)
	}
	return matches
}

// runReferenceTool runs tool in dir with the fixture's base name as argument,
// so the course tool and Cooler see the same relative file name.
func runReferenceTool(t *testing.T, tool, dir string, stdin []byte, args ...string) (string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, tool, args...)
	command.Dir = dir
	command.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	_ = command.Run()
	return stdout.String(), stderr.String()
}

func TestLiveReferenceLexerMatches(t *testing.T) {
	lexer := requireReferenceTool(t, "lexer")
	for _, fixture := range phaseFixtures(t, "lex_*.cl") {
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			content, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatal(err)
			}
			dir, name := filepath.Dir(fixture), filepath.Base(fixture)
			want, _ := runReferenceTool(t, lexer, dir, nil, name)
			got := compiler.ReferenceTokenDump([]compiler.SourceUnit{{ID: name, Name: name, Content: string(content)}})
			if got != want {
				t.Fatalf("token stream differs from %s\n--- cooler\n%s\n--- reference\n%s", lexer, got, want)
			}
		})
	}
}

func TestReferenceTokenFixturesBuildOffline(t *testing.T) {
	for _, fixture := range phaseFixtures(t, "lex_*.cl") {
		content, err := os.ReadFile(fixture)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(fixture)
		dump := compiler.ReferenceTokenDump([]compiler.SourceUnit{{ID: name, Name: name, Content: string(content)}})
		if !bytes.HasPrefix([]byte(dump), []byte("#name \""+name+"\"\n")) {
			t.Fatalf("%s: unexpected dump header:\n%s", name, dump)
		}
	}
}

// runCoursePipeline runs `lexer name | next...` the way the course scripts do.
func runCoursePipeline(t *testing.T, dir, name string, tools ...string) (string, string) {
	t.Helper()
	stdout, stderr := runReferenceTool(t, tools[0], dir, nil, name)
	for _, tool := range tools[1:] {
		var nextErr string
		stdout, nextErr = runReferenceTool(t, tool, dir, []byte(stdout))
		stderr += nextErr
	}
	return stdout, stderr
}

func TestLiveReferenceParserMatches(t *testing.T) {
	lexer := requireReferenceTool(t, "lexer")
	parser := requireReferenceTool(t, "parser")
	for _, fixture := range phaseFixtures(t, "parse_*.cl") {
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			content, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatal(err)
			}
			dir, name := filepath.Dir(fixture), filepath.Base(fixture)
			stdout, stderr := runCoursePipeline(t, dir, name, lexer, parser)
			got, ok := compiler.ReferenceParserDump([]compiler.SourceUnit{{ID: name, Name: name, Content: string(content)}})
			want := stdout
			if !ok {
				want = stderr
			}
			if got != want {
				t.Fatalf("parser output differs\n--- cooler\n%s\n--- reference\n%s", got, want)
			}
		})
	}
}

func TestLiveReferenceSemantMatches(t *testing.T) {
	lexer := requireReferenceTool(t, "lexer")
	parser := requireReferenceTool(t, "parser")
	semant := requireReferenceTool(t, "semant")
	for _, fixture := range phaseFixtures(t, "semant_*.cl") {
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			content, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatal(err)
			}
			dir, name := filepath.Dir(fixture), filepath.Base(fixture)
			stdout, stderr := runCoursePipeline(t, dir, name, lexer, parser, semant)
			got, ok := compiler.ReferenceSemantDump([]compiler.SourceUnit{{ID: name, Name: name, Content: string(content)}})
			want := stdout
			if !ok {
				want = stderr
			}
			if got != want {
				t.Fatalf("semant output differs\n--- cooler\n%s\n--- reference\n%s", got, want)
			}
		})
	}
}
