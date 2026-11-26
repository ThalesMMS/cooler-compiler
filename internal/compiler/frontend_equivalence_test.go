// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"encoding/json"
	"strings"
	"testing"
)

// The front-end equivalence suite proves the hand-written front-end and the
// generated one (cool.lex + cool.y) are interchangeable. Run it with
// `make frontend-equivalence`; it needs no course toolchain and no generator
// at test time. Lexer equality (tokens, spans, diagnostics, and reference
// ERROR tokens) is covered by TestGeneratedLexer*; parser acceptance and AST
// equality by TestLALRParserOnSharedCorpus and TestFrontendsAgreeOnOperandForms.

// parserDiagnosticAllowlist documents the only accepted difference: the
// generated parser reports LALR syntax errors (COL2200, course wording) where
// the hand-written parser reports its own COL20xx code and message. Both must
// still reject the same programs, at the same first offending token, except
// for the constructs listed in parserLocationAllowlist.
const parserDiagnosticAllowlist = "COL2200 replaces COL20xx wording; removal: never (different parser technologies)"

// parserLocationAllowlist lists hand-written diagnostics that are reported
// at a different token than the LALR parser's first syntax error, with the
// reason. Each is a check the recursive-descent parser makes after it has
// consumed a construct, where an LR parser fails on the next token instead.
var parserLocationAllowlist = map[string]string{
	"COL2082": "empty block: reported for the whole '{ }' span; LALR fails at '}'",
	"COL2107": "case without branches: reported for the whole case span; LALR fails at 'esac'",
	"COL2002": "program without classes: reported at the first token; LALR fails at end of input",
}

func compileWithFrontend(t *testing.T, units []SourceUnit, frontend string) Result {
	t.Helper()
	pipeline := NewProjectCompilationSession(units, ResultContract{Options: CompilationOptions{Execute: true, VMMaxSteps: 20000, Frontend: frontend}})
	pipeline.Complete()
	if debug, ok := pipeline.DebugSession(); ok && !debug.blocked {
		return debug.Run(20000, nil).Result
	}
	return pipeline.Snapshot().Result
}

func TestFrontendEquivalenceFullPipeline(t *testing.T) {
	compared := 0
	for _, item := range frontendCorpus(t) {
		handwritten := compileWithFrontend(t, item.units, "")
		generated := compileWithFrontend(t, item.units, FrontendGenerated)
		if hasStageErrors(handwritten.Diagnostics, "parser") || hasStageErrors(generated.Diagnostics, "parser") {
			compareParserDiagnostics(t, item.name, handwritten.Diagnostics, generated.Diagnostics)
			continue
		}
		generated.Options.Frontend = ""
		want, _ := json.Marshal(handwritten)
		got, _ := json.Marshal(generated)
		if string(want) != string(got) {
			t.Errorf("%s: results differ between front-ends", item.name)
		}
		compared++
	}
	if compared < 50 {
		t.Fatalf("only %d programs compared end to end", compared)
	}
}

func compareParserDiagnostics(t *testing.T, name string, handwritten, generated []Diagnostic) {
	t.Helper()
	first := func(diagnostics []Diagnostic) (Diagnostic, bool) {
		for _, diagnostic := range diagnostics {
			if diagnostic.Severity == SeverityError {
				return diagnostic, true
			}
		}
		return Diagnostic{}, false
	}
	hand, handOK := first(handwritten)
	gen, genOK := first(generated)
	if handOK != genOK {
		t.Errorf("%s: only one front-end reports an error (hand=%v generated=%v)", name, handOK, genOK)
		return
	}
	if hand.Stage != gen.Stage {
		t.Errorf("%s: first error stage hand=%s generated=%s", name, hand.Stage, gen.Stage)
		return
	}
	if hand.Stage != "parser" {
		// Lexer errors are identical by construction.
		if hand.Code != gen.Code || hand.Span != gen.Span {
			t.Errorf("%s: first lexer error differs: %+v vs %+v", name, hand, gen)
		}
		return
	}
	if gen.Code != "COL2200" || !strings.HasPrefix(gen.Message, "syntax error at or near ") {
		t.Errorf("%s: unexpected generated parser diagnostic %+v (%s)", name, gen, parserDiagnosticAllowlist)
	}
	if _, allowed := parserLocationAllowlist[hand.Code]; allowed {
		return
	}
	if hand.Span.StartOffset != gen.Span.StartOffset || hand.Span.SourceID != gen.Span.SourceID {
		t.Errorf("%s: first syntax error location differs: hand %s at %d:%d, generated at %d:%d (%s)",
			name, hand.Code, hand.Span.StartLine, hand.Span.StartColumn, gen.Span.StartLine, gen.Span.StartColumn, gen.Message)
	}
}

func TestFrontendEquivalenceReferenceDumps(t *testing.T) {
	for _, item := range frontendCorpus(t) {
		hand := ReferencePipeline{}
		gen := ReferencePipeline{Frontend: FrontendGenerated}
		if a, b := hand.Tokens(item.units), gen.Tokens(item.units); a != b {
			t.Errorf("%s: reference token dumps differ", item.name)
		}
		a, aok := hand.Parser(item.units)
		b, bok := gen.Parser(item.units)
		if a != b || aok != bok {
			t.Errorf("%s: reference parser dumps differ", item.name)
		}
		x, _ := hand.SemantDump(item.units)
		y, _ := gen.SemantDump(item.units)
		if x != y {
			t.Errorf("%s: reference semant output differs", item.name)
		}
	}
}
