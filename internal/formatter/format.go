// SPDX-License-Identifier: AGPL-3.0-or-later
// Package formatter formats validated COOL tokens without reconstructing expressions.
package formatter

import (
	"fmt"
	compiler "github.com/ThalesMMS/cooler-compiler/internal/compiler"
	"strings"
	"unicode/utf8"
)

const MaxBytes = 1 << 20

type atom struct {
	text    string
	comment bool
}

// Format preserves every literal, comment and token in order, changing only trivia.
func Format(source string) (string, error) {
	if len(source) > MaxBytes || !utf8.ValidString(source) {
		return "", fmt.Errorf("format requires valid UTF-8 source of at most 1 MiB")
	}
	tokens, diagnostics := compiler.Lex(source)
	if len(diagnostics) > 0 {
		return "", fmt.Errorf("cannot format lexical errors: %s", diagnostics[0].Message)
	}
	_, diagnostics = compiler.Parse(tokens)
	if len(diagnostics) > 0 {
		return "", fmt.Errorf("cannot format syntax errors: %s", diagnostics[0].Message)
	}
	offsets := make([]int, 0, len(source)+1)
	for offset, r := range source {
		offsets = append(offsets, offset)
		if r > 0xffff {
			offsets = append(offsets, offset)
		}
	}
	offsets = append(offsets, len(source))
	atoms := []atom{}
	previous := 0
	for _, token := range tokens {
		start, end := offsets[token.Span.StartOffset], offsets[token.Span.EndOffset]
		gap := source[previous:start]
		for len(gap) > 0 {
			gap = strings.TrimLeft(gap, " \t\r\n\f\v")
			if gap == "" {
				break
			}
			length := 0
			if strings.HasPrefix(gap, "--") {
				length = strings.IndexAny(gap, "\r\n")
				if length < 0 {
					length = len(gap)
				}
			} else if strings.HasPrefix(gap, "(*") {
				depth := 1
				length = 2
				for depth > 0 && length < len(gap) {
					if strings.HasPrefix(gap[length:], "(*") {
						depth++
						length += 2
					} else if strings.HasPrefix(gap[length:], "*)") {
						depth--
						length += 2
					} else {
						length++
					}
				}
			} else {
				return "", fmt.Errorf("unsupported source trivia")
			}
			atoms = append(atoms, atom{text: gap[:length], comment: true})
			gap = gap[length:]
		}
		if end > start {
			atoms = append(atoms, atom{text: source[start:end]})
		}
		previous = end
	}
	var out strings.Builder
	indent := 0
	lineStart := true
	prior := ""
	tooLarge := false
	newline := func() {
		if !lineStart {
			out.WriteByte('\n')
			lineStart = true
		}
	}
	write := func(text string, space bool) {
		padding := 0
		if lineStart {
			padding = indent * 2
		} else if space {
			padding = 1
		}
		if out.Len()+padding+len(text)+1 > MaxBytes {
			tooLarge = true
			return
		}
		if lineStart {
			out.WriteString(strings.Repeat("  ", indent))
			lineStart = false
		} else if space {
			out.WriteByte(' ')
		}
		out.WriteString(text)
	}
	for _, item := range atoms {
		if tooLarge {
			return "", fmt.Errorf("formatted source exceeds 1 MiB")
		}
		if item.comment {
			write(item.text, true)
			newline()
			prior = ""
			continue
		}
		token := item.text
		if token == "}" {
			newline()
			if indent > 0 {
				indent--
			}
		}
		space := prior != "" && token != ";" && token != "," && token != ")" && token != "." && token != "@" && token != ":" && token != "(" && prior != "(" && prior != "." && prior != "@"
		write(token, space)
		switch token {
		case "{":
			indent++
			newline()
		case ";":
			newline()
		}
		prior = token
	}
	newline()
	if tooLarge {
		return "", fmt.Errorf("formatted source exceeds 1 MiB")
	}
	formatted := out.String()
	if len(formatted) > MaxBytes {
		return "", fmt.Errorf("formatted source exceeds 1 MiB")
	}
	// A final lexical comparison guards token fusion when trivia is normalized.
	check, errs := compiler.Lex(formatted)
	if len(errs) > 0 || len(check) != len(tokens) {
		return "", fmt.Errorf("format would change tokens")
	}
	for i := range tokens {
		if check[i].Type != tokens[i].Type || check[i].Lexeme != tokens[i].Lexeme {
			return "", fmt.Errorf("format would change token %d", i)
		}
	}
	return formatted, nil
}
