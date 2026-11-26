// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// frontendCorpusCase is one source program shared by the front-end tests.
type frontendCorpusCase struct {
	name   string
	units  []SourceUnit
	origin string
}

var parseSourceLiteral = regexp.MustCompile("parseSource\\(t, `([^`]*)`\\)")

// frontendCorpus gathers the shared corpus: the sources embedded in
// parser_test.go and lexer_test.go, the valid/invalid test programs, every
// conformance fixture source, and the examples.
func frontendCorpus(t *testing.T) []frontendCorpusCase {
	t.Helper()
	cases := []frontendCorpusCase{}
	for _, testFile := range []string{"parser_test.go", "lexer_test.go"} {
		content, err := os.ReadFile(testFile)
		if err != nil {
			t.Fatal(err)
		}
		for index, match := range parseSourceLiteral.FindAllStringSubmatch(string(content), -1) {
			name := testFile + "#" + string(rune('a'+index%26)) + strings.Repeat("'", index/26)
			cases = append(cases, frontendCorpusCase{name: name, origin: "parser_test", units: []SourceUnit{{ID: "u", Name: "test.cl", Content: match[1]}}})
		}
	}
	patterns := []string{
		filepath.Join("testdata", "valid", "*.cl"),
		filepath.Join("testdata", "invalid", "*.cl"),
		filepath.Join("testdata", "*.cl"),
		filepath.Join("testdata", "conformance", "*", "*", "*.cl"),
		filepath.Join("testdata", "golden", "*", "*.cl"),
		filepath.Join("..", "..", "examples", "*.cl"),
		filepath.Join("..", "..", "examples", "*", "*.cl"),
		filepath.Join("..", "course", "testdata", "phases", "*.cl"),
	}
	for _, pattern := range patterns {
		files, _ := filepath.Glob(pattern)
		sort.Strings(files)
		for _, file := range files {
			content, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			cases = append(cases, frontendCorpusCase{name: file, origin: pattern, units: []SourceUnit{{ID: "u", Name: filepath.Base(file), Content: string(content)}}})
		}
	}
	if len(cases) < 50 {
		t.Fatalf("front-end corpus unexpectedly small: %d cases", len(cases))
	}
	return cases
}

// TestLALRParserOnSharedCorpus runs the existing parser-test sources and the
// valid/invalid corpus through the generated parser: it must accept what the
// hand-written parser accepts, with the same AST, and reject what it rejects.
func TestLALRParserOnSharedCorpus(t *testing.T) {
	accepted, rejected := 0, 0
	for _, item := range frontendCorpus(t) {
		tokens, lexDiagnostics := LexSource("u", item.units[0].Content)
		if hasErrors(lexDiagnostics) {
			continue
		}
		hand, handDiagnostics := Parse(tokens)
		generated, generatedDiagnostics := ParseLALR(tokens)
		switch {
		case !hasErrors(handDiagnostics):
			if hasErrors(generatedDiagnostics) {
				t.Errorf("%s: generated parser rejects a valid program: %+v", item.name, generatedDiagnostics)
				continue
			}
			if astJSON(t, hand) != astJSON(t, generated) {
				t.Errorf("%s: ASTs differ", item.name)
			}
			accepted++
		default:
			if !hasErrors(generatedDiagnostics) {
				t.Errorf("%s: generated parser accepts an invalid program (hand-written: %s)", item.name, handDiagnostics[0].Message)
			}
			rejected++
		}
	}
	t.Logf("shared corpus: %d accepted, %d rejected", accepted, rejected)
	if accepted == 0 || rejected == 0 {
		t.Fatalf("corpus did not exercise both outcomes: accepted=%d rejected=%d", accepted, rejected)
	}
}

// TestFrontendsAgreeOnOperandForms covers COOL forms that are only legal
// because 'not' and assignment are expressions in any operand position.
func TestFrontendsAgreeOnOperandForms(t *testing.T) {
	for _, body := range []string{
		"1 + not b", "1 + x <- 3", "~ x <- 3", "isvoid not b", "x <- 1 + not 2 < 3",
		"not x <- 1 < 2", "y <- x <- 4", "1 + not b + 2", "(not b) = ((x <- 2) < 3)",
	} {
		source := "class A { f(x : Int, y : Int, b : Bool) : Object { " + body + " }; };"
		tokens, _ := LexSource("u", source)
		hand, handDiagnostics := Parse(tokens)
		generated, generatedDiagnostics := ParseLALR(tokens)
		if hasErrors(handDiagnostics) || hasErrors(generatedDiagnostics) {
			t.Fatalf("%s: hand=%+v generated=%+v", body, handDiagnostics, generatedDiagnostics)
		}
		if astJSON(t, hand) != astJSON(t, generated) {
			t.Fatalf("%s: ASTs differ\nhand:      %s\ngenerated: %s", body, astJSON(t, hand), astJSON(t, generated))
		}
	}
}
