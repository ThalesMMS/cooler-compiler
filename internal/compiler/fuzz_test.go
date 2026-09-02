// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const (
	fuzzMaxSourceBytes   = 32 << 10
	fuzzMaxInputBytes    = 8 << 10
	fuzzMaxMutationBytes = 128
	fuzzMaxCorpusSeeds   = 128
	fuzzVMMaxSteps       = 512
)

var representativeFuzzFixtures = []string{
	"testdata/conformance/lexer/comments-invalid/tokens.cl",
	"testdata/conformance/lexer/locations-valid/tokens.cl",
	"testdata/conformance/lexer/strings-valid/tokens.cl",
	"testdata/conformance/parser/expressions-valid/main.cl",
	"testdata/conformance/parser/recovery-multiple-invalid/main.cl",
	"testdata/conformance/projects/multifile-valid/helper.cl",
	"testdata/conformance/projects/multifile-valid/main.cl",
	"testdata/conformance/semantics/classes/classes-valid/main.cl",
	"testdata/conformance/semantics/classes/cycle-invalid/main.cl",
	"testdata/conformance/semantics/expressions/expressions-valid/main.cl",
	"testdata/conformance/semantics/expressions/operators-invalid/main.cl",
	"testdata/conformance/runtime/core-valid/main.cl",
	"testdata/conformance/runtime/limit-paused/main.cl",
	"testdata/conformance/runtime/void-dispatch-invalid/main.cl",
}

func addCompilerFuzzSourceSeeds(f *testing.F, add func(string)) {
	f.Helper()
	paths := []string{}
	exampleRoot := filepath.Join("..", "..", "examples")
	err := filepath.WalkDir(exampleRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && filepath.Ext(path) == ".cl" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil || len(paths) == 0 {
		f.Fatalf("load bundled example fuzz seeds: paths=%v err=%v", paths, err)
	}
	paths = append(paths, representativeFuzzFixtures...)
	regressions, err := filepath.Glob(filepath.Join("testdata", "fuzz-regressions", "*.cl"))
	if err != nil {
		f.Fatalf("load fuzz regression paths: %v", err)
	}
	paths = append(paths, regressions...)
	sort.Strings(paths)

	seen := map[string]bool{}
	for _, path := range paths {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			f.Fatalf("read fuzz seed %s: %v", path, readErr)
		}
		source := string(data)
		if len(source) <= fuzzMaxSourceBytes && !seen[source] {
			if len(seen) >= fuzzMaxCorpusSeeds {
				f.Fatalf("shared fuzz seed corpus exceeds %d inputs", fuzzMaxCorpusSeeds)
			}
			seen[source] = true
			add(source)
		}
	}
	for _, source := range compilerAdversarialSeeds() {
		if !seen[source] {
			if len(seen) >= fuzzMaxCorpusSeeds {
				f.Fatalf("shared fuzz seed corpus exceeds %d inputs", fuzzMaxCorpusSeeds)
			}
			seen[source] = true
			add(source)
		}
	}
}

func TestRecoveredProgramASTSpanRegression(t *testing.T) {
	sourceData, err := os.ReadFile(filepath.Join("testdata", "fuzz-regressions", "empty-token-stream.cl"))
	if err != nil {
		t.Fatal(err)
	}
	result := compileFuzzSource(string(sourceData), "")
	if result.AST == nil || !validVerifierSpan(result.AST.Span) || result.AST.Span.SourceID != "source-1" {
		t.Fatalf("recovered Program AST has invalid source span: %+v", result.AST)
	}
}

func compilerAdversarialSeeds() []string {
	deep := "class Main { main() : Object { " + strings.Repeat("(", 256) + "0" + strings.Repeat(")", 256) + " }; };"
	longIdentifier := "class Main { " + strings.Repeat("identifier", 256) + " : Int; main() : Object { 0 }; };"
	return []string{
		"",
		"class Main { main() : Object { \"Olá, 世界 😀\" }; };",
		"class Main {\r\n  main() : Object {\r\n    0\r\n  };\r\n};\r\n",
		"class Main { main() : Object { \"unterminated\n }; };",
		"class Main { main() : Object { " + strings.Repeat("9", 4096) + " }; };",
		"(* " + strings.Repeat("nested payload ", 256) + " *) class Main { main() : Object { 0 }; };",
		longIdentifier,
		deep,
		string([]byte{'c', 'l', 'a', 's', 's', ' ', 0xff, 0xfe}),
	}
}

func FuzzCompile(f *testing.F) {
	addCompilerFuzzSourceSeeds(f, func(source string) { f.Add(source) })
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > fuzzMaxSourceBytes {
			t.Skip()
		}
		first := compileFuzzSource(source, "")
		second := compileFuzzSource(source, "")
		assertFuzzResult(t, first, map[string]string{"source-1": source})
		assertDeterministicResult(t, first, second)
	})
}

func FuzzCompileProject(f *testing.F) {
	addCompilerFuzzSourceSeeds(f, func(source string) { f.Add(source, "") })
	helper, helperErr := os.ReadFile(filepath.Join("testdata", "conformance", "projects", "multifile-valid", "helper.cl"))
	mainSource, mainErr := os.ReadFile(filepath.Join("testdata", "conformance", "projects", "multifile-valid", "main.cl"))
	if helperErr != nil || mainErr != nil {
		f.Fatalf("read multifile fuzz seed: helper=%v main=%v", helperErr, mainErr)
	}
	f.Add(string(helper), string(mainSource))
	f.Fuzz(func(t *testing.T, firstSource, secondSource string) {
		if len(firstSource)+len(secondSource) > fuzzMaxSourceBytes {
			t.Skip()
		}
		units := []SourceUnit{{ID: "fuzz-a", Name: "a.cl", Content: firstSource}}
		sources := map[string]string{"fuzz-a": firstSource}
		if secondSource != "" {
			units = append(units, SourceUnit{ID: "fuzz-b", Name: "b.cl", Content: secondSource})
			sources["fuzz-b"] = secondSource
		}
		options := ProjectOptions{VMMaxSteps: fuzzVMMaxSteps, CompilerVersion: "fuzz"}
		first := CompileProject(units, options)
		second := CompileProject(units, options)
		assertFuzzResult(t, first, sources)
		assertDeterministicResult(t, first, second)
	})
}

func FuzzProgramInput(f *testing.F) {
	addCompilerFuzzSourceSeeds(f, func(source string) { f.Add(source, "") })
	f.Add(`class Main inherits IO { main() : Object { { out_string(in_string()); out_int(in_int()); } }; };`, "hello\r\n-42\n")
	f.Fuzz(func(t *testing.T, source, input string) {
		if len(source) > fuzzMaxSourceBytes || len(input) > fuzzMaxInputBytes {
			t.Skip()
		}
		first := compileFuzzSource(source, input)
		second := compileFuzzSource(source, input)
		assertFuzzResult(t, first, map[string]string{"source-1": source})
		assertDeterministicResult(t, first, second)
	})
}

func FuzzJSONRoundTrip(f *testing.F) {
	addCompilerFuzzSourceSeeds(f, func(source string) { f.Add(source) })
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > fuzzMaxSourceBytes {
			t.Skip()
		}
		result := compileFuzzSource(source, "")
		assertFuzzResult(t, result, map[string]string{"source-1": source})
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("marshal compiler result: %v", err)
		}
		decoded, err := DecodeReport(encoded)
		if err != nil {
			t.Fatalf("decode compiler report: %v", err)
		}
		reencoded, err := json.Marshal(decoded)
		if err != nil {
			t.Fatalf("remarshal compiler report: %v", err)
		}
		if !bytes.Equal(encoded, reencoded) {
			t.Fatal("v1 compiler report changed after JSON round trip")
		}
	})
}

func compileFuzzSource(source, input string) Result {
	return CompileWithContract(source, input, ResultContract{
		CompilerVersion: "fuzz",
		Options: CompilationOptions{
			Execute: true, VMMaxSteps: fuzzVMMaxSteps, InputProvided: input != "",
		},
	})
}

func assertDeterministicResult(t testing.TB, first, second Result) {
	t.Helper()
	if !reflect.DeepEqual(first, second) {
		t.Fatal("compiler result changed across identical bounded runs")
	}
	firstJSON, firstErr := json.Marshal(first)
	secondJSON, secondErr := json.Marshal(second)
	if firstErr != nil || secondErr != nil || !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("serialized compiler result is nondeterministic: first=%v second=%v", firstErr, secondErr)
	}
}

func assertFuzzResult(t testing.TB, result Result, sources map[string]string) {
	t.Helper()
	if result.Runtime.ExecutedSteps < 0 || result.Runtime.ExecutedSteps > result.Options.VMMaxSteps || result.Runtime.ExecutedSteps > fuzzVMMaxSteps {
		t.Fatalf("VM escaped configured step bound: steps=%d options=%d", result.Runtime.ExecutedSteps, result.Options.VMMaxSteps)
	}
	if _, err := json.Marshal(result); err != nil {
		t.Fatalf("compiler result is not JSON serializable: %v", err)
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Stage == "" || diagnostic.Severity == "" || diagnostic.Message == "" {
			t.Fatalf("uncontrolled diagnostic: %+v", diagnostic)
		}
		if strings.HasPrefix(diagnostic.Code, "COL51") {
			t.Fatalf("compiler-generated bytecode failed verification: %+v", diagnostic)
		}
	}
	if hasFrontendErrors(result.Diagnostics) {
		if len(result.IR) != 0 || len(result.Assembly) != 0 || result.Runtime.ExecutedSteps != 0 {
			t.Fatal("frontend or semantic errors crossed the backend execution barrier")
		}
	}
	assertResultSpans(t, result, sources)
}

func hasFrontendErrors(diagnostics []Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity != SeverityError {
			continue
		}
		switch diagnostic.Stage {
		case "input", "lexer", "parser", "semantic":
			return true
		}
	}
	return false
}

func assertResultSpans(t testing.TB, result Result, sources map[string]string) {
	t.Helper()
	previousTokenEnd := map[string]int{}
	for index, token := range result.Tokens {
		assertBoundedSpan(t, token.Span, sources, "token")
		key := token.Span.SourceID
		if token.Span.StartOffset < previousTokenEnd[key] {
			t.Fatalf("token %d regressed within source %q: %+v", index, key, token.Span)
		}
		previousTokenEnd[key] = token.Span.EndOffset
	}
	if result.AST != nil {
		walkAST(result.AST, func(node *ASTNode) { assertBoundedSpan(t, node.Span, sources, "AST "+node.Kind) })
	}
	for _, symbol := range result.Symbols {
		assertBoundedSpan(t, symbol.Span, sources, "symbol")
	}
	for _, instruction := range result.IR {
		assertBoundedSpan(t, instruction.Span, sources, "IR")
	}
	for _, span := range result.AssemblySpans {
		assertBoundedSpan(t, span, sources, "assembly")
	}
	for _, block := range result.ControlFlow.Blocks {
		assertBoundedSpan(t, block.Span, sources, "CFG block")
	}
	for _, edge := range result.ControlFlow.Edges {
		assertBoundedSpan(t, edge.Span, sources, "CFG edge")
	}
	for _, diagnostic := range result.Diagnostics {
		assertBoundedSpan(t, diagnostic.Span, sources, "diagnostic")
		for _, related := range diagnostic.Related {
			assertBoundedSpan(t, related.Span, sources, "related diagnostic")
		}
		for _, fix := range diagnostic.QuickFix {
			assertBoundedSpan(t, fix.Span, sources, "quick fix")
		}
	}
	for _, state := range result.Runtime.Trace {
		assertBoundedSpan(t, state.Span, sources, "runtime trace")
		for _, frame := range state.Frames {
			assertBoundedSpan(t, frame.Span, sources, "runtime trace frame")
		}
	}
	for _, frame := range result.Runtime.Frames {
		assertBoundedSpan(t, frame.Span, sources, "runtime frame")
	}
	for _, diagnostic := range result.Runtime.Diagnostics {
		assertBoundedSpan(t, diagnostic.Span, sources, "runtime diagnostic")
	}
}

func assertBoundedSpan(t testing.TB, span Span, sources map[string]string, artifact string) {
	t.Helper()
	if !validVerifierSpan(span) {
		t.Fatalf("%s has malformed span %+v", artifact, span)
	}
	content, found := sources[span.SourceID]
	if !found && span.SourceID == "" && len(sources) == 1 {
		for _, source := range sources {
			content, found = source, true
		}
	}
	if !found && span.SourceID != "" {
		t.Fatalf("%s references unknown source %q", artifact, span.SourceID)
	}
	if found && span.EndOffset > sourceUTF16Length(content) {
		t.Fatalf("%s span exceeds source: %+v length=%d", artifact, span, sourceUTF16Length(content))
	}
}

func sourceUTF16Length(source string) int {
	runes, _ := decodeSource(source)
	total := 0
	for _, value := range runes {
		total += utf16RuneLen(value)
	}
	return total
}

func walkAST(node *ASTNode, visit func(*ASTNode)) {
	if node == nil {
		return
	}
	visit(node)
	for index := range node.Children {
		walkAST(&node.Children[index], visit)
	}
}

func mutationsByte(mutations []byte, index int) byte {
	if len(mutations) == 0 {
		return 0
	}
	return mutations[index%len(mutations)]
}
