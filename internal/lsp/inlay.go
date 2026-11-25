// SPDX-License-Identifier: AGPL-3.0-or-later
package lsp

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
)

type inlayHintOptions struct{ ParameterNames, SelfType bool }
type inlayHintSettings struct {
	ParameterNames *bool `json:"parameterNames"`
	SelfType       *bool `json:"selfType"`
}

func (settings *inlayHintSettings) apply(options inlayHintOptions) inlayHintOptions {
	if settings == nil {
		return options
	}
	if settings.ParameterNames != nil {
		options.ParameterNames = *settings.ParameterNames
	}
	if settings.SelfType != nil {
		options.SelfType = *settings.SelfType
	}
	return options
}

type inlayHint struct {
	Position     Position `json:"position"`
	Label        string   `json:"label"`
	Kind         int      `json:"kind"`
	Tooltip      string   `json:"tooltip,omitempty"`
	PaddingLeft  bool     `json:"paddingLeft,omitempty"`
	PaddingRight bool     `json:"paddingRight,omitempty"`
}

func (s *Server) requestInlayHints(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	empty := []inlayHint{}
	if ctx.Err() != nil {
		return nil, &rpcError{Code: jsonRPCRequestCancel, Message: "Request cancelled"}
	}
	var params struct {
		TextDocument TextDocumentIdentifier `json:"textDocument"`
		Range        Range                  `json:"range"`
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
		return empty, nil
	}
	if comparePosition(params.Range.Start, params.Range.End) > 0 {
		return nil, &rpcError{Code: jsonRPCInvalidParams, Message: "Invalid hint range"}
	}
	if _, err := byteOffsetAt(doc.Text, params.Range.Start); err != nil {
		return nil, invalidParams(err)
	}
	// Clients may request a range ending after the final document line.
	s.mu.Lock()
	options := s.inlayHints
	current := value.Generation == s.generation
	s.mu.Unlock()
	if !current {
		return nil, &rpcError{Code: jsonRPCContentModified, Message: "Document changed"}
	}
	if value.Result.HasErrors() || (!options.ParameterNames && !options.SelfType) {
		return empty, nil
	}
	model := value.signatureModel()
	hints := []inlayHint{}
	add := func(hint inlayHint) {
		if len(hints) < 500 && positionInRange(hint.Position, params.Range) {
			hints = append(hints, hint)
		}
	}
	for n, ref := range value.Index.Nodes[params.TextDocument.URI] {
		if n%64 == 0 && ctx.Err() != nil {
			return nil, &rpcError{Code: jsonRPCRequestCancel, Message: "Request cancelled"}
		}
		node := ref.Node
		if node.Kind != "Dispatch" || node.Type == "" {
			continue
		}
		name := strings.TrimSuffix(node.Text, "(...)")
		if dot := strings.LastIndex(name, "."); dot >= 0 {
			name = name[dot+1:]
		}
		token := value.Index.tokenOutsideChildren(node, name)
		if token == nil {
			continue
		}
		methodRange := value.Index.rangeFromSpan(token.Span)
		item := value.Index.occurrenceExact(params.TextDocument.URI, methodRange)
		if item == nil {
			continue
		}
		decl := value.Index.ByID[item.DeclarationID]
		if decl == nil {
			continue
		}
		sig := model.lookup(decl.Owner, name)
		if sig == nil {
			continue
		}
		argument := 0
		for _, child := range node.Children {
			childRange := value.Index.rangeFromSpan(child.Span)
			if comparePosition(childRange.End, methodRange.Start) <= 0 {
				continue
			}
			if argument >= len(sig.params) {
				break
			}
			parameter := sig.params[argument].Label
			parameterName := strings.SplitN(parameter, ":", 2)[0]
			argument++
			if options.ParameterNames && !(child.Kind == "Identifier" && child.Name == parameterName) {
				add(inlayHint{Position: childRange.Start, Label: parameterName + ":", Kind: 2, PaddingRight: true, Tooltip: parameter + " — " + sig.owner + "." + sig.name})
			}
		}
		if options.SelfType && sig.result == "SELF_TYPE" {
			typ := node.Type
			if typ == "SELF_TYPE" {
				typ = ref.Class
			}
			add(inlayHint{Position: ref.Range.End, Label: ": " + typ, Kind: 1, PaddingLeft: true, Tooltip: "Static SELF_TYPE context: " + typ + ". This is not the runtime dynamic type."})
		}
	}
	if ctx.Err() != nil {
		return nil, &rpcError{Code: jsonRPCRequestCancel, Message: "Request cancelled"}
	}
	s.mu.Lock()
	current = value.Generation == s.generation
	s.mu.Unlock()
	if !current {
		return nil, &rpcError{Code: jsonRPCContentModified, Message: "Document changed"}
	}
	sort.SliceStable(hints, func(i, j int) bool {
		if order := comparePosition(hints[i].Position, hints[j].Position); order != 0 {
			return order < 0
		}
		return hints[i].Kind < hints[j].Kind
	})
	return hints, nil
}

func (s *Server) updateClientConfiguration(ctx context.Context, raw json.RawMessage) error {
	var params struct {
		Settings struct {
			Cooler struct {
				SourceSet    json.RawMessage    `json:"sourceSet"`
				WarningRules *string            `json:"warningRules"`
				InlayHints   *inlayHintSettings `json:"inlayHints"`
			} `json:"cooler"`
		} `json:"settings"`
	}
	if err := decodeParams(raw, &params); err != nil {
		return err
	}
	change := params.Settings.Cooler
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(change.SourceSet) == 0 && change.WarningRules == nil && change.InlayHints == nil {
		return nil
	}
	s.mu.Lock()
	selection, warnings := s.sourceSet, s.warningRules
	s.mu.Unlock()
	if len(change.SourceSet) > 0 {
		selection = nil // Decode a replacement without mutating published configuration.
		if err := json.Unmarshal(change.SourceSet, &selection); err != nil {
			return err
		}
	}
	if change.WarningRules != nil {
		warnings = *change.WarningRules
	}
	if len(change.SourceSet) == 0 && change.WarningRules == nil && change.InlayHints != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		s.inlayHints = change.InlayHints.apply(s.inlayHints)
		s.invalidateLocked()
		s.scheduleDiagnosticsLockedContext(ctx)
		return nil
	}
	return s.changeConfiguration(ctx, selection, warnings, change.InlayHints)
}
