// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// The generated ("spec-driven") lexer runs the DFA tables that cooler-lexgen
// compiled from cool.lex. This file is its support code, the analogue of the
// C code around a Flex scanner: the scan loop (longest match, earliest rule),
// start conditions, yyless, and the actions' token and diagnostic helpers,
// which it shares with the hand-written lexer through the embedded *lexer.

type generatedLexer struct {
	*lexer
	condition int
	depth     int
	// match is the position where the current match began (for yyless).
	match lexMark
	// mark is where the open comment or string literal began.
	mark lexMark

	stringValue      strings.Builder
	stringCharacters int
	stringFailed     bool
	stringReference  string
}

type lexMark struct {
	current, line, column, offset16 int
	previousCR                      bool
}

func (l *lexer) markHere() lexMark {
	return lexMark{current: l.current, line: l.line, column: l.column, offset16: l.offset16, previousCR: l.previousCR}
}

// LexGenerated lexes source with the generated lexer. Its tokens, spans, and
// diagnostics are those of LexSource.
func LexGenerated(sourceID, source string) ([]Token, []Diagnostic) {
	runes, invalidUTF8 := decodeSource(source)
	g := &generatedLexer{lexer: &lexer{sourceID: sourceID, runes: runes, invalidUTF8: invalidUTF8, line: 1, column: 1}}
	g.scanGenerated()
	return g.tokens, g.diagnostics
}

// LexGeneratedReference is LexReferenceSource for the generated lexer.
func LexGeneratedReference(sourceID, source string) []Token {
	runes, invalidUTF8 := decodeSource(source)
	g := &generatedLexer{lexer: &lexer{sourceID: sourceID, runes: runes, invalidUTF8: invalidUTF8, line: 1, column: 1, reference: true, rawBytes: rawInvalidBytes(source)}}
	g.scanGenerated()
	return g.tokens
}

func rawInvalidBytes(source string) map[int]byte {
	raw := map[int]byte{}
	index, offset := 0, 0
	for offset < len(source) {
		r, size := utf8.DecodeRuneInString(source[offset:])
		if r == utf8.RuneError && size == 1 {
			raw[index] = source[offset]
		}
		offset += size
		index++
	}
	return raw
}

func genlexClass(r rune) int {
	return sort.Search(len(genlexBounds), func(i int) bool { return genlexBounds[i] > r }) - 1
}

func (g *generatedLexer) scanGenerated() {
	for {
		if g.atEnd() {
			g.genlexEofAction(g.condition)
			break
		}
		g.start, g.startLine, g.startColumn, g.startOffset = g.current, g.line, g.column, g.offset16
		g.match = g.markHere()
		if g.invalidUTF8[g.current] && g.genlexInvalidAction(g.condition) {
			continue
		}
		state, rule, length := genlexStart[g.condition], -1, 0
		for index := g.current; index < len(g.runes); index++ {
			next := genlexDelta[state*genlexClasses+genlexClass(g.runes[index])]
			if next < 0 {
				break
			}
			state = int(next)
			if accepted := genlexAccept[state]; accepted >= 0 {
				rule, length = int(accepted), index-g.current+1
			}
		}
		if rule < 0 {
			// The specification's catch-all rules make this unreachable;
			// treat it as an invalid character rather than looping.
			c := g.advance()
			g.unrecognized(c)
			continue
		}
		for ; length > 0; length-- {
			g.advance()
		}
		g.genlexAction(rule)
	}
	span := Span{
		SourceID:  g.sourceID,
		StartLine: g.line, StartColumn: g.column,
		EndLine: g.line, EndColumn: g.column,
		StartOffset: g.offset16, EndOffset: g.offset16,
	}
	g.tokens = append(g.tokens, Token{Type: TokenEOF, Line: g.line, Column: g.column, Offset: g.offset16, Span: span})
}

// yyless keeps the first n runes of the current match and returns the rest
// to the input, like Flex's yyless.
func (g *generatedLexer) yyless(n int) {
	g.current, g.line, g.column, g.offset16, g.previousCR = g.match.current, g.match.line, g.match.column, g.match.offset16, g.match.previousCR
	for ; n > 0; n-- {
		g.advance()
	}
}

// restoreStart makes the open comment or string the current lexeme, so its
// diagnostics and token span cover it from its first character.
func (g *generatedLexer) restoreStart() {
	g.start, g.startLine, g.startColumn, g.startOffset = g.mark.current, g.mark.line, g.mark.column, g.mark.offset16
}

func (g *generatedLexer) lexeme() string { return string(g.runes[g.start:g.current]) }

func (g *generatedLexer) underscore() {
	if g.reference {
		// cool.flex has no rule for '_': one invalid character, then resume.
		g.yyless(1)
		g.refError("_")
		return
	}
	g.leadingUnderscore()
}

func (g *generatedLexer) beginComment() {
	g.mark = g.match
	g.depth = 1
	g.condition = genlexCOMMENT
}

func (g *generatedLexer) endComment() {
	g.depth--
	if g.depth == 0 {
		g.condition = genlexINITIAL
	}
}

func (g *generatedLexer) commentEOF() {
	g.restoreStart()
	g.unterminatedComment()
}

func (g *generatedLexer) beginString() {
	g.mark = g.match
	g.stringValue.Reset()
	g.stringCharacters, g.stringFailed, g.stringReference = 0, false, ""
	g.condition = genlexSTRING
}

func (g *generatedLexer) stringFail(code, message, hint string, span Span) {
	if g.stringFailed {
		return
	}
	g.stringFailed = true
	if g.stringReference == "" {
		g.stringReference = referenceLexerMessage(code)
	}
	g.addError(code, message, hint, span)
}

func (g *generatedLexer) stringAppend(r rune, span Span) {
	if g.stringFailed {
		return
	}
	if g.reference {
		g.stringCharacters += utf8.RuneLen(r)
	} else {
		g.stringCharacters++
	}
	if g.stringCharacters > maxCoolStringCharacters {
		g.stringFail("COL1006", "string constant is too long", "Keep string constants at or below 1024 characters.", span)
		return
	}
	g.stringValue.WriteRune(r)
}

func (g *generatedLexer) stringCharacter(r rune) { g.stringAppend(r, g.currentSpan()) }

func (g *generatedLexer) endString() {
	g.restoreStart()
	if !g.stringFailed {
		g.addToken(TokenString, g.stringValue.String())
	} else {
		g.refError(g.stringReference)
	}
	g.condition = genlexINITIAL
}

func (g *generatedLexer) stringNewline() {
	if g.reference && g.runes[g.start] == '\r' {
		// Only '\n' ends a line for the course lexer.
		g.stringAppend('\r', g.currentSpan())
		return
	}
	g.restoreStart()
	if !g.stringFailed {
		g.stringReference = "Unterminated string constant"
	}
	g.stringFail("COL1002", "unterminated string constant", "Keep the string on one line or escape the newline with a backslash.", g.currentSpan())
	g.refError(g.stringReference)
	g.condition = genlexINITIAL
}

func (g *generatedLexer) stringNull() {
	g.stringFail("COL1005", "string contains a null character", "Remove the null character from the string.", g.currentSpan())
}

func (g *generatedLexer) stringInvalidByte() {
	if g.reference {
		// Bytes are ordinary string characters for the course lexer.
		if !g.stringFailed {
			g.stringCharacters++
			if g.stringCharacters > maxCoolStringCharacters {
				g.stringFail("COL1006", "string constant is too long", "Keep string constants at or below 1024 characters.", g.currentSpan())
				return
			}
			g.stringValue.WriteByte(g.rawBytes[g.start])
		}
		return
	}
	g.stringFail("COL1008", "string contains malformed UTF-8", "Save COOL source as valid UTF-8.", g.currentSpan())
}

func (g *generatedLexer) stringEscape() {
	escapeIndex := g.current - 1
	escaped := g.runes[escapeIndex]
	if g.invalidUTF8[escapeIndex] {
		if g.reference {
			if !g.stringFailed {
				g.stringCharacters++
				g.stringValue.WriteByte(g.rawBytes[escapeIndex])
			}
			return
		}
		g.stringFail("COL1008", "string escape contains malformed UTF-8", "Save COOL source as valid UTF-8.", g.currentSpan())
		return
	}
	switch escaped {
	case 'b':
		g.stringAppend('\b', g.currentSpan())
	case 't':
		g.stringAppend('\t', g.currentSpan())
	case 'n', '\n':
		g.stringAppend('\n', g.currentSpan())
	case 'f':
		g.stringAppend('\f', g.currentSpan())
	case '\r':
		if g.reference {
			g.stringAppend('\r', g.currentSpan())
			return
		}
		// An escaped CRLF is one escaped line break (Flex's input()).
		if g.peek() == '\n' {
			g.advance()
		}
		g.stringAppend('\n', g.currentSpan())
	case 0:
		g.stringFail("COL1010", "string contains an escaped null character", "Remove the escaped null character from the string.", g.currentSpan())
	default:
		// COOL treats \c as c for otherwise unrecognized escapes.
		g.stringAppend(escaped, g.currentSpan())
	}
}

func (g *generatedLexer) stringEOF() {
	g.restoreStart()
	if !g.stringFailed {
		g.stringReference = "EOF in string constant"
	}
	g.stringFail("COL1002", "unterminated string constant", "Add a closing double quote.", g.currentSpan())
	g.refError(g.stringReference)
}
