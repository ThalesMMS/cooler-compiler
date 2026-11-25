// SPDX-License-Identifier: AGPL-3.0-or-later

// Package wasmbridge is an experimental memory-only adapter, not a public API.
package wasmbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"

	api "github.com/ThalesMMS/cooler-compiler/compiler"
)

type Request struct {
	ID       int              `json:"id"`
	Action   string           `json:"action"`
	Sources  []api.SourceUnit `json:"sources,omitempty"`
	Input    string           `json:"input,omitempty"`
	Steps    int              `json:"steps,omitempty"`
	Line     string           `json:"line,omitempty"`
	Sequence int              `json:"sequence,omitempty"`
}

type State struct {
	Blocked     bool              `json:"blocked"`
	StopReason  api.StopReason    `json:"stopReason"`
	Diagnostics []api.Diagnostic  `json:"diagnostics"`
	Runtime     api.RuntimeResult `json:"runtime"`
}

type Response struct {
	ID            int    `json:"id"`
	Error         string `json:"error,omitempty"`
	State         *State `json:"state,omitempty"`
	HeapBytes     uint64 `json:"heapBytes"`
	GoSystemBytes uint64 `json:"goSystemBytes"`
}

type Bridge struct {
	mu          sync.Mutex
	compilation *api.Compilation
	debugger    *api.DebugSession
}

func (b *Bridge) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.close()
}

func (b *Bridge) close() error {
	var err error
	if b.debugger != nil {
		err = b.debugger.Close()
		b.debugger = nil
	}
	if b.compilation != nil {
		err = errors.Join(err, b.compilation.Close())
		b.compilation = nil
	}
	return err
}

func (b *Bridge) HandleJSON(ctx context.Context, text string) string {
	response := Response{}
	if len(text) > 128<<10 {
		response.Error = "request exceeds 128 KiB"
	} else {
		var request Request
		decoder := json.NewDecoder(strings.NewReader(text))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			response.Error = err.Error()
		} else if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
			response.Error = "expected one JSON request"
		} else {
			response = b.Handle(ctx, request)
		}
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return `{"error":"response encoding failed"}`
	}
	return string(encoded)
}

func (b *Bridge) Handle(ctx context.Context, request Request) (response Response) {
	b.mu.Lock()
	defer b.mu.Unlock()
	response.ID = request.ID
	defer func() {
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		response.HeapBytes = stats.HeapAlloc
		response.GoSystemBytes = stats.Sys
	}()
	if err := ctx.Err(); err != nil {
		response.Error = err.Error()
		return response
	}
	if request.Steps < 0 || request.Steps > 10000 || len(request.Input) > 16<<10 || len(request.Line) > 16<<10 {
		response.Error = "use at most 10000 steps and 16 KiB of input"
		return response
	}
	if request.Steps == 0 {
		request.Steps = 1000
	}
	if request.Action == "compile" {
		state, err := b.compile(ctx, request)
		response.State = state
		if err != nil {
			response.Error = err.Error()
		}
		return response
	}
	if request.Action == "dispose" {
		if err := b.close(); err != nil {
			response.Error = err.Error()
		}
		return response
	}
	if b.debugger == nil {
		response.Error = "compile a valid program first"
		return response
	}
	var snapshot api.DebugSnapshot
	var err error
	switch request.Action {
	case "run":
		snapshot, err = b.debugger.Run(ctx, request.Steps, nil)
	case "input":
		snapshot, err = b.debugger.ProvideInputLine(ctx, request.Line, request.Sequence)
	default:
		err = fmt.Errorf("unknown action %q", request.Action)
	}
	if err != nil {
		response.Error = err.Error()
		return response
	}
	response.State = &State{StopReason: snapshot.StopReason, Diagnostics: snapshot.Result.Diagnostics(), Runtime: snapshot.Result.Runtime()}
	return response
}

func (b *Bridge) compile(ctx context.Context, request Request) (*State, error) {
	if len(request.Sources) < 1 || len(request.Sources) > 8 {
		return nil, errors.New("provide 1 through 8 in-memory sources")
	}
	total := 0
	for _, source := range request.Sources {
		total += len(source.Content)
		if total > 64<<10 {
			return nil, errors.New("sources exceed 64 KiB")
		}
	}
	if err := b.close(); err != nil {
		return nil, err
	}
	options := api.DefaultOptions()
	options.MaxSteps = 1000
	options.Limits.MaxInstructionsPerRun = 10000
	options.Limits.MaxInstructionsTotal = 100000
	options.Limits.MaxObjectAllocations = 1000
	options.Limits.MaxCallFrameDepth = 128
	options.Limits.MaxTraceEntries = 64
	options.Limits.MaxTraceBytes = 1 << 20
	options.Limits.MaxInputBytes = 16 << 10
	options.Limits.MaxOutputBytes = 16 << 10
	options.Limits.MaxStringBytes = 16 << 10
	session, err := api.NewSession(ctx, api.Project{Sources: request.Sources}, options)
	if err != nil {
		return nil, err
	}
	b.compilation = session
	snapshot, err := session.Complete(ctx)
	if err != nil {
		return nil, err
	}
	state := &State{Blocked: snapshot.Blocked, StopReason: api.StopReady, Diagnostics: snapshot.Result.Diagnostics(), Runtime: snapshot.Result.Runtime()}
	if snapshot.Blocked {
		return state, nil
	}
	debugger, err := session.Debugger(ctx)
	if err != nil {
		return nil, err
	}
	if err := debugger.SetInput(ctx, request.Input); err != nil {
		return nil, errors.Join(err, debugger.Close())
	}
	if err := debugger.SetInteractiveInput(ctx, true); err != nil {
		return nil, errors.Join(err, debugger.Close())
	}
	b.debugger = debugger
	return state, nil
}
