// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"os"
	"testing"

	"github.com/ThalesMMS/cooler-compiler/internal/lexgen"
)

func assertSameLexing(t *testing.T, name, source string) {
	t.Helper()
	handTokens, handDiagnostics := LexSource("u", source)
	generatedTokens, generatedDiagnostics := LexGenerated("u", source)
	want, _ := json.Marshal([]any{handTokens, handDiagnostics})
	got, _ := json.Marshal([]any{generatedTokens, generatedDiagnostics})
	if !bytes.Equal(want, got) {
		t.Fatalf("%s: generated lexer differs for %q\nhand:      %s\ngenerated: %s", name, source, want, got)
	}
	handReference, _ := json.Marshal(LexReferenceSource("u", source))
	generatedReference, _ := json.Marshal(LexGeneratedReference("u", source))
	if !bytes.Equal(handReference, generatedReference) {
		t.Fatalf("%s: reference-mode streams differ for %q\nhand:      %s\ngenerated: %s", name, source, handReference, generatedReference)
	}
}

func TestGeneratedLexerMatchesHandwrittenOnCorpus(t *testing.T) {
	for _, item := range frontendCorpus(t) {
		assertSameLexing(t, item.name, item.units[0].Content)
	}
}

func TestGeneratedLexerEdgeCases(t *testing.T) {
	for _, source := range []string{
		"", "class CLASS ClAsS classy", "true True tRUE fALSE false", "_ __x _1 a_b",
		"(* a (* nested *) still *) x", "(*)", "(* **) *)", "(* unterminated", "*) x",
		"-- comment\r\nx", "--", "a-b--c", "<- <= => < = <<-",
		"\"esc \\b\\t\\n\\f\\q\\\\\\\"\"", "\"line\\\ncontinued\"", "\"crlf\\\r\nx\"", "\"cr\\\rx\"",
		"\"open\nx", "\"open\r\nx", "\"eof", "\"slash at eof\\", "\"nul\x00x\"", "\"esc nul\\\x00\"",
		"\"bad \xff byte\" \xfe x", "\"esc bad \\\xff\"", "é   � # $ [ ] ? !",
		"\"" + string(bytes.Repeat([]byte("a"), 1025)) + "\"", "\"" + string(bytes.Repeat([]byte("é"), 600)) + "\"",
		"007 2147483647 2147483648 99999999999999999999", "a\rb\r\nc\n\rd", "\x00 \x0b \x0c",
	} {
		assertSameLexing(t, "edge", source)
	}
}

func TestGeneratedLexerRandomInputs(t *testing.T) {
	alphabet := []string{"a", "Z", "_", "0", "9", " ", "\n", "\r", "\t", "\"", "\\", "(", ")", "*", "-", "<", "=", ">",
		"{", "}", ";", ":", ".", "@", "~", "/", "+", ",", "class", "true", "fALSE", "(*", "*)", "--", "\x00", "\xff", "é", "#"}
	random := rand.New(rand.NewSource(20261125))
	for iteration := 0; iteration < 4000; iteration++ {
		var source bytes.Buffer
		for length := random.Intn(24); length >= 0; length-- {
			source.WriteString(alphabet[random.Intn(len(alphabet))])
		}
		assertSameLexing(t, "random", source.String())
	}
}

// TestGeneratedLexerIsUpToDate fails when cool.lex or the generator changed
// without regenerating lexer_generated.go (make generate-frontend).
func TestGeneratedLexerIsUpToDate(t *testing.T) {
	spec, err := os.ReadFile("cool.lex")
	if err != nil {
		t.Fatal(err)
	}
	generated, err := lexgen.Generate(spec, "cool.lex")
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile("lexer_generated.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, committed) {
		t.Fatal("lexer_generated.go is stale; run make generate-frontend")
	}
}
