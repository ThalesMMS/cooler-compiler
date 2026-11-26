// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func astJSON(t *testing.T, program *Program) string {
	t.Helper()
	encoded, err := json.Marshal(program.ToAST())
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestLALRParserMatchesHandwrittenOnExamples(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("..", "..", "examples", "*.cl"))
	more, _ := filepath.Glob(filepath.Join("..", "..", "examples", "*", "*.cl"))
	files = append(files, more...)
	if len(files) == 0 {
		t.Fatal("no example sources")
	}
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		tokens, lexDiagnostics := LexSource("u", string(content))
		if len(lexDiagnostics) > 0 {
			continue
		}
		hand, handDiagnostics := Parse(tokens)
		generated, generatedDiagnostics := ParseLALR(tokens)
		if len(handDiagnostics) != 0 || len(generatedDiagnostics) != 0 {
			t.Fatalf("%s: diagnostics hand=%v generated=%v", file, handDiagnostics, generatedDiagnostics)
		}
		if a, b := astJSON(t, hand), astJSON(t, generated); a != b {
			t.Fatalf("%s: AST differs\nhand:      %.400s\ngenerated: %.400s", file, a, b)
		}
	}
}
