// SPDX-License-Identifier: AGPL-3.0-or-later

package wasmbridge

import (
	"context"
	"strings"
	"testing"

	api "github.com/ThalesMMS/cooler-compiler/compiler"
)

func TestBridgeInteractiveLifecycle(t *testing.T) {
	bridge := &Bridge{}
	t.Cleanup(func() {
		if err := bridge.Close(); err != nil {
			t.Error(err)
		}
	})
	compile := Request{ID: 1, Action: "compile", Sources: []api.SourceUnit{{ID: "main", Name: "main.cl", Content: `class Main inherits IO {main():Object {out_string(in_string())};};`}}}
	if response := bridge.Handle(context.Background(), compile); response.Error != "" || response.State.Blocked {
		t.Fatalf("compile: %+v", response)
	}
	response := bridge.Handle(context.Background(), Request{ID: 2, Action: "run", Steps: 100})
	if response.Error != "" || response.State.Runtime.WaitingInput == nil {
		t.Fatalf("waiting: %+v", response)
	}
	sequence := response.State.Runtime.WaitingInput.Sequence
	if response := bridge.Handle(context.Background(), Request{Action: "input", Sequence: sequence + 1, Line: "bad"}); response.Error == "" {
		t.Fatal("accepted stale input")
	}
	if response := bridge.Handle(context.Background(), Request{Action: "input", Sequence: sequence, Line: "hello"}); response.Error != "" {
		t.Fatal(response.Error)
	}
	response = bridge.Handle(context.Background(), Request{Action: "run", Steps: 100})
	if response.Error != "" || !response.State.Runtime.Halted || strings.Join(response.State.Runtime.Output, "") != "hello" {
		t.Fatalf("run: %+v", response)
	}
	bridge.Handle(context.Background(), compile)
	response = bridge.Handle(context.Background(), Request{Action: "run", Steps: 100})
	if response.State.Runtime.WaitingInput == nil || len(response.State.Runtime.Output) != 0 {
		t.Fatal("compile retained previous input/output")
	}
}

func TestBridgeBounds(t *testing.T) {
	bridge := &Bridge{}
	t.Cleanup(func() {
		if err := bridge.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, request := range []Request{{Action: "unknown"}, {Action: "run", Steps: 10001}, {Action: "compile", Sources: []api.SourceUnit{{ID: "x", Content: strings.Repeat("x", 65537)}}}} {
		if response := bridge.Handle(context.Background(), request); response.Error == "" {
			t.Fatalf("accepted %+v", request)
		}
	}
}
