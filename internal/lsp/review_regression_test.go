// SPDX-License-Identifier: AGPL-3.0-or-later
package lsp

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestCodeActionsRejectAnalysisChangedDuringCompilation(t *testing.T) {
	const uri = "file:///workspace/main.cl"
	s := testServer(map[string]documentSnapshot{uri: {URI: uri, Text: `class Main { main():Object { missing }; };`, Open: true, Version: 1}})
	s.config.measurement = &measurementProbe{clock: func() time.Duration { return 0 }, record: func(stage string, _ time.Duration) {
		if stage == "index" {
			s.mu.Lock()
			s.invalidateLocked()
			s.mu.Unlock()
		}
	}}
	_, err := s.requestCodeActions(context.Background(), json.RawMessage(`{"textDocument":{"uri":"file:///workspace/main.cl"},"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":100}}}`))
	if err == nil || err.Code != jsonRPCContentModified {
		t.Fatalf("stale quick fixes: %+v", err)
	}
}

func TestSignatureModelReusedWithinAnalysisGeneration(t *testing.T) {
	const uri = "file:///workspace/main.cl"
	s := testServer(map[string]documentSnapshot{uri: {URI: uri, Text: `class Main { main():Object { self }; };`, Open: true, Version: 1}})
	value, err := s.analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	models := make(chan *signatureModel, 8)
	for i := 0; i < cap(models); i++ {
		go func() { models <- value.signatureModel() }()
	}
	first := <-models
	for i := 1; i < cap(models); i++ {
		if <-models != first {
			t.Fatal("concurrent requests rebuilt signature model")
		}
	}
	cached, err := s.analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cached.signatureModel() != first {
		t.Fatal("cached analysis lost signature model")
	}
	s.mu.Lock()
	s.invalidateLocked()
	s.mu.Unlock()
	next, err := s.analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if next.signatureModel() == first {
		t.Fatal("new generation retained signature model")
	}
}
