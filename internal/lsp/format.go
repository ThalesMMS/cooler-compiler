// SPDX-License-Identifier: AGPL-3.0-or-later
package lsp

import (
	"context"
	"encoding/json"
	"github.com/ThalesMMS/cooler-compiler/internal/formatter"
	"unicode/utf16"
)

func (s *Server) requestFormatting(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var params struct {
		TextDocument TextDocumentIdentifier `json:"textDocument"`
	}
	if err := decodeParams(raw, &params); err != nil {
		return nil, invalidParams(err)
	}
	if ctx.Err() != nil {
		return nil, &rpcError{Code: jsonRPCRequestCancel, Message: "Request cancelled"}
	}
	s.mu.Lock()
	doc := s.documents[params.TextDocument.URI]
	generation := s.generation
	if doc == nil || doc.Desynchronized {
		s.mu.Unlock()
		return []textEdit{}, nil
	}
	source := doc.Text
	s.mu.Unlock()
	text, err := formatter.Format(source)
	if err != nil {
		return nil, invalidParams(err)
	}
	if ctx.Err() != nil {
		return nil, &rpcError{Code: jsonRPCRequestCancel, Message: "Request cancelled"}
	}
	s.mu.Lock()
	current := generation == s.generation
	s.mu.Unlock()
	if !current {
		return nil, &rpcError{Code: jsonRPCContentModified, Message: "Document changed"}
	}
	if text == source {
		return []textEdit{}, nil
	}
	end, _ := positionAtUTF16Offset(source, len(utf16.Encode([]rune(source))))
	return []textEdit{{Range: Range{End: end}, NewText: text}}, nil
}
