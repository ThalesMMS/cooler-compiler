// SPDX-License-Identifier: AGPL-3.0-or-later

package lsp

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync"

	publiccompiler "github.com/ThalesMMS/cooler-compiler/compiler"
)

type analysis struct {
	Generation     uint64
	Documents      map[string]documentSnapshot
	Result         publiccompiler.Result
	Report         publiccompiler.Report
	Index          *semanticIndex
	signaturesOnce sync.Once
	signatures     *signatureModel
}

func (value *analysis) signatureModel() *signatureModel {
	value.signaturesOnce.Do(func() { value.signatures = newSignatureModel(value) })
	return value.signatures
}

func (s *Server) analyze(ctx context.Context) (*analysis, error) {
	if ctx == nil {
		return nil, errors.New("nil analysis context")
	}
	queueDone := beginMeasurement(s.config.measurement, "queue")
	if s.config.measurement != nil {
		s.config.measurement.record("queue-enter", 0)
	}
	select {
	case <-ctx.Done():
		queueDone()
		return nil, ctx.Err()
	case <-s.analysisToken:
	}
	queueDone()
	defer func() { s.analysisToken <- struct{}{} }()

	s.mu.Lock()
	if s.cache != nil && s.cache.Generation == s.generation {
		cached := s.cache
		s.mu.Unlock()
		if s.config.measurement != nil {
			s.config.measurement.record("cache-hit", 0)
		}
		return cached, nil
	}
	s.mu.Unlock()
	generation, documents, warnings := s.snapshotAnalysisInputs()
	byURI := make(map[string]documentSnapshot, len(documents))
	if len(documents) == 0 {
		empty := &analysis{Generation: generation, Documents: byURI, Index: newSemanticIndex()}
		s.mu.Lock()
		if generation == s.generation {
			s.cache = empty
		}
		s.mu.Unlock()
		return empty, nil
	}
	project := publiccompiler.Project{Sources: make([]publiccompiler.SourceUnit, 0, len(documents))}
	totalBytes := 0
	for _, doc := range documents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		totalBytes += len(doc.Text)
		if totalBytes > s.config.MaxWorkspaceBytes {
			return nil, fmt.Errorf("workspace source exceeds %d bytes", s.config.MaxWorkspaceBytes)
		}
		byURI[doc.URI] = doc
		name := filepath.Base(doc.Path)
		if name == "." || name == "" {
			name = doc.URI
		}
		project.Sources = append(project.Sources, publiccompiler.SourceUnit{ID: doc.URI, Name: name, Content: doc.Text})
	}
	options := publiccompiler.DefaultOptions()
	options.Mode = publiccompiler.ModeCheck
	options.WarningRules = warnings
	compileDone := beginMeasurement(s.config.measurement, "compile")
	result, err := publiccompiler.CompileProject(ctx, project, options)
	compileDone()
	if err != nil {
		return nil, err
	}
	reportDone := beginMeasurement(s.config.measurement, "report")
	report := result.Report()
	reportDone()
	completed := &analysis{Generation: generation, Documents: byURI, Result: result, Report: report}
	indexDone := beginMeasurement(s.config.measurement, "index")
	completed.Index = buildSemanticIndex(completed)
	indexDone()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if generation == s.generation {
		s.cache = completed
	}
	s.mu.Unlock()
	return completed, nil
}

func (s *Server) analysisForRequest(ctx context.Context) (*analysis, *rpcError) {
	operation, cancel := context.WithTimeout(ctx, s.config.AnalysisTimeout)
	defer cancel()
	value, err := s.analyze(operation)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, &rpcError{Code: jsonRPCRequestCancel, Message: "Request cancelled"}
	}
	if err != nil {
		return nil, &rpcError{Code: jsonRPCInternalError, Message: "analysis failed", Data: err.Error()}
	}
	return value, nil
}

func (s *Server) publishDiagnostics(value *analysis) {
	if value == nil {
		return
	}
	s.mu.Lock()
	if value.Generation != s.generation {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	publicationDone := beginMeasurement(s.config.measurement, "diagnostic-conversion")
	grouped := make(map[string][]diagnostic, len(value.Documents))
	for uri := range value.Documents {
		grouped[uri] = []diagnostic{}
	}
	for _, item := range value.Report.Diagnostics {
		if item.Stage != "input" && item.Stage != "lexer" && item.Stage != "parser" && item.Stage != "semantic" {
			continue
		}
		uri := item.Span.SourceID
		if uri == "" && len(value.Report.SourceUnits) == 1 {
			uri = value.Report.SourceUnits[0].ID
		}
		if _, exists := grouped[uri]; !exists {
			continue
		}
		grouped[uri] = append(grouped[uri], lspDiagnostic(value.Documents, uri, item))
	}
	uris := make([]string, 0, len(grouped))
	for uri := range grouped {
		uris = append(uris, uri)
	}
	sort.Strings(uris)
	publicationDone()
	for _, uri := range uris {
		var version *int
		if doc := value.Documents[uri]; doc.Open {
			captured := doc.Version
			version = &captured
		}
		s.publishDiagnosticsIfCurrent(value.Generation, publishDiagnosticsParams{URI: uri, Version: version, Diagnostics: grouped[uri]})
	}
}

func (s *Server) publishDiagnosticsIfCurrent(generation uint64, params publishDiagnosticsParams) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if generation != s.generation {
		return
	}
	current := s.documents[params.URI]
	if current == nil || params.Version != nil && (!current.Open || current.Version != *params.Version) {
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := writeFrameMeasured(s.writer, rpcNotification{JSONRPC: "2.0", Method: "textDocument/publishDiagnostics", Params: params}, s.config.measurement); err != nil {
		s.logger.Printf("write diagnostics: %v", err)
	}
}

func lspDiagnostic(documents map[string]documentSnapshot, defaultURI string, item publiccompiler.Diagnostic) diagnostic {
	severity := 3
	switch item.Severity {
	case publiccompiler.SeverityError:
		severity = 1
	case publiccompiler.SeverityWarning:
		severity = 2
	}
	message := item.Message
	if item.Hint != "" {
		message += "\n\n" + item.Hint
	}
	result := diagnostic{Range: rangeForSpan(documents, defaultURI, item.Span), Severity: severity, Code: item.Code, Source: "cooler", Message: message}
	for _, related := range item.Related {
		uri := related.Span.SourceID
		if uri == "" {
			uri = defaultURI
		}
		result.RelatedInformation = append(result.RelatedInformation, diagnosticRelatedInfo{Location: Location{URI: uri, Range: rangeForSpan(documents, uri, related.Span)}, Message: related.Label})
	}
	if len(item.QuickFix) > 0 {
		fixes := make([]map[string]any, 0, len(item.QuickFix))
		for _, fix := range item.QuickFix {
			uri := fix.Span.SourceID
			if uri == "" {
				uri = defaultURI
			}
			fixes = append(fixes, map[string]any{"title": fix.Title, "replacement": fix.Replacement, "uri": uri, "range": rangeForSpan(documents, uri, fix.Span)})
		}
		result.Data = map[string]any{"quickFixes": fixes}
	}
	return result
}

func rangeFromSpan(span publiccompiler.Span) Range {
	startLine, startColumn := span.StartLine-1, span.StartColumn-1
	endLine, endColumn := span.EndLine-1, span.EndColumn-1
	if startLine < 0 {
		startLine = 0
	}
	if startColumn < 0 {
		startColumn = 0
	}
	if endLine < startLine {
		endLine = startLine
	}
	if endLine == startLine && endColumn < startColumn {
		endColumn = startColumn
	}
	return Range{Start: Position{Line: startLine, Character: startColumn}, End: Position{Line: endLine, Character: endColumn}}
}

func rangeForSpan(documents map[string]documentSnapshot, defaultURI string, span publiccompiler.Span) Range {
	uri := span.SourceID
	if uri == "" {
		uri = defaultURI
	}
	if document, ok := documents[uri]; ok {
		if document.Positions != nil {
			return rangeFromSpanInIndex(span, document.Positions)
		}
		return rangeFromSpanInText(span, document.Text)
	}
	return rangeFromSpan(span)
}

type textPositionIndex struct {
	lineStarts []int
	invalid    []int
	total      int
}

func newTextPositionIndex(text string) *textPositionIndex {
	index := &textPositionIndex{lineStarts: []int{0}}
	offset := 0
	for _, r := range text {
		width := 1
		if r > 0xFFFF {
			width = 2
			index.invalid = append(index.invalid, offset+1)
		}
		offset += width
		if r == '\n' {
			index.lineStarts = append(index.lineStarts, offset)
		}
	}
	index.total = offset
	return index
}

func (index *textPositionIndex) position(target int) (Position, bool) {
	if index == nil || target < 0 || target > index.total {
		return Position{}, false
	}
	if invalid := sort.SearchInts(index.invalid, target); invalid < len(index.invalid) && index.invalid[invalid] == target {
		return Position{}, false
	}
	line := sort.Search(len(index.lineStarts), func(i int) bool { return index.lineStarts[i] > target }) - 1
	if line < 0 {
		line = 0
	}
	return Position{Line: line, Character: target - index.lineStarts[line]}, true
}

func rangeFromSpanInIndex(span publiccompiler.Span, index *textPositionIndex) Range {
	start, startOK := index.position(span.StartOffset)
	end, endOK := index.position(span.EndOffset)
	if !startOK || !endOK || comparePosition(end, start) < 0 {
		return rangeFromSpan(span)
	}
	return Range{Start: start, End: end}
}

func rangeFromSpanInText(span publiccompiler.Span, text string) Range {
	start, startOK := positionAtUTF16Offset(text, span.StartOffset)
	end, endOK := positionAtUTF16Offset(text, span.EndOffset)
	if !startOK || !endOK || comparePosition(end, start) < 0 {
		return rangeFromSpan(span)
	}
	return Range{Start: start, End: end}
}

func positionAtUTF16Offset(text string, target int) (Position, bool) {
	if target < 0 {
		return Position{}, false
	}
	position := Position{}
	offset := 0
	if target == 0 {
		return position, true
	}
	for _, r := range text {
		width := 1
		if r > 0xFFFF {
			width = 2
		}
		if offset+width > target {
			return Position{}, false
		}
		offset += width
		if r == '\n' {
			position.Line++
			position.Character = 0
		} else {
			position.Character += width
		}
		if offset == target {
			return position, true
		}
	}
	return position, offset == target
}

func positionInRange(position Position, target Range) bool {
	if comparePosition(position, target.Start) < 0 {
		return false
	}
	return comparePosition(position, target.End) < 0 || target.Start == target.End && position == target.Start
}

func comparePosition(left, right Position) int {
	if left.Line < right.Line {
		return -1
	}
	if left.Line > right.Line {
		return 1
	}
	if left.Character < right.Character {
		return -1
	}
	if left.Character > right.Character {
		return 1
	}
	return 0
}
