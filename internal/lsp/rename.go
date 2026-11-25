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

func renameFailure(message string) *rpcError {
	return &rpcError{Code: jsonRPCInvalidParams, Message: message}
}

func renameTarget(value *analysis, params TextDocumentPositionParams) *declaration {
	if value.Result.HasErrors() {
		return nil
	}
	item := value.Index.occurrenceAt(params.TextDocument.URI, params.Position)
	if item == nil {
		return nil
	}
	decl := value.Index.ByID[item.DeclarationID]
	if decl == nil || decl.Builtin || decl.Name == "self" || decl.Name == "SELF_TYPE" || decl.Kind == "class" && decl.Name == "Main" || decl.Kind == "method" && decl.Owner == "Main" && decl.Name == "main" {
		return nil
	}
	switch decl.Kind {
	case "class", "method", "attribute", "formal", "parameter", "let", "case":
		return decl
	}
	return nil
}

func (s *Server) requestPrepareRename(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
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
	decl := renameTarget(value, params)
	if decl == nil {
		return nil, nil
	}
	item := value.Index.occurrenceAt(params.TextDocument.URI, params.Position)
	s.mu.Lock()
	current := value.Generation == s.generation && s.documentChanges
	s.mu.Unlock()
	if !current || ctx.Err() != nil {
		return nil, nil
	}
	return map[string]any{"range": item.Range, "placeholder": decl.Name}, nil
}

var renameIdentifier = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)

func validRenameName(name, kind string) bool {
	if !renameIdentifier.MatchString(name) || name == "self" || name == "SELF_TYPE" {
		return false
	}
	if (name[0] >= 'A' && name[0] <= 'Z') != (kind == "class") {
		return false
	}
	switch strings.ToLower(name) {
	case "class", "inherits", "if", "then", "else", "fi", "while", "loop", "pool", "let", "in", "case", "of", "esac", "new", "isvoid", "not", "true", "false":
		return false
	}
	return true
}

func methodFamilyRoot(index *semanticIndex, owner, name string) string {
	root := owner
	for parent := index.Parents[owner]; parent != ""; parent = index.Parents[parent] {
		if len(index.Methods[parent][name]) > 0 {
			root = parent
		}
	}
	return root
}

func translatedRenamePosition(position Position, edits []textEdit) Position {
	result := position
	for _, edit := range edits {
		if edit.Range.End.Line == position.Line && comparePosition(edit.Range.End, position) <= 0 {
			result.Character += len(edit.NewText) - (edit.Range.End.Character - edit.Range.Start.Character)
		}
	}
	return result
}

func translatedRenameRange(r Range, edits []textEdit) Range {
	return Range{Start: translatedRenamePosition(r.Start, edits), End: translatedRenamePosition(r.End, edits)}
}

func (s *Server) requestRename(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	operation, cancel := context.WithTimeout(ctx, s.config.AnalysisTimeout)
	defer cancel()
	ctx = operation
	if ctx.Err() != nil {
		return nil, &rpcError{Code: jsonRPCRequestCancel, Message: "Request cancelled"}
	}
	var params struct {
		TextDocumentPositionParams
		NewName string `json:"newName"`
	}
	if err := decodeParams(raw, &params); err != nil {
		return nil, invalidParams(err)
	}
	value, rpcErr := s.analysisForRequest(ctx)
	if rpcErr != nil {
		return nil, rpcErr
	}
	decl := renameTarget(value, params.TextDocumentPositionParams)
	if decl == nil {
		return nil, renameFailure("Symbol is not safely editable")
	}
	if !validRenameName(params.NewName, decl.Kind) {
		return nil, renameFailure("Invalid COOL identifier")
	}
	s.mu.Lock()
	versioned := s.documentChanges
	s.mu.Unlock()
	if !versioned {
		return nil, renameFailure("Rename requires versioned documentChanges support")
	}
	selected := map[string]bool{decl.ID: true}
	if decl.Kind == "method" {
		root := methodFamilyRoot(value.Index, decl.Owner, decl.Name)
		for _, candidate := range value.Index.Declarations {
			if (candidate.Kind == "method" || candidate.Kind == "builtin method") && candidate.Name == decl.Name && methodFamilyRoot(value.Index, candidate.Owner, candidate.Name) == root {
				if candidate.Builtin {
					return nil, renameFailure("Builtin override families cannot be renamed")
				}
				selected[candidate.ID] = true
			}
		}
		model := value.signatureModel()
		for _, candidate := range value.Index.Declarations {
			if candidate.Name == params.NewName && candidate.Name != decl.Name && (candidate.Kind == "method" || candidate.Kind == "builtin method") && (model.ancestor(candidate.Owner, root) || model.ancestor(root, candidate.Owner)) {
				return nil, renameFailure("Method rename would change dispatch families")
			}
		}
	}
	edits := map[string][]textEdit{}
	count := 0
	for uri, items := range value.Index.Occurrences {
		for _, item := range items {
			count++
			if count > 32768 {
				return nil, renameFailure("Rename exceeds 32768 indexed occurrences")
			}
			if selected[item.DeclarationID] {
				edits[uri] = append(edits[uri], textEdit{Range: item.Range, NewText: params.NewName})
			}
		}
	}
	documents := map[string]documentSnapshot{}
	project := publiccompiler.Project{}
	totalBytes := 0
	totalEdits := 0
	uris := make([]string, 0, len(value.Documents))
	for uri := range value.Documents {
		uris = append(uris, uri)
	}
	sort.Strings(uris)
	for _, uri := range uris {
		doc := value.Documents[uri]
		sort.Slice(edits[uri], func(i, j int) bool { return comparePosition(edits[uri][i].Range.Start, edits[uri][j].Range.Start) < 0 })
		totalEdits += len(edits[uri])
		projected := len(doc.Text)
		for _, edit := range edits[uri] {
			projected += len(edit.NewText) - (edit.Range.End.Character - edit.Range.Start.Character)
		}
		totalBytes += projected
		if totalEdits > 1000 || totalBytes > s.config.MaxWorkspaceBytes || totalBytes > 2*1024*1024 || projected > s.config.MaxFileBytes {
			return nil, renameFailure("Rename exceeds edit or source size limits")
		}
		text, editErr := applyRenameText(ctx, doc.Text, edits[uri], projected)
		if editErr != nil {
			return nil, editErr
		}
		doc.Text = text
		doc.Positions = newTextPositionIndex(doc.Text)
		documents[uri] = doc
		project.Sources = append(project.Sources, publiccompiler.SourceUnit{ID: uri, Name: uri, Content: doc.Text})
	}
	options := publiccompiler.DefaultOptions()
	options.Mode = publiccompiler.ModeCheck
	result, err := publiccompiler.CompileProject(ctx, project, options)
	if ctx.Err() != nil {
		return nil, &rpcError{Code: jsonRPCRequestCancel, Message: "Request cancelled"}
	}
	if err != nil || result.HasErrors() {
		return nil, renameFailure("Renamed project does not resolve without errors")
	}
	updated := &analysis{Documents: documents, Result: result, Report: result.Report()}
	updated.Index = buildSemanticIndex(updated)
	identities := map[string]string{}
	for _, old := range value.Index.Declarations {
		if old.Builtin {
			identities[old.ID] = old.ID
			continue
		}
		next := updated.Index.occurrenceExact(old.URI, translatedRenameRange(old.Selection, edits[old.URI]))
		if next == nil || !next.Declaration {
			return nil, renameFailure("Declaration identity changed")
		}
		identities[old.ID] = next.DeclarationID
	}
	for uri, items := range value.Index.Occurrences {
		for _, old := range items {
			if ctx.Err() != nil {
				return nil, &rpcError{Code: jsonRPCRequestCancel, Message: "Request cancelled"}
			}
			next := updated.Index.occurrenceExact(uri, translatedRenameRange(old.Range, edits[uri]))
			if old.DeclarationID == "" && (old.Name == "self" || old.Name == "SELF_TYPE") && next != nil && next.Name == old.Name && next.DeclarationID == "" {
				continue
			}
			if old.DeclarationID == "" || next == nil || next.DeclarationID != identities[old.DeclarationID] {
				return nil, renameFailure("Rename would capture or redirect a reference")
			}
		}
	}
	s.mu.Lock()
	current := value.Generation == s.generation
	s.mu.Unlock()
	if !current {
		return nil, &rpcError{Code: jsonRPCContentModified, Message: "Document changed"}
	}
	workspace := workspaceEdit{DocumentChanges: []textDocumentEdit{}}
	for _, uri := range uris {
		if len(edits[uri]) == 0 {
			continue
		}
		doc := value.Documents[uri]
		change := textDocumentEdit{Edits: edits[uri]}
		change.TextDocument.URI = uri
		if doc.Open {
			version := doc.Version
			change.TextDocument.Version = &version
		}
		workspace.DocumentChanges = append(workspace.DocumentChanges, change)
	}
	return workspace, nil
}

// Walk source once; identifiers are ASCII but their surrounding source is UTF-16 indexed.
func applyRenameText(ctx context.Context, source string, edits []textEdit, size int) (string, *rpcError) {
	var result strings.Builder
	result.Grow(size)
	position := Position{}
	offset := 0
	copied := 0
	advance := func(target Position) bool {
		for comparePosition(position, target) < 0 && offset < len(source) {
			if offset%1024 == 0 && ctx.Err() != nil {
				return false
			}
			r, n := utf8.DecodeRuneInString(source[offset:])
			offset += n
			if r == '\n' {
				position.Line++
				position.Character = 0
			} else {
				position.Character++
				if r > 0xffff {
					position.Character++
				}
			}
		}
		return position == target
	}
	for _, edit := range edits {
		if ctx.Err() != nil {
			return "", &rpcError{Code: jsonRPCRequestCancel, Message: "Request cancelled"}
		}
		if !advance(edit.Range.Start) {
			return "", renameFailure("Invalid rename position")
		}
		result.WriteString(source[copied:offset])
		result.WriteString(edit.NewText)
		if !advance(edit.Range.End) {
			return "", renameFailure("Invalid rename position")
		}
		copied = offset
	}
	result.WriteString(source[copied:])
	return result.String(), nil
}
