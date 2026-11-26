// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"errors"
	"strings"
)

// Front-end selections. The hand-written lexer and recursive-descent parser
// are the default; the generated front-end uses the spec-driven lexer and the
// goyacc LALR(1) parser, whose Go sources are generated at build time and
// committed. No generator runs at compile time or on a device.
const (
	FrontendHandwritten = "handwritten"
	FrontendGenerated   = "generated"
)

// NormalizeFrontend maps a requested front-end to its canonical option value:
// "" (the hand-written default) or FrontendGenerated.
func NormalizeFrontend(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", FrontendHandwritten:
		return "", nil
	case FrontendGenerated:
		return FrontendGenerated, nil
	}
	return "", errors.New("expected handwritten or generated")
}

// lexForFrontend lexes one unit with the selected front-end's lexer.
func lexForFrontend(frontend, sourceID, source string) ([]Token, []Diagnostic) {
	if frontend == FrontendGenerated {
		return LexGenerated(sourceID, source)
	}
	return LexSource(sourceID, source)
}

// parseForFrontend parses one unit with the selected front-end's parser.
func parseForFrontend(frontend string, tokens []Token) (*Program, []Diagnostic) {
	if frontend == FrontendGenerated {
		return ParseLALR(tokens)
	}
	return Parse(tokens)
}
