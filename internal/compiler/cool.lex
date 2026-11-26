// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Flex-style specification of the COOL lexer (the course's cool.flex),
// compiled by cooler-lexgen into lexer_generated.go. Regenerate with
// `make generate-frontend` on a development machine; nothing is generated at
// build time or on a device.
//
// Longest match wins; among equally long matches the earlier rule wins.
// Actions call the support code in lexer_generated_runtime.go, which shares
// token and diagnostic construction with the hand-written lexer.

%package compiler
%receiver g *generatedLexer
%states INITIAL COMMENT STRING
%%

// ---- INITIAL ------------------------------------------------------------
<INITIAL>[\x20\t\n\f\r\v]+    { }
<INITIAL>--[^\n\r]*           { }
<INITIAL>\(\*                 { g.beginComment() }
<INITIAL>\*\)                 { g.unmatchedCommentEnd() }
<INITIAL>"                    { g.beginString() }

<INITIAL>(?i:class)           { g.addSimple(TokenClass) }
<INITIAL>(?i:else)            { g.addSimple(TokenElse) }
<INITIAL>(?i:fi)              { g.addSimple(TokenFi) }
<INITIAL>(?i:if)              { g.addSimple(TokenIf) }
<INITIAL>(?i:in)              { g.addSimple(TokenIn) }
<INITIAL>(?i:inherits)        { g.addSimple(TokenInherits) }
<INITIAL>(?i:isvoid)          { g.addSimple(TokenIsVoid) }
<INITIAL>(?i:let)             { g.addSimple(TokenLet) }
<INITIAL>(?i:loop)            { g.addSimple(TokenLoop) }
<INITIAL>(?i:pool)            { g.addSimple(TokenPool) }
<INITIAL>(?i:then)            { g.addSimple(TokenThen) }
<INITIAL>(?i:while)           { g.addSimple(TokenWhile) }
<INITIAL>(?i:case)            { g.addSimple(TokenCase) }
<INITIAL>(?i:esac)            { g.addSimple(TokenEsac) }
<INITIAL>(?i:new)             { g.addSimple(TokenNew) }
<INITIAL>(?i:of)              { g.addSimple(TokenOf) }
<INITIAL>(?i:not)             { g.addSimple(TokenNot) }
<INITIAL>t(?i:rue)            { g.addToken(TokenBool, "true") }
<INITIAL>f(?i:alse)           { g.addToken(TokenBool, "false") }

<INITIAL>[A-Z][A-Za-z0-9_]*   { g.addToken(TokenTypeID, g.lexeme()) }
<INITIAL>[a-z][A-Za-z0-9_]*   { g.addToken(TokenObjectID, g.lexeme()) }
<INITIAL>[0-9]+               { g.finishNumber() }
<INITIAL>_[A-Za-z0-9_]*       { g.underscore() }

<INITIAL><-                   { g.addSimple(TokenAssign) }
<INITIAL><=                   { g.addSimple(TokenLessEqual) }
<INITIAL>=>                   { g.addSimple(TokenCaseArrow) }
<INITIAL>\{                   { g.addSimple(TokenLBrace) }
<INITIAL>\}                   { g.addSimple(TokenRBrace) }
<INITIAL>\(                   { g.addSimple(TokenLParen) }
<INITIAL>\)                   { g.addSimple(TokenRParen) }
<INITIAL>:                    { g.addSimple(TokenColon) }
<INITIAL>;                    { g.addSimple(TokenSemicolon) }
<INITIAL>,                    { g.addSimple(TokenComma) }
<INITIAL>\.                   { g.addSimple(TokenDot) }
<INITIAL>@                    { g.addSimple(TokenAt) }
<INITIAL>\+                   { g.addSimple(TokenPlus) }
<INITIAL>-                    { g.addSimple(TokenMinus) }
<INITIAL>\*                   { g.addSimple(TokenStar) }
<INITIAL>/                    { g.addSimple(TokenSlash) }
<INITIAL>~                    { g.addSimple(TokenTilde) }
<INITIAL><                    { g.addSimple(TokenLess) }
<INITIAL>=                    { g.addSimple(TokenEqual) }

// Any other character is invalid.
<INITIAL>(?s:.)               { g.unrecognized(g.runes[g.start]) }
<INITIAL><<INVALID>>          { g.advance(); g.invalidSourceByte() }

// ---- COMMENT: nested (* ... *) ------------------------------------------
<COMMENT>\(\*                 { g.depth++ }
<COMMENT>\*\)                 { g.endComment() }
<COMMENT>[^(*]+               { }
<COMMENT>\(                   { }
<COMMENT>\*                   { }
<COMMENT><<EOF>>              { g.commentEOF() }

// ---- STRING: after the opening quote -------------------------------------
<STRING>"                     { g.endString() }
<STRING>[\n\r]                { g.stringNewline() }
<STRING>\x00                  { g.stringNull() }
<STRING>\\(?s:.)              { g.stringEscape() }
<STRING>\\                    { }
<STRING>[^"\\\n\r\x00]        { g.stringCharacter(g.runes[g.start]) }
<STRING><<INVALID>>           { g.advance(); g.stringInvalidByte() }
<STRING><<EOF>>               { g.stringEOF() }
