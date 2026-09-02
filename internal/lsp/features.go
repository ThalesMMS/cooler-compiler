// SPDX-License-Identifier: AGPL-3.0-or-later

package lsp

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	publiccompiler "github.com/ThalesMMS/cooler-compiler/compiler"
)

func (s *Server) requestDocumentSymbols(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var params struct {
		TextDocument TextDocumentIdentifier `json:"textDocument"`
	}
	if err := decodeParams(raw, &params); err != nil {
		return nil, invalidParams(err)
	}
	value, rpcErr := s.analysisForRequest(ctx)
	if rpcErr != nil {
		return nil, rpcErr
	}
	if _, exists := value.Documents[params.TextDocument.URI]; !exists {
		return []documentSymbol{}, nil
	}
	return value.Index.documentSymbols(params.TextDocument.URI), nil
}

func (s *Server) requestWorkspaceSymbols(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var params struct {
		Query string `json:"query"`
	}
	if err := decodeParams(raw, &params); err != nil {
		return nil, invalidParams(err)
	}
	value, rpcErr := s.analysisForRequest(ctx)
	if rpcErr != nil {
		return nil, rpcErr
	}
	query := strings.ToLower(strings.TrimSpace(params.Query))
	result := []symbolInformation{}
	for _, decl := range value.Index.Declarations {
		if decl.Builtin || decl.URI == "" || query != "" && !strings.Contains(strings.ToLower(decl.Name), query) {
			continue
		}
		result = append(result, symbolInformation{Name: decl.Name, Kind: symbolKind(decl.Kind), Location: Location{URI: decl.URI, Range: decl.Selection}, ContainerName: decl.Owner})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		if result[i].Location.URI != result[j].Location.URI {
			return result[i].Location.URI < result[j].Location.URI
		}
		return comparePosition(result[i].Location.Range.Start, result[j].Location.Range.Start) < 0
	})
	return result, nil
}

func (s *Server) requestHover(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var params TextDocumentPositionParams
	if err := decodeParams(raw, &params); err != nil {
		return nil, invalidParams(err)
	}
	value, rpcErr := s.analysisForRequest(ctx)
	if rpcErr != nil {
		return nil, rpcErr
	}
	index := value.Index
	if item := index.occurrenceAt(params.TextDocument.URI, params.Position); item != nil {
		parts := []string{}
		label := item.Kind + " " + item.Name
		if item.Type != "" {
			label += " : " + item.Type
		}
		parts = append(parts, "```cool\n"+label+"\n```")
		if decl := index.ByID[item.DeclarationID]; decl != nil {
			if decl.Type != "" && decl.Type != item.Type {
				parts = append(parts, "Declaration type: `"+decl.Type+"`.")
			}
			if decl.Owner != "" {
				parts = append(parts, "Declared by `"+decl.Owner+"`.")
			}
			if item.Kind == "method" || item.Kind == "builtin method" {
				parts = append(parts, "Effective dispatch target: `"+decl.Owner+"."+decl.Name+"`.")
			}
			if strings.Contains(decl.Type, "SELF_TYPE") {
				contextClass := decl.Owner
				if node := index.contextAt(params.TextDocument.URI, params.Position); node != nil && node.Class != "" {
					contextClass = node.Class
				}
				parts = append(parts, "`SELF_TYPE` is substituted in the receiver context `"+contextClass+"`.")
			}
			if decl.Builtin {
				if documentation := builtinDocumentation[decl.Owner+"."+decl.Name]; documentation != "" {
					parts = append(parts, documentation)
				}
			}
		}
		if node := index.contextAt(params.TextDocument.URI, params.Position); node != nil {
			if node.Node.Type != "" {
				parts = append(parts, "Static type: `"+node.Node.Type+"`.")
			}
			if strings.Contains(item.Type, "SELF_TYPE") || node.Node.Type == "SELF_TYPE" {
				parts = append(parts, "Here `SELF_TYPE` is substituted in the context of `"+node.Class+"`.")
			}
		}
		return hover{Contents: markupContent{Kind: "markdown", Value: strings.Join(parts, "\n\n")}, Range: &item.Range}, nil
	}
	if token := index.tokenAt(params.TextDocument.URI, params.Position); token != nil {
		kind := strings.ToLower(strings.ReplaceAll(string(token.Type), "_", " "))
		parts := []string{"```cool\n" + token.Lexeme + "\n```", "Token kind: `" + kind + "`."}
		if node := index.contextAt(params.TextDocument.URI, params.Position); node != nil && node.Node.Type != "" {
			parts = append(parts, "Static type: `"+node.Node.Type+"`.")
		}
		tokenRange := index.rangeFromSpan(token.Span)
		return hover{Contents: markupContent{Kind: "markdown", Value: strings.Join(parts, "\n\n")}, Range: &tokenRange}, nil
	}
	return nil, nil
}

func (s *Server) requestDefinition(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var params TextDocumentPositionParams
	if err := decodeParams(raw, &params); err != nil {
		return nil, invalidParams(err)
	}
	value, rpcErr := s.analysisForRequest(ctx)
	if rpcErr != nil {
		return nil, rpcErr
	}
	item := value.Index.occurrenceAt(params.TextDocument.URI, params.Position)
	if item == nil {
		return nil, nil
	}
	decl := value.Index.ByID[item.DeclarationID]
	if decl == nil || decl.Builtin || decl.URI == "" {
		return nil, nil
	}
	return Location{URI: decl.URI, Range: decl.Selection}, nil
}

func (s *Server) requestReferences(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var params struct {
		TextDocument TextDocumentIdentifier `json:"textDocument"`
		Position     Position               `json:"position"`
		Context      struct {
			IncludeDeclaration bool `json:"includeDeclaration"`
		} `json:"context"`
	}
	if err := decodeParams(raw, &params); err != nil {
		return nil, invalidParams(err)
	}
	value, rpcErr := s.analysisForRequest(ctx)
	if rpcErr != nil {
		return nil, rpcErr
	}
	item := value.Index.occurrenceAt(params.TextDocument.URI, params.Position)
	if item == nil || item.DeclarationID == "" {
		return []Location{}, nil
	}
	locations := append([]Location(nil), value.Index.References[item.DeclarationID]...)
	if params.Context.IncludeDeclaration {
		if decl := value.Index.ByID[item.DeclarationID]; decl != nil && !decl.Builtin && decl.URI != "" {
			locations = append([]Location{{URI: decl.URI, Range: decl.Selection}}, locations...)
		}
	}
	return locations, nil
}

func (s *Server) requestCompletion(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var params TextDocumentPositionParams
	if err := decodeParams(raw, &params); err != nil {
		return nil, invalidParams(err)
	}
	value, rpcErr := s.analysisForRequest(ctx)
	if rpcErr != nil {
		return nil, rpcErr
	}
	doc, exists := value.Documents[params.TextDocument.URI]
	if !exists {
		return []completionItem{}, nil
	}
	offset, err := byteOffsetAt(doc.Text, params.Position)
	if err != nil {
		return nil, invalidParams(err)
	}
	prefix := doc.Text[:offset]
	return value.Index.completions(params.TextDocument.URI, params.Position, prefix), nil
}

func (s *Server) requestSemanticTokens(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var params struct {
		TextDocument TextDocumentIdentifier `json:"textDocument"`
	}
	if err := decodeParams(raw, &params); err != nil {
		return nil, invalidParams(err)
	}
	value, rpcErr := s.analysisForRequest(ctx)
	if rpcErr != nil {
		return nil, rpcErr
	}
	doc, exists := value.Documents[params.TextDocument.URI]
	if !exists {
		return semanticTokens{Data: []uint32{}}, nil
	}
	return value.Index.semanticTokens(params.TextDocument.URI, doc.Text), nil
}

func (s *Server) requestCodeActions(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var params codeActionParams
	if err := decodeParams(raw, &params); err != nil {
		return nil, invalidParams(err)
	}
	value, rpcErr := s.analysisForRequest(ctx)
	if rpcErr != nil {
		return nil, rpcErr
	}
	actions := []codeAction{}
	for _, item := range value.Report.Diagnostics {
		uri := item.Span.SourceID
		if uri == "" && len(value.Report.SourceUnits) == 1 {
			uri = value.Report.SourceUnits[0].ID
		}
		if uri != params.TextDocument.URI || !rangesOverlap(rangeForSpan(value.Documents, uri, item.Span), params.Range) {
			continue
		}
		converted := lspDiagnostic(value.Documents, uri, item)
		for _, fix := range item.QuickFix {
			fixURI := fix.Span.SourceID
			if fixURI == "" {
				fixURI = uri
			}
			if fixURI != uri || fix.Span.StartLine < 1 || fix.Span.EndLine < fix.Span.StartLine {
				continue
			}
			actions = append(actions, codeAction{
				Title: fix.Title, Kind: "quickfix", Diagnostics: []diagnostic{converted}, IsPreferred: len(item.QuickFix) == 1,
				Edit: &workspaceEdit{Changes: map[string][]textEdit{fixURI: {{Range: rangeForSpan(value.Documents, fixURI, fix.Span), NewText: fix.Replacement}}}},
			})
		}
	}
	return actions, nil
}

func invalidParams(err error) *rpcError {
	return &rpcError{Code: jsonRPCInvalidParams, Message: err.Error()}
}

func rangesOverlap(left, right Range) bool {
	return comparePosition(left.Start, right.End) <= 0 && comparePosition(right.Start, left.End) <= 0
}

func (index *semanticIndex) documentSymbols(uri string) []documentSymbol {
	classes := []*declaration{}
	for _, decl := range index.Declarations {
		if !decl.Builtin && decl.URI == uri && decl.Kind == "class" {
			classes = append(classes, decl)
		}
	}
	sortDeclarations(classes)
	result := make([]documentSymbol, 0, len(classes))
	for _, class := range classes {
		classSymbol := documentSymbol{Name: class.Name, Detail: "inherits " + index.Parents[class.Name], Kind: symbolKind(class.Kind), Range: class.Full, SelectionRange: class.Selection}
		features := []*declaration{}
		for _, decl := range index.Declarations {
			if !decl.Builtin && decl.URI == uri && decl.Owner == class.Name && (decl.Kind == "method" || decl.Kind == "attribute") {
				features = append(features, decl)
			}
		}
		sortDeclarations(features)
		for _, feature := range features {
			child := documentSymbol{Name: feature.Name, Detail: feature.Type, Kind: symbolKind(feature.Kind), Range: feature.Full, SelectionRange: feature.Selection}
			if feature.Kind == "method" {
				locals := []*declaration{}
				methodScope := class.Name + "." + feature.Name
				for _, decl := range index.Declarations {
					if !decl.Builtin && decl.URI == uri && decl.Owner == class.Name && (decl.Scope == methodScope || strings.HasPrefix(decl.Scope, methodScope+"/")) && decl.Kind != "method" {
						locals = append(locals, decl)
					}
				}
				sortDeclarations(locals)
				for _, local := range locals {
					child.Children = append(child.Children, documentSymbol{Name: local.Name, Detail: local.Type, Kind: symbolKind(local.Kind), Range: local.Full, SelectionRange: local.Selection})
				}
			}
			classSymbol.Children = append(classSymbol.Children, child)
		}
		result = append(result, classSymbol)
	}
	return result
}

func sortDeclarations(values []*declaration) {
	sort.Slice(values, func(i, j int) bool {
		if comparison := comparePosition(values[i].Selection.Start, values[j].Selection.Start); comparison != 0 {
			return comparison < 0
		}
		return values[i].Name < values[j].Name
	})
}

func symbolKind(kind string) int {
	switch kind {
	case "class":
		return 5
	case "method", "builtin method":
		return 6
	case "attribute":
		return 8
	case "formal", "let", "case":
		return 13
	default:
		return 13
	}
}

var wordBeforeDot = regexp.MustCompile(`([A-Za-z][A-Za-z0-9_]*)\.\s*[A-Za-z0-9_]*$`)
var wordBeforeAt = regexp.MustCompile(`([A-Za-z][A-Za-z0-9_]*)@\s*[A-Za-z0-9_]*$`)
var inheritsContext = regexp.MustCompile(`(?i)\binherits\s+[A-Za-z0-9_]*$`)
var completionPrefix = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_]*$`)

func (index *semanticIndex) completions(uri string, position Position, sourcePrefix string) []completionItem {
	node := index.contextAt(uri, position)
	currentClass, currentMethod := "", ""
	if node != nil {
		currentClass, currentMethod = node.Class, node.Method
	}
	prefix := completionPrefix.FindString(sourcePrefix)
	items := map[string]completionItem{}
	add := func(item completionItem) {
		if prefix != "" && !strings.HasPrefix(strings.ToLower(item.Label), strings.ToLower(prefix)) {
			return
		}
		if existing, ok := items[item.Label]; !ok || item.SortText < existing.SortText {
			items[item.Label] = item
		}
	}
	if inheritsContext.MatchString(sourcePrefix) {
		for name, candidates := range index.Classes {
			if name == currentClass || name == "Int" || name == "String" || name == "Bool" || len(candidates) == 0 {
				continue
			}
			add(completionItem{Label: name, Kind: 7, Detail: "class", SortText: "1-" + name})
		}
		return sortedCompletions(items)
	}
	if match := wordBeforeDot.FindStringSubmatch(sourcePrefix); len(match) == 2 {
		receiverType := index.visibleType(match[1], currentClass, currentMethod, uri, position)
		for _, method := range index.methodsFor(receiverType) {
			add(completionForDeclaration(method, "1-"))
		}
		return sortedCompletions(items)
	}
	if match := wordBeforeAt.FindStringSubmatch(sourcePrefix); len(match) == 2 {
		receiverType := index.visibleType(match[1], currentClass, currentMethod, uri, position)
		visited := map[string]bool{}
		for receiverType != "" && !visited[receiverType] {
			visited[receiverType] = true
			if receiverType != currentClass {
				add(completionItem{Label: receiverType, Kind: 7, Detail: "ancestor class", SortText: "1-" + receiverType})
			}
			receiverType = index.Parents[receiverType]
		}
		return sortedCompletions(items)
	}
	for _, keyword := range []string{"class", "else", "fi", "if", "in", "inherits", "isvoid", "let", "loop", "pool", "then", "while", "case", "esac", "new", "of", "not", "true", "false"} {
		add(completionItem{Label: keyword, Kind: 14, Detail: "COOL keyword", SortText: "4-" + keyword})
	}
	for _, snippet := range []struct{ label, insert string }{{"if … then … else … fi", "if ${1:condition} then ${2:value} else ${3:value} fi"}, {"let … in …", "let ${1:name} : ${2:Type} <- ${3:value} in ${4:body}"}, {"case … of … esac", "case ${1:value} of\n  ${2:name} : ${3:Type} => ${4:value};\nesac"}} {
		add(completionItem{Label: snippet.label, Kind: 15, Detail: "expression form", InsertText: snippet.insert, InsertTextFormat: 2, SortText: "5-" + snippet.label})
	}
	for _, decl := range index.visibleDeclarations(currentClass, currentMethod, uri, position) {
		add(completionForDeclaration(decl, "1-"))
	}
	for name, candidates := range index.Classes {
		if len(candidates) > 0 {
			add(completionItem{Label: name, Kind: 7, Detail: "class", SortText: "3-" + name})
		}
	}
	for _, method := range index.methodsFor(currentClass) {
		add(completionForDeclaration(method, "2-"))
	}
	return sortedCompletions(items)
}

func completionForDeclaration(decl *declaration, prefix string) completionItem {
	kind := 6
	switch decl.Kind {
	case "method", "builtin method":
		kind = 2
	case "class":
		kind = 7
	case "attribute":
		kind = 5
	case "formal", "let", "case":
		kind = 6
	}
	documentation := any(nil)
	if decl.Builtin {
		documentation = builtinDocumentation[decl.Owner+"."+decl.Name]
	}
	return completionItem{Label: decl.Name, Kind: kind, Detail: decl.Type, Documentation: documentation, SortText: prefix + decl.Name}
}

func sortedCompletions(values map[string]completionItem) []completionItem {
	result := make([]completionItem, 0, len(values))
	for _, item := range values {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].SortText != result[j].SortText {
			return result[i].SortText < result[j].SortText
		}
		return result[i].Label < result[j].Label
	})
	return result
}

func (index *semanticIndex) visibleType(name, class, method, uri string, position Position) string {
	if name == "self" {
		return class
	}
	for _, decl := range index.visibleDeclarations(class, method, uri, position) {
		if decl.Name == name {
			if decl.Type == "SELF_TYPE" {
				return class
			}
			return decl.Type
		}
	}
	if index.resolveClass(name) != nil {
		return name
	}
	return class
}

func (index *semanticIndex) visibleDeclarations(class, method, uri string, position Position) []*declaration {
	result := []*declaration{}
	for _, decl := range index.Declarations {
		if decl.Builtin || decl.URI != uri || decl.Owner != class || decl.Kind == "class" || decl.Kind == "method" || decl.Kind == "attribute" {
			continue
		}
		if method != "" && !strings.HasPrefix(decl.Scope, class+"."+method) {
			continue
		}
		if comparePosition(decl.Selection.Start, position) <= 0 && positionInRange(position, decl.Visible) {
			result = append(result, decl)
		}
	}
	result = append(result, index.attributesFor(class)...)
	sort.SliceStable(result, func(i, j int) bool { return comparePosition(result[i].Selection.Start, result[j].Selection.Start) > 0 })
	return result
}

type semanticItem struct{ line, character, length, tokenType, modifiers int }

func (index *semanticIndex) semanticTokens(uri, text string) semanticTokens {
	items := []semanticItem{}
	for _, token := range index.Tokens[uri] {
		if string(token.Type) == "EOF" || token.Span.StartLine != token.Span.EndLine {
			continue
		}
		tokenType := semanticTypeForToken(token)
		modifiers := 0
		tokenRange := index.rangeFromSpan(token.Span)
		if occurrence := index.occurrenceExact(uri, tokenRange); occurrence != nil {
			tokenType = semanticTypeForOccurrence(*occurrence)
			if occurrence.Declaration {
				modifiers |= 1 << 0
			}
			if !occurrence.Mutable {
				modifiers |= 1 << 2
			}
			if occurrence.Builtin {
				modifiers |= 1 << 4
			}
		}
		if tokenType < 0 {
			continue
		}
		length := tokenRange.End.Character - tokenRange.Start.Character
		if length <= 0 {
			length = utf16Length(token.Lexeme)
		}
		items = append(items, semanticItem{line: tokenRange.Start.Line, character: tokenRange.Start.Character, length: length, tokenType: tokenType, modifiers: modifiers})
	}
	items = append(items, commentSemanticItems(text)...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].line != items[j].line {
			return items[i].line < items[j].line
		}
		return items[i].character < items[j].character
	})
	data := make([]uint32, 0, len(items)*5)
	lastLine, lastCharacter := 0, 0
	for index, item := range items {
		lineDelta := item.line - lastLine
		characterDelta := item.character
		if index > 0 && lineDelta == 0 {
			characterDelta = item.character - lastCharacter
		}
		if lineDelta < 0 || characterDelta < 0 || item.length <= 0 {
			continue
		}
		data = append(data, uint32(lineDelta), uint32(characterDelta), uint32(item.length), uint32(item.tokenType), uint32(item.modifiers))
		lastLine, lastCharacter = item.line, item.character
	}
	return semanticTokens{Data: data}
}

func semanticTypeForOccurrence(item occurrence) int {
	switch item.Kind {
	case "class":
		return 0
	case "method", "builtin method":
		return 1
	case "attribute":
		return 2
	case "formal":
		return 3
	case "let", "case", "variable":
		return 4
	default:
		return 4
	}
}

func semanticTypeForToken(token publiccompiler.Token) int {
	typeName := string(token.Type)
	switch typeName {
	case "TYPE_ID":
		return 5
	case "STR_CONST":
		return 7
	case "INT_CONST":
		return 8
	case "OBJECT_ID":
		return 4
	case "ERROR", "EOF":
		return -1
	}
	if tokenIsKeyword(typeName) {
		return 6
	}
	if strings.HasSuffix(typeName, "CONST") || typeName == "TRUE" || typeName == "FALSE" {
		return 8
	}
	return 10
}

func tokenIsKeyword(value string) bool {
	switch value {
	case "CLASS", "ELSE", "FI", "IF", "IN", "INHERITS", "ISVOID", "LET", "LOOP", "POOL", "THEN", "WHILE", "CASE", "ESAC", "NEW", "OF", "NOT", "BOOL_CONST":
		return true
	}
	return false
}

func commentSemanticItems(text string) []semanticItem {
	result := []semanticItem{}
	line, column := 0, 0
	for offset := 0; offset < len(text); {
		if strings.HasPrefix(text[offset:], "--") {
			startLine, startColumn := line, column
			length := 0
			for offset < len(text) && text[offset] != '\n' {
				r, size := utf8.DecodeRuneInString(text[offset:])
				offset += size
				width := 1
				if r > 0xFFFF {
					width = 2
				}
				column += width
				length += width
			}
			result = append(result, semanticItem{line: startLine, character: startColumn, length: length, tokenType: 9})
			continue
		}
		if strings.HasPrefix(text[offset:], "(*") {
			depth := 0
			segmentLine, segmentColumn, segmentLength := line, column, 0
			for offset < len(text) {
				if strings.HasPrefix(text[offset:], "(*") {
					depth++
					offset += 2
					column += 2
					segmentLength += 2
					continue
				}
				if strings.HasPrefix(text[offset:], "*)") {
					depth--
					offset += 2
					column += 2
					segmentLength += 2
					if depth == 0 {
						break
					}
					continue
				}
				r, size := utf8.DecodeRuneInString(text[offset:])
				offset += size
				if r == '\n' {
					if segmentLength > 0 {
						result = append(result, semanticItem{line: segmentLine, character: segmentColumn, length: segmentLength, tokenType: 9})
					}
					line++
					column = 0
					segmentLine, segmentColumn, segmentLength = line, 0, 0
				} else {
					width := 1
					if r > 0xFFFF {
						width = 2
					}
					column += width
					segmentLength += width
				}
			}
			if segmentLength > 0 {
				result = append(result, semanticItem{line: segmentLine, character: segmentColumn, length: segmentLength, tokenType: 9})
			}
			continue
		}
		r, size := utf8.DecodeRuneInString(text[offset:])
		if r == '"' {
			offset += size
			column++
			for offset < len(text) {
				r, size = utf8.DecodeRuneInString(text[offset:])
				offset += size
				if r == '\\' && offset < len(text) {
					escapedRune, escaped := utf8.DecodeRuneInString(text[offset:])
					offset += escaped
					column++
					if escapedRune == '\n' {
						line++
						column = 0
					} else if escapedRune > 0xFFFF {
						column += 2
					} else {
						column++
					}
					continue
				}
				if r == '"' {
					column++
					break
				}
				if r == '\n' {
					line++
					column = 0
				} else {
					if r > 0xFFFF {
						column += 2
					} else {
						column++
					}
				}
			}
			continue
		}
		offset += size
		if r == '\n' {
			line++
			column = 0
		} else if r > 0xFFFF {
			column += 2
		} else {
			column++
		}
	}
	return result
}

var builtinDocumentation = map[string]string{
	"Object.abort":     "Terminates execution with a COOL runtime diagnostic.",
	"Object.type_name": "Returns the receiver's dynamic class name.",
	"Object.copy":      "Returns a shallow copy while preserving the dynamic type.",
	"IO.out_string":    "Writes a string and returns `self`.", "IO.out_int": "Writes a signed 32-bit integer and returns `self`.",
	"IO.in_string": "Reads one input line, or the deterministic empty-string EOF fallback.", "IO.in_int": "Reads one signed 32-bit integer, returning zero for invalid input.",
	"String.length": "Returns the string length in Unicode code points.", "String.concat": "Returns the concatenation of receiver and argument.",
	"String.substr": "Returns a bounds-checked Unicode substring.",
}
