// SPDX-License-Identifier: AGPL-3.0-or-later

package lsp

import (
	"fmt"
	"sort"
	"strings"

	publiccompiler "github.com/ThalesMMS/cooler-compiler/compiler"
)

type declaration struct {
	ID        string
	Name      string
	Kind      string
	Type      string
	Owner     string
	Scope     string
	URI       string
	Selection Range
	Full      Range
	Visible   Range
	Mutable   bool
	Builtin   bool
}

type occurrence struct {
	Name          string
	Kind          string
	Type          string
	URI           string
	Range         Range
	DeclarationID string
	Declaration   bool
	Mutable       bool
	Builtin       bool
}

type nodeRef struct {
	Node   *publiccompiler.ASTNode
	URI    string
	Range  Range
	Class  string
	Method string
	Depth  int
}

type semanticIndex struct {
	Declarations   []*declaration
	ByID           map[string]*declaration
	Occurrences    map[string][]occurrence
	References     map[string][]Location
	Classes        map[string][]*declaration
	Methods        map[string]map[string][]*declaration
	Attributes     map[string]map[string][]*declaration
	Parents        map[string]string
	Nodes          map[string][]nodeRef
	Tokens         map[string][]publiccompiler.Token
	TokensByLexeme map[string]map[string][]int
	Documents      map[string]documentSnapshot
	Ranges         map[publiccompiler.Span]Range
	seen           map[string]struct{}
}

func newSemanticIndex() *semanticIndex {
	index := &semanticIndex{
		ByID: map[string]*declaration{}, Occurrences: map[string][]occurrence{}, References: map[string][]Location{},
		Classes: map[string][]*declaration{}, Methods: map[string]map[string][]*declaration{},
		Attributes: map[string]map[string][]*declaration{}, Parents: map[string]string{}, Nodes: map[string][]nodeRef{},
		Tokens: map[string][]publiccompiler.Token{}, TokensByLexeme: map[string]map[string][]int{},
		Documents: map[string]documentSnapshot{}, Ranges: map[publiccompiler.Span]Range{}, seen: map[string]struct{}{},
	}
	index.Parents["IO"] = "Object"
	index.Parents["Int"] = "Object"
	index.Parents["String"] = "Object"
	index.Parents["Bool"] = "Object"
	return index
}

func (index *semanticIndex) rangeFromSpan(span publiccompiler.Span) Range {
	if cached, exists := index.Ranges[span]; exists {
		return cached
	}
	value := rangeForSpan(index.Documents, span.SourceID, span)
	index.Ranges[span] = value
	return value
}

func buildSemanticIndex(value *analysis) *semanticIndex {
	index := newSemanticIndex()
	index.Documents = value.Documents
	for _, token := range value.Report.Tokens {
		uri := token.Span.SourceID
		position := len(index.Tokens[uri])
		index.Tokens[uri] = append(index.Tokens[uri], token)
		if index.TokensByLexeme[uri] == nil {
			index.TokensByLexeme[uri] = map[string][]int{}
		}
		index.TokensByLexeme[uri][token.Lexeme] = append(index.TokensByLexeme[uri][token.Lexeme], position)
	}
	for _, symbol := range value.Report.Symbols {
		selection := index.rangeFromSpan(symbol.Span)
		builtin := symbol.Span.SourceID == ""
		decl := &declaration{
			Name: symbol.Name, Kind: symbol.Kind, Type: symbol.Type, Owner: symbol.Owner, Scope: symbol.Scope,
			URI: symbol.Span.SourceID, Selection: selection, Full: selection, Mutable: symbol.Mutable, Builtin: builtin,
		}
		decl.Visible = selection
		decl.ID = declarationID(decl)
		index.Declarations = append(index.Declarations, decl)
		index.ByID[decl.ID] = decl
		switch symbol.Kind {
		case "class":
			index.Classes[symbol.Name] = append(index.Classes[symbol.Name], decl)
		case "method", "builtin method":
			if index.Methods[symbol.Owner] == nil {
				index.Methods[symbol.Owner] = map[string][]*declaration{}
			}
			index.Methods[symbol.Owner][symbol.Name] = append(index.Methods[symbol.Owner][symbol.Name], decl)
		case "attribute":
			if index.Attributes[symbol.Owner] == nil {
				index.Attributes[symbol.Owner] = map[string][]*declaration{}
			}
			index.Attributes[symbol.Owner][symbol.Name] = append(index.Attributes[symbol.Owner][symbol.Name], decl)
		}
		if !builtin {
			index.addOccurrence(occurrence{Name: decl.Name, Kind: decl.Kind, Type: decl.Type, URI: decl.URI, Range: decl.Selection, DeclarationID: decl.ID, Declaration: true, Mutable: decl.Mutable})
		}
	}
	if value.Report.AST != nil {
		for childIndex := range value.Report.AST.Children {
			class := &value.Report.AST.Children[childIndex]
			if class.Kind != "Class" {
				continue
			}
			index.Parents[class.Name] = class.Type
			index.walkClass(class)
		}
	}
	index.addTypeOccurrences()
	for uri := range index.Occurrences {
		sort.Slice(index.Occurrences[uri], func(i, j int) bool {
			left, right := index.Occurrences[uri][i], index.Occurrences[uri][j]
			if comparison := comparePosition(left.Range.Start, right.Range.Start); comparison != 0 {
				return comparison < 0
			}
			return comparePosition(left.Range.End, right.Range.End) < 0
		})
	}
	for id := range index.References {
		sort.Slice(index.References[id], func(i, j int) bool {
			left, right := index.References[id][i], index.References[id][j]
			if left.URI != right.URI {
				return left.URI < right.URI
			}
			return comparePosition(left.Range.Start, right.Range.Start) < 0
		})
	}
	return index
}

func declarationID(decl *declaration) string {
	return fmt.Sprintf("%s|%s|%s|%s|%d:%d|%s", decl.Kind, decl.Owner, decl.Scope, decl.URI, decl.Selection.Start.Line, decl.Selection.Start.Character, decl.Name)
}

func (index *semanticIndex) addOccurrence(item occurrence) {
	key := fmt.Sprintf("%s|%d:%d-%d:%d|%s", item.URI, item.Range.Start.Line, item.Range.Start.Character, item.Range.End.Line, item.Range.End.Character, item.Name)
	if _, exists := index.seen[key]; exists {
		return
	}
	index.seen[key] = struct{}{}
	index.Occurrences[item.URI] = append(index.Occurrences[item.URI], item)
	if item.DeclarationID != "" && !item.Declaration {
		index.References[item.DeclarationID] = append(index.References[item.DeclarationID], Location{URI: item.URI, Range: item.Range})
	}
}

func (index *semanticIndex) walkClass(class *publiccompiler.ASTNode) {
	classDecl := index.findDeclaration(class.Name, []string{"class"}, class.Name, index.rangeFromSpan(class.Span))
	if classDecl != nil {
		classDecl.Full = index.rangeFromSpan(class.Span)
		classDecl.Visible = classDecl.Full
	}
	for childIndex := range class.Children {
		feature := &class.Children[childIndex]
		switch feature.Kind {
		case "Attribute":
			decl := index.findDeclaration(feature.Name, []string{"attribute"}, class.Name, index.rangeFromSpan(feature.Span))
			if decl != nil {
				decl.Full = index.rangeFromSpan(feature.Span)
				decl.Visible = decl.Full
			}
			index.recordNode(feature, class.Name, "", 1)
			for indexChild := range feature.Children {
				index.walkExpression(&feature.Children[indexChild], class.Name, "", nil, 2)
			}
		case "Method":
			methodDecl := index.findDeclaration(feature.Name, []string{"method"}, class.Name, index.rangeFromSpan(feature.Span))
			if methodDecl != nil {
				methodDecl.Full = index.rangeFromSpan(feature.Span)
				methodDecl.Visible = methodDecl.Full
			}
			index.recordNode(feature, class.Name, feature.Name, 1)
			environment := []*declaration{}
			for formalIndex := range feature.Children {
				formal := &feature.Children[formalIndex]
				if formal.Kind != "Formal" {
					continue
				}
				decl := index.findDeclaration(formal.Name, []string{"formal"}, class.Name, index.rangeFromSpan(formal.Span))
				if decl != nil {
					decl.Full = index.rangeFromSpan(feature.Span)
					decl.Visible = decl.Full
					environment = append(environment, decl)
				}
				index.recordNode(formal, class.Name, feature.Name, 2)
			}
			for bodyIndex := range feature.Children {
				body := &feature.Children[bodyIndex]
				if body.Kind != "Formal" {
					index.walkExpression(body, class.Name, feature.Name, environment, 2)
				}
			}
		}
	}
}

func (index *semanticIndex) walkExpression(node *publiccompiler.ASTNode, class, method string, environment []*declaration, depth int) {
	if node == nil {
		return
	}
	index.recordNode(node, class, method, depth)
	switch node.Kind {
	case "Identifier":
		decl := index.resolveValue(node.Name, class, environment)
		if token := index.tokenOutsideChildren(node, node.Name); token != nil {
			index.addReference(node.Name, valueKind(decl), node.Type, token.Span, decl)
		}
		return
	case "Assignment":
		name := strings.TrimSpace(strings.TrimSuffix(node.Text, "<-"))
		if token := index.tokenOutsideChildren(node, name); token != nil {
			decl := index.resolveValue(name, class, environment)
			index.addReference(name, valueKind(decl), node.Type, token.Span, decl)
		}
	case "Dispatch":
		index.recordDispatch(node, class, environment)
	case "Let":
		local := append([]*declaration(nil), environment...)
		for childIndex := range node.Children {
			child := &node.Children[childIndex]
			if child.Kind != "LetBinding" {
				index.walkExpression(child, class, method, local, depth+1)
				continue
			}
			index.recordNode(child, class, method, depth+1)
			for initializer := range child.Children {
				index.walkExpression(&child.Children[initializer], class, method, local, depth+2)
			}
			decl := index.findDeclaration(child.Name, []string{"let"}, class, index.rangeFromSpan(child.Span))
			if decl != nil {
				decl.Full = index.rangeFromSpan(node.Span)
				decl.Visible = Range{Start: index.rangeFromSpan(child.Span).End, End: index.rangeFromSpan(node.Span).End}
				local = append(local, decl)
			}
		}
		return
	case "Case":
		if len(node.Children) > 0 {
			index.walkExpression(&node.Children[0], class, method, environment, depth+1)
		}
		for childIndex := 1; childIndex < len(node.Children); childIndex++ {
			branch := &node.Children[childIndex]
			index.recordNode(branch, class, method, depth+1)
			local := append([]*declaration(nil), environment...)
			decl := index.findDeclaration(branch.Name, []string{"case"}, class, index.rangeFromSpan(branch.Span))
			if decl != nil {
				decl.Full = index.rangeFromSpan(branch.Span)
				decl.Visible = decl.Full
				local = append(local, decl)
			}
			for body := range branch.Children {
				index.walkExpression(&branch.Children[body], class, method, local, depth+2)
			}
		}
		return
	}
	for childIndex := range node.Children {
		index.walkExpression(&node.Children[childIndex], class, method, environment, depth+1)
	}
}

func (index *semanticIndex) recordNode(node *publiccompiler.ASTNode, class, method string, depth int) {
	if node.Span.SourceID == "" {
		return
	}
	index.Nodes[node.Span.SourceID] = append(index.Nodes[node.Span.SourceID], nodeRef{Node: node, URI: node.Span.SourceID, Range: index.rangeFromSpan(node.Span), Class: class, Method: method, Depth: depth})
}

func (index *semanticIndex) recordDispatch(node *publiccompiler.ASTNode, class string, environment []*declaration) {
	text := node.Text
	method := strings.TrimSuffix(text, "(...)")
	if dot := strings.LastIndex(method, "."); dot >= 0 {
		method = method[dot+1:]
	}
	method = strings.TrimSpace(method)
	token := index.tokenOutsideChildren(node, method)
	if token == nil {
		return
	}
	receiverType := class
	if strings.HasPrefix(text, "@") {
		if dot := strings.Index(text, "."); dot > 1 {
			receiverType = text[1:dot]
		}
	} else {
		methodStart := index.rangeFromSpan(token.Span).Start
		for childIndex := range node.Children {
			child := &node.Children[childIndex]
			childRange := index.rangeFromSpan(child.Span)
			if comparePosition(childRange.End, methodStart) <= 0 && child.Type != "" {
				receiverType = child.Type
				break
			}
		}
	}
	if receiverType == "SELF_TYPE" {
		receiverType = class
	}
	decl := index.resolveMethod(receiverType, method)
	index.addReference(method, "method", node.Type, token.Span, decl)
}

func (index *semanticIndex) tokenOutsideChildren(node *publiccompiler.ASTNode, name string) *publiccompiler.Token {
	tokens := index.Tokens[node.Span.SourceID]
	nodeRange := index.rangeFromSpan(node.Span)
	for _, tokenIndex := range index.TokensByLexeme[node.Span.SourceID][name] {
		token := &tokens[tokenIndex]
		tokenRange := index.rangeFromSpan(token.Span)
		if !positionInRange(tokenRange.Start, nodeRange) {
			continue
		}
		insideChild := false
		for childIndex := range node.Children {
			if positionInRange(tokenRange.Start, index.rangeFromSpan(node.Children[childIndex].Span)) {
				insideChild = true
				break
			}
		}
		if !insideChild {
			return token
		}
	}
	return nil
}

func (index *semanticIndex) addReference(name, kind, staticType string, span publiccompiler.Span, decl *declaration) {
	item := occurrence{Name: name, Kind: kind, Type: staticType, URI: span.SourceID, Range: index.rangeFromSpan(span)}
	if decl != nil {
		item.DeclarationID = decl.ID
		item.Kind = decl.Kind
		item.Mutable = decl.Mutable
		item.Builtin = decl.Builtin
	}
	index.addOccurrence(item)
}

func (index *semanticIndex) addTypeOccurrences() {
	for uri, tokens := range index.Tokens {
		for _, token := range tokens {
			if string(token.Type) != "TYPE_ID" {
				continue
			}
			if index.occurrenceExact(uri, index.rangeFromSpan(token.Span)) != nil {
				continue
			}
			decl := index.resolveClass(token.Lexeme)
			index.addReference(token.Lexeme, "class", token.Lexeme, token.Span, decl)
		}
	}
}

func (index *semanticIndex) findDeclaration(name string, kinds []string, owner string, within Range) *declaration {
	for _, decl := range index.Declarations {
		if decl.Name != name || decl.Builtin || owner != "" && decl.Owner != owner {
			continue
		}
		matched := false
		for _, kind := range kinds {
			if decl.Kind == kind {
				matched = true
				break
			}
		}
		if !matched || decl.URI == "" || !positionInRange(decl.Selection.Start, within) {
			continue
		}
		return decl
	}
	return nil
}

func (index *semanticIndex) resolveClass(name string) *declaration {
	if candidates := index.Classes[name]; len(candidates) > 0 {
		return candidates[0]
	}
	return nil
}

func (index *semanticIndex) resolveMethod(class, name string) *declaration {
	visited := map[string]bool{}
	for class != "" && !visited[class] {
		visited[class] = true
		if byName := index.Methods[class]; len(byName[name]) > 0 {
			return byName[name][0]
		}
		class = index.Parents[class]
	}
	return nil
}

func (index *semanticIndex) resolveAttribute(class, name string) *declaration {
	visited := map[string]bool{}
	for class != "" && !visited[class] {
		visited[class] = true
		if byName := index.Attributes[class]; len(byName[name]) > 0 {
			return byName[name][0]
		}
		class = index.Parents[class]
	}
	return nil
}

func (index *semanticIndex) resolveValue(name, class string, environment []*declaration) *declaration {
	for position := len(environment) - 1; position >= 0; position-- {
		if environment[position] != nil && environment[position].Name == name {
			return environment[position]
		}
	}
	return index.resolveAttribute(class, name)
}

func valueKind(decl *declaration) string {
	if decl == nil {
		return "variable"
	}
	return decl.Kind
}

func (index *semanticIndex) occurrenceExact(uri string, target Range) *occurrence {
	for itemIndex := range index.Occurrences[uri] {
		item := &index.Occurrences[uri][itemIndex]
		if item.Range == target {
			return item
		}
	}
	return nil
}

func (index *semanticIndex) occurrenceAt(uri string, position Position) *occurrence {
	for itemIndex := range index.Occurrences[uri] {
		item := &index.Occurrences[uri][itemIndex]
		if positionInRange(position, item.Range) {
			return item
		}
	}
	return nil
}

func (index *semanticIndex) tokenAt(uri string, position Position) *publiccompiler.Token {
	for tokenIndex := range index.Tokens[uri] {
		token := &index.Tokens[uri][tokenIndex]
		if positionInRange(position, index.rangeFromSpan(token.Span)) {
			return token
		}
	}
	return nil
}

func (index *semanticIndex) contextAt(uri string, position Position) *nodeRef {
	var best *nodeRef
	for nodeIndex := range index.Nodes[uri] {
		node := &index.Nodes[uri][nodeIndex]
		if !positionInRange(position, node.Range) {
			continue
		}
		if best == nil || node.Depth > best.Depth {
			best = node
		}
	}
	return best
}

func (index *semanticIndex) methodsFor(class string) []*declaration {
	result := []*declaration{}
	seen := map[string]bool{}
	visited := map[string]bool{}
	if class == "SELF_TYPE" {
		return result
	}
	for class != "" && !visited[class] {
		visited[class] = true
		names := make([]string, 0, len(index.Methods[class]))
		for name := range index.Methods[class] {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if !seen[name] && len(index.Methods[class][name]) > 0 {
				result = append(result, index.Methods[class][name][0])
				seen[name] = true
			}
		}
		class = index.Parents[class]
	}
	return result
}

func (index *semanticIndex) attributesFor(class string) []*declaration {
	result := []*declaration{}
	seen := map[string]bool{}
	visited := map[string]bool{}
	for class != "" && !visited[class] {
		visited[class] = true
		names := make([]string, 0, len(index.Attributes[class]))
		for name := range index.Attributes[class] {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if !seen[name] && len(index.Attributes[class][name]) > 0 {
				result = append(result, index.Attributes[class][name][0])
				seen[name] = true
			}
		}
		class = index.Parents[class]
	}
	return result
}
