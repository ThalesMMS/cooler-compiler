// SPDX-License-Identifier: AGPL-3.0-or-later
package lsp

import (
	"context"
	"encoding/json"
	"strings"

	api "github.com/ThalesMMS/cooler-compiler/compiler"
)

type signatureParameter struct {
	Label string `json:"label"`
}
type signatureInformation struct {
	Label         string               `json:"label"`
	Documentation string               `json:"documentation,omitempty"`
	Parameters    []signatureParameter `json:"parameters"`
}
type signatureHelp struct {
	Signatures      []signatureInformation `json:"signatures"`
	ActiveSignature int                    `json:"activeSignature"`
	ActiveParameter *int                   `json:"activeParameter,omitempty"`
}
type methodSignature struct {
	owner, name, result string
	params              []signatureParameter
}
type signatureModel struct {
	classes map[string]*api.ASTNode
	parents map[string]string
	methods map[string]*methodSignature
	index   *semanticIndex
}

func newSignatureModel(value *analysis) *signatureModel {
	m := &signatureModel{classes: map[string]*api.ASTNode{}, parents: map[string]string{"IO": "Object", "Int": "Object", "Bool": "Object", "String": "Object"}, methods: map[string]*methodSignature{}, index: value.Index}
	for _, b := range []struct {
		owner, name, result string
		params              []string
	}{
		{"Object", "abort", "Object", nil}, {"Object", "type_name", "String", nil}, {"Object", "copy", "SELF_TYPE", nil},
		{"IO", "out_string", "SELF_TYPE", []string{"x: String"}}, {"IO", "out_int", "SELF_TYPE", []string{"x: Int"}},
		{"IO", "in_string", "String", nil}, {"IO", "in_int", "Int", nil}, {"String", "length", "Int", nil},
		{"String", "concat", "String", []string{"s: String"}}, {"String", "substr", "String", []string{"i: Int", "l: Int"}},
	} {
		sig := &methodSignature{owner: b.owner, name: b.name, result: b.result, params: []signatureParameter{}}
		for _, p := range b.params {
			sig.params = append(sig.params, signatureParameter{Label: p})
		}
		m.methods[b.owner+"."+b.name] = sig
	}
	if value.Report.AST == nil {
		return m
	}
	for i := range value.Report.AST.Children {
		class := &value.Report.AST.Children[i]
		if class.Kind != "Class" {
			continue
		}
		if _, exists := m.classes[class.Name]; exists {
			m.classes[class.Name] = nil
			continue
		}
		m.classes[class.Name] = class
		m.parents[class.Name] = class.Type
		for j := range class.Children {
			method := &class.Children[j]
			if method.Kind != "Method" {
				continue
			}
			key := class.Name + "." + method.Name
			if _, exists := m.methods[key]; exists {
				m.methods[key] = nil
				continue
			}
			sig := &methodSignature{owner: class.Name, name: method.Name, result: method.Type, params: []signatureParameter{}}
			for _, formal := range method.Children {
				if formal.Kind == "Formal" {
					sig.params = append(sig.params, signatureParameter{Label: formal.Name + ": " + formal.Type})
				}
			}
			m.methods[key] = sig
		}
	}
	return m
}

func (m *signatureModel) lookup(class, name string) *methodSignature {
	seen := map[string]bool{}
	for class != "" && !seen[class] {
		seen[class] = true
		if node, exists := m.classes[class]; exists && node == nil {
			return nil
		}
		if sig, exists := m.methods[class+"."+name]; exists {
			return sig
		}
		class = m.parents[class]
	}
	return nil
}

func (m *signatureModel) ancestor(receiver, target string) bool {
	seen := map[string]bool{}
	for receiver != "" && !seen[receiver] {
		if receiver == target {
			return true
		}
		seen[receiver] = true
		receiver = m.parents[receiver]
	}
	return false
}

func signatureEnvironment(node *api.ASTNode, position Position, env map[string]string, index *semanticIndex) {
	if node.Kind == "Let" {
		for i := range node.Children {
			child := &node.Children[i]
			if child.Kind == "LetBinding" {
				if positionInRange(position, index.rangeFromSpan(child.Span)) {
					for j := range child.Children {
						signatureEnvironment(&child.Children[j], position, env, index)
					}
					return
				}
				env[child.Name] = child.Type
			} else if positionInRange(position, index.rangeFromSpan(child.Span)) {
				signatureEnvironment(child, position, env, index)
				return
			}
		}
		return
	}
	if node.Kind == "CaseBranch" {
		env[node.Name] = node.Type
	}
	for i := range node.Children {
		child := &node.Children[i]
		if positionInRange(position, index.rangeFromSpan(child.Span)) {
			signatureEnvironment(child, position, env, index)
			return
		}
	}
}

func (m *signatureModel) environment(class string, position Position) map[string]string {
	env := map[string]string{"self": class}
	lineage := []string{}
	seen := map[string]bool{}
	for current := class; current != "" && !seen[current]; current = m.parents[current] {
		seen[current] = true
		lineage = append(lineage, current)
	}
	for i := len(lineage) - 1; i >= 0; i-- {
		if node := m.classes[lineage[i]]; node != nil {
			for _, attr := range node.Children {
				if attr.Kind == "Attribute" {
					env[attr.Name] = attr.Type
				}
			}
		}
	}
	if node := m.classes[class]; node != nil {
		for i := range node.Children {
			method := &node.Children[i]
			if (method.Kind != "Method" && method.Kind != "Attribute") || !positionInRange(position, m.index.rangeFromSpan(method.Span)) {
				continue
			}
			for _, formal := range method.Children {
				if formal.Kind == "Formal" {
					env[formal.Name] = formal.Type
				}
			}
			signatureEnvironment(method, position, env, m.index)
			break
		}
	}
	return env
}

func (m *signatureModel) expressionType(node *api.ASTNode, class string, env map[string]string, depth int) string {
	if node == nil || depth > 64 {
		return ""
	}
	typ := node.Type
	if typ != "" {
		if typ == "SELF_TYPE" {
			return class
		}
		return typ
	}
	switch node.Kind {
	case "Int":
		return "Int"
	case "String":
		return "String"
	case "Bool":
		return "Bool"
	case "Identifier":
		typ = env[node.Name]
		if typ == "SELF_TYPE" {
			return class
		}
		return typ
	case "New":
		typ = strings.TrimPrefix(node.Text, "new ")
		if typ == "SELF_TYPE" {
			return class
		}
		return typ
	case "Block":
		if len(node.Children) > 0 {
			return m.expressionType(&node.Children[len(node.Children)-1], class, env, depth+1)
		}
	case "Dispatch":
		method := strings.TrimSuffix(node.Text, "(...)")
		static := ""
		if strings.HasPrefix(method, "@") {
			parts := strings.SplitN(method[1:], ".", 2)
			if len(parts) != 2 {
				return ""
			}
			static, method = parts[0], parts[1]
		}
		token := m.index.tokenOutsideChildren(node, method)
		if token == nil {
			return ""
		}
		receiver := class
		for i := range node.Children {
			child := &node.Children[i]
			if comparePosition(m.index.rangeFromSpan(child.Span).End, m.index.rangeFromSpan(token.Span).Start) <= 0 {
				receiver = m.expressionType(child, class, env, depth+1)
				break
			}
		}
		lookup := receiver
		if static != "" {
			if !m.ancestor(receiver, static) {
				return ""
			}
			lookup = static
		}
		sig := m.lookup(lookup, method)
		if sig == nil {
			return ""
		}
		if sig.result == "SELF_TYPE" {
			return lookup
		}
		return sig.result
	}
	return ""
}

func (s *Server) requestSignatureHelp(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	if ctx.Err() != nil {
		return nil, &rpcError{Code: jsonRPCRequestCancel, Message: "Request cancelled"}
	}
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
		return nil, nil
	}
	if _, err := byteOffsetAt(doc.Text, params.Position); err != nil {
		return nil, invalidParams(err)
	}
	result := signatureAt(ctx, value, params.TextDocument.URI, params.Position)
	if ctx.Err() != nil {
		return nil, &rpcError{Code: jsonRPCRequestCancel, Message: "Request cancelled"}
	}
	s.mu.Lock()
	current := value.Generation == s.generation
	s.mu.Unlock()
	if !current {
		return nil, &rpcError{Code: jsonRPCContentModified, Message: "Document changed during signature request"}
	}
	if result == nil {
		return nil, nil
	}
	return result, nil
}

func signatureAt(ctx context.Context, value *analysis, uri string, position Position) *signatureHelp {
	tokens := value.Index.Tokens[uri]
	type delimiter struct {
		kind           string
		method, commas int
	}
	stack := []delimiter{}
	for i, token := range tokens {
		if i%256 == 0 && ctx.Err() != nil {
			return nil
		}
		if comparePosition(value.Index.rangeFromSpan(token.Span).End, position) > 0 {
			break
		}
		switch string(token.Type) {
		case "LPAREN":
			method := -1
			if i > 0 && string(tokens[i-1].Type) == "OBJECT_ID" {
				method = i - 1
			}
			stack = append(stack, delimiter{kind: "paren", method: method})
		case "LBRACE":
			stack = append(stack, delimiter{kind: "brace", method: -1})
		case "LET":
			stack = append(stack, delimiter{kind: "let", method: -1})
		case "IN":
			if len(stack) > 0 && stack[len(stack)-1].kind == "let" {
				stack = stack[:len(stack)-1]
			}
		case "RPAREN", "RBRACE":
			kind := "paren"
			if string(token.Type) == "RBRACE" {
				kind = "brace"
			}
			for len(stack) > 0 {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if top.kind == kind {
					break
				}
			}
		case "COMMA":
			if len(stack) > 0 && stack[len(stack)-1].kind == "paren" {
				stack[len(stack)-1].commas++
			}
		}
	}
	var call *delimiter
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i].kind == "paren" && stack[i].method >= 0 {
			call = &stack[i]
			break
		}
	}
	if call == nil {
		return nil
	}
	nameToken := tokens[call.method]
	namePosition := value.Index.rangeFromSpan(nameToken.Span).Start
	model := value.signatureModel()
	class := ""
	for name, node := range model.classes {
		if node != nil && node.Span.SourceID == uri && positionInRange(namePosition, value.Index.rangeFromSpan(node.Span)) {
			if class != "" {
				return nil
			}
			class = name
		}
	}
	if class == "" {
		return nil
	}
	// A method declaration's parameter list is not an invocation.
	for _, method := range model.classes[class].Children {
		if method.Kind == "Method" && method.Name == nameToken.Lexeme && value.Index.rangeFromSpan(method.Span).Start == namePosition {
			return nil
		}
	}
	env := model.environment(class, namePosition)
	lookup := class
	if call.method > 0 && string(tokens[call.method-1].Type) == "DOT" {
		dot := call.method - 1
		receiverEnd := dot - 1
		static := ""
		if dot >= 2 && string(tokens[dot-2].Type) == "AT" {
			static = tokens[dot-1].Lexeme
			receiverEnd = dot - 3
		}
		if receiverEnd < 0 {
			return nil
		}
		end := value.Index.rangeFromSpan(tokens[receiverEnd].Span).End
		var expression *api.ASTNode
		for _, ref := range value.Index.Nodes[uri] {
			if ref.Range.End == end && comparePosition(ref.Range.Start, namePosition) < 0 && (expression == nil || comparePosition(ref.Range.Start, value.Index.rangeFromSpan(expression.Span).Start) < 0) {
				expression = ref.Node
			}
		}
		receiver := model.expressionType(expression, class, env, 0)
		if receiver == "" {
			return nil
		}
		lookup = receiver
		if static != "" {
			if !model.ancestor(receiver, static) {
				return nil
			}
			lookup = static
		}
	}
	sig := model.lookup(lookup, nameToken.Lexeme)
	if sig == nil {
		return nil
	}
	resultType := sig.result
	documentation := builtinDocumentation[sig.owner+"."+sig.name]
	if resultType == "SELF_TYPE" {
		resultType = lookup
		documentation += " SELF_TYPE is resolved in dispatch context " + lookup + "."
	}
	labels := []string{}
	for _, param := range sig.params {
		labels = append(labels, param.Label)
	}
	info := signatureInformation{Label: sig.owner + "." + sig.name + "(" + strings.Join(labels, ", ") + "): " + resultType, Documentation: strings.TrimSpace(documentation), Parameters: sig.params}
	result := &signatureHelp{Signatures: []signatureInformation{info}}
	if len(sig.params) > 0 {
		active := min(call.commas, len(sig.params)-1)
		result.ActiveParameter = &active
	}
	return result
}
