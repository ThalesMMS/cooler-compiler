// SPDX-License-Identifier: AGPL-3.0-or-later
// Package dap adapts the public compiler debugger to a bounded local DAP session.
package dap

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ThalesMMS/cooler-compiler/compiler"
)

type object = map[string]any
type request struct {
	Seq       int             `json:"seq"`
	Type      string          `json:"type"`
	Command   string          `json:"command"`
	Arguments json.RawMessage `json:"arguments"`
}
type incoming struct {
	request request
	err     error
}
type completion struct {
	request  request
	debugger *compiler.DebugSession
	snapshot compiler.DebugSnapshot
	err      error
	launch   bool
	input    bool
}
type adapter struct {
	pauseRequested atomic.Bool
	ir             []compiler.IRInstruction
	writer         io.Writer
	sequence       int
	writeErr       error
	debugger       *compiler.DebugSession
	snapshot       compiler.DebugSnapshot
	project        compiler.Project
	initialized    bool
	configured     bool
	running        bool
	stopOnEntry    bool
	terminated     bool
	cancel         context.CancelFunc
	done           chan completion
	breakpoints    map[string][]compiler.Breakpoint
	generation     int
	outputBytes    int
	maxSteps       int
}

// Serve owns and closes input. Standard output contains protocol frames only.
func Serve(ctx context.Context, input io.ReadCloser, output io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() { _ = input.Close() }()
	messages := make(chan incoming, 1)
	go func() {
		reader := bufio.NewReaderSize(input, 8192)
		for {
			body, err := readMessage(reader)
			var req request
			if err == nil {
				err = json.Unmarshal(body, &req)
			}
			select {
			case messages <- incoming{req, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	a := &adapter{writer: output, done: make(chan completion, 1), breakpoints: make(map[string][]compiler.Breakpoint), maxSteps: 100000}
	defer func() {
		if a.cancel != nil {
			a.cancel()
		}
		if a.running {
			result := <-a.done
			if result.debugger != nil {
				_ = result.debugger.Close()
			}
		}
		if a.debugger != nil {
			_ = a.debugger.Close()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case message := <-messages:
			if message.err != nil {
				if errors.Is(message.err, io.EOF) {
					return nil
				}
				return message.err
			}
			if message.request.Type != "request" {
				return fmt.Errorf("expected DAP request")
			}
			if a.handle(ctx, message.request) {
				return a.writeErr
			}
		case result := <-a.done:
			a.complete(ctx, result)
		}
		if a.writeErr != nil {
			return a.writeErr
		}
	}
}
func (a *adapter) send(message object) {
	if a.writeErr != nil {
		return
	}
	a.sequence++
	message["seq"] = a.sequence
	body, err := json.Marshal(message)
	if err != nil {
		a.writeErr = err
		return
	}
	_, a.writeErr = fmt.Fprintf(a.writer, "Content-Length: %d\r\n\r\n%s", len(body), body)
}
func (a *adapter) reply(req request, body any, err error) {
	message := object{"type": "response", "request_seq": req.Seq, "command": req.Command, "success": err == nil}
	if err != nil {
		message["message"] = err.Error()
	} else if body != nil {
		message["body"] = body
	}
	a.send(message)
}
func (a *adapter) event(name string, body any) {
	a.send(object{"type": "event", "event": name, "body": body})
}
func decode(req request, value any) error {
	if len(req.Arguments) == 0 {
		return nil
	}
	return json.Unmarshal(req.Arguments, value)
}
func (a *adapter) handle(ctx context.Context, req request) bool {
	if req.Command == "disconnect" || req.Command == "terminate" {
		if a.cancel != nil {
			a.cancel()
		}
		a.reply(req, nil, nil)
		a.end(0)
		// The client owns the adapter process and closes its transport after
		// receiving this response. Exiting first races VS Code's teardown.
		return false
	}
	if req.Command == "initialize" {
		if a.initialized {
			a.reply(req, nil, fmt.Errorf("already initialized"))
			return false
		}
		var args struct {
			PathFormat      string
			LinesStartAt1   *bool
			ColumnsStartAt1 *bool
		}
		err := decode(req, &args)
		if err == nil && ((args.PathFormat != "" && args.PathFormat != "path") || (args.LinesStartAt1 != nil && !*args.LinesStartAt1) || (args.ColumnsStartAt1 != nil && !*args.ColumnsStartAt1)) {
			err = fmt.Errorf("use native paths and one-based lines/columns")
		}
		a.initialized = err == nil
		a.reply(req, object{"supportsConfigurationDoneRequest": true, "supportsTerminateRequest": true, "supportsEvaluateForHovers": false, "supportsStepBack": false, "supportsSetVariable": false, "exceptionBreakpointFilters": []any{}}, err)
		return false
	}
	if !a.initialized {
		a.reply(req, nil, fmt.Errorf("initialize first"))
		return false
	}
	if req.Command == "launch" {
		a.launch(ctx, req)
		return false
	}
	if req.Command == "pause" {
		if a.running && a.debugger != nil {
			a.pauseRequested.Store(true)
			a.reply(req, nil, nil)
		} else {
			a.reply(req, nil, fmt.Errorf("no active execution"))
		}
		return false
	}
	if req.Command == "cancel" {
		if a.running && a.cancel != nil {
			a.cancel()
			a.reply(req, nil, nil)
		} else {
			a.reply(req, nil, fmt.Errorf("no active execution"))
		}
		return false
	}
	if req.Command == "threads" {
		threads := []any{}
		if a.debugger != nil && !a.terminated {
			threads = append(threads, object{"id": 1, "name": "COOL VM"})
		}
		a.reply(req, object{"threads": threads}, nil)
		return false
	}
	if req.Command == "stackTrace" && (a.running || a.terminated) {
		a.reply(req, object{"stackFrames": []any{}, "totalFrames": 0}, nil)
		return false
	}
	if a.debugger == nil || a.terminated {
		a.reply(req, nil, fmt.Errorf("no active debug session"))
		return false
	}
	if a.running {
		a.reply(req, nil, fmt.Errorf("pause execution before this request"))
		return false
	}
	switch req.Command {
	case "setBreakpoints":
		a.setBreakpoints(req)
	case "setExceptionBreakpoints":
		a.reply(req, object{"breakpoints": []any{}}, nil)
	case "configurationDone":
		if a.configured {
			a.reply(req, nil, fmt.Errorf("already configured"))
			break
		}
		a.configured = true
		a.reply(req, nil, nil)
		if a.stopOnEntry {
			a.stopped("entry", "")
		} else {
			a.execute(ctx, request{Command: "continue"}, false)
		}
	case "continue", "next", "stepIn", "stepOut":
		if !a.configured {
			a.reply(req, nil, fmt.Errorf("configurationDone required"))
			break
		}
		if a.snapshot.Runtime.Halted {
			a.reply(req, nil, nil)
			code := 0
			if len(a.snapshot.Runtime.Diagnostics) > 0 {
				code = 1
			}
			a.end(code)
			break
		}
		a.execute(ctx, req, true)
	case "stackTrace":
		a.stackTrace(req)
	case "scopes":
		a.scopes(req)
	case "variables":
		a.variables(req)
	case "evaluate", "cooler/input":
		a.input(ctx, req)
	default:
		a.reply(req, nil, fmt.Errorf("unsupported request: %s", req.Command))
	}
	return false
}
func (a *adapter) launch(ctx context.Context, req request) {
	if a.debugger != nil || a.running || a.terminated {
		a.reply(req, nil, fmt.Errorf("one launch per adapter"))
		return
	}
	var args struct {
		Program     string
		Sources     []string
		Cwd         string
		Input       string
		StopOnEntry bool
		MaxSteps    int
	}
	if err := decode(req, &args); err != nil {
		a.reply(req, nil, err)
		return
	}
	paths := args.Sources
	if len(paths) == 0 && args.Program != "" {
		paths = []string{args.Program}
	}
	if len(paths) == 0 || len(paths) > 64 {
		a.reply(req, nil, fmt.Errorf("launch requires 1–64 source paths"))
		return
	}
	if args.MaxSteps < 0 || args.MaxSteps > 100000 {
		a.reply(req, nil, fmt.Errorf("maxSteps must be 1–100000, or zero for default"))
		return
	}
	if args.MaxSteps > 0 {
		a.maxSteps = args.MaxSteps
	}
	project := compiler.Project{}
	seen := map[string]bool{}
	total := 0
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			path = filepath.Join(args.Cwd, path)
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			a.reply(req, nil, err)
			return
		}
		// The resolved path identifies the source (so two spellings of one
		// file are the same source); the client's own path is what the
		// adapter reports back, so editors match it to their open documents.
		requested := absolute
		absolute, err = filepath.EvalSymlinks(absolute)
		if err != nil {
			a.reply(req, nil, err)
			return
		}
		if seen[absolute] {
			a.reply(req, nil, fmt.Errorf("duplicate source"))
			return
		}
		seen[absolute] = true
		info, err := os.Stat(absolute)
		if err != nil || !info.Mode().IsRegular() {
			a.reply(req, nil, fmt.Errorf("source must be a regular file"))
			return
		}
		file, err := os.Open(absolute)
		if err != nil {
			a.reply(req, nil, err)
			return
		}
		info, err = file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			_ = file.Close()
			a.reply(req, nil, fmt.Errorf("source must be a regular file"))
			return
		}
		content, err := io.ReadAll(io.LimitReader(file, maxMessage+1))
		_ = file.Close()
		total += len(content)
		if err != nil || total > maxMessage {
			a.reply(req, nil, fmt.Errorf("source project exceeds 1 MiB or could not be read"))
			return
		}
		project.Sources = append(project.Sources, compiler.SourceUnit{ID: absolute, Name: requested, Content: string(content)})
	}
	a.project = project
	a.stopOnEntry = args.StopOnEntry
	a.running = true
	operation, cancel := context.WithTimeout(ctx, 10*time.Second)
	a.cancel = cancel
	go func() {
		debugger, err := compiler.NewDebugSession(operation, project, compiler.Options{Input: args.Input, Limits: compiler.Limits{MaxInstructionsPerRun: 100000, MaxInstructionsTotal: 1000000, MaxOutputBytes: 65536, MaxTraceEntries: 100, MaxTraceBytes: 65536, MaxCallFrameDepth: 128, MaxBreakpoints: 256}})
		var snapshot compiler.DebugSnapshot
		if err == nil {
			err = debugger.SetInteractiveInput(operation, true)
		}
		if err == nil {
			snapshot, err = debugger.Snapshot(operation, false, 0)
		}
		if err == nil {
			for _, diagnostic := range snapshot.Result.Diagnostics() {
				if diagnostic.Severity == compiler.SeverityError {
					err = fmt.Errorf("compilation failed: %s: %s", diagnostic.Code, diagnostic.Message)
					break
				}
			}
		}
		cancel()
		result := completion{request: req, debugger: debugger, snapshot: snapshot, err: err, launch: true}
		a.done <- result
	}()
}
func (a *adapter) complete(ctx context.Context, result completion) {
	a.running = false
	a.cancel = nil
	if a.terminated {
		if result.debugger != nil {
			_ = result.debugger.Close()
		}
		return
	}
	if result.launch {
		if result.err != nil {
			if result.debugger != nil {
				_ = result.debugger.Close()
			}
			a.reply(result.request, nil, result.err)
			return
		}
		a.debugger = result.debugger
		a.snapshot = result.snapshot
		a.ir = result.snapshot.Result.IR()
		a.generation++
		a.reply(result.request, nil, nil)
		a.event("initialized", object{})
		return
	}
	if result.input {
		if result.err != nil {
			a.reply(result.request, nil, result.err)
			return
		}
		a.snapshot = result.snapshot
		a.generation++
		a.flushOutput()
		a.reply(result.request, object{"result": "Input accepted. Continue to resume.", "variablesReference": 0}, nil)
		a.stopped("step", "Input accepted. Continue to resume.")
		return
	}
	a.snapshot = result.snapshot
	a.generation++
	a.flushOutput()
	if result.err != nil && !errors.Is(result.err, context.Canceled) && !errors.Is(result.err, context.DeadlineExceeded) {
		a.event("output", object{"category": "stderr", "output": result.err.Error() + "\n"})
	}
	switch {
	case a.snapshot.StopReason == compiler.StopCancelled:
		a.stopped("exception", diagnosticsText(a.snapshot.Runtime.Diagnostics))
	case a.snapshot.StopReason == compiler.StopRuntimeError:
		a.stopped("exception", diagnosticsText(a.snapshot.Runtime.Diagnostics))
	case a.snapshot.Runtime.Halted:
		a.end(0)
	case a.pauseRequested.Load():
		a.stopped("pause", "Paused. Continue to resume.")
	case a.snapshot.Runtime.WaitingInput != nil:
		a.stopped("pause", "Input required: enter :input followed by a line in Debug Console, then Continue.")
	case a.snapshot.StopReason == compiler.StopBreakpoint:
		a.stopped("breakpoint", "")
	case result.err != nil || a.snapshot.StopReason == compiler.StopInstructionLimit:
		a.stopped("pause", "Execution paused or reached the instruction/time budget.")
	default:
		a.stopped("step", "")
	}
}
func (a *adapter) execute(ctx context.Context, req request, reply bool) {
	var args struct{ ThreadID int }
	if err := decode(req, &args); err != nil || args.ThreadID != 0 && args.ThreadID != 1 {
		if reply {
			a.reply(req, nil, fmt.Errorf("invalid thread"))
		}
		return
	}
	if a.snapshot.Runtime.WaitingInput != nil {
		if reply {
			a.reply(req, nil, fmt.Errorf("provide input before continuing"))
		}
		return
	}
	a.running = true
	a.pauseRequested.Store(false)
	operation, cancel := context.WithTimeout(ctx, 5*time.Second)
	a.cancel = cancel
	points := []compiler.Breakpoint{}
	keys := []string{}
	for key := range a.breakpoints {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		points = append(points, a.breakpoints[key]...)
	}
	if reply {
		a.reply(req, object{"allThreadsContinued": true}, nil)
	}
	a.event("continued", object{"threadId": 1, "allThreadsContinued": true})
	debugger := a.debugger
	limit := a.maxSteps
	initial := a.snapshot
	go func() {
		snapshot := initial
		var err error
		switch req.Command {
		case "next":
			snapshot, err = debugger.StepOver(operation, limit, points)
		case "stepIn":
			snapshot, err = debugger.StepInto(operation, limit, points)
		case "stepOut":
			snapshot, err = debugger.StepOut(operation, limit, points)
		default:
			for remaining := limit; remaining > 0 && !a.pauseRequested.Load(); {
				chunk := min(remaining, 1024)
				snapshot, err = debugger.Run(operation, chunk, points)
				remaining -= chunk
				if err != nil || snapshot.StopReason != compiler.StopInstructionLimit {
					break
				}
			}
		}
		cancel()
		if err != nil && len(snapshot.Runtime.Frames) == 0 {
			recovery, stop := context.WithTimeout(ctx, 5*time.Second)
			if recovered, recoveryErr := debugger.Snapshot(recovery, true, 0); recoveryErr == nil {
				snapshot = recovered
			}
			stop()
		}
		a.done <- completion{snapshot: snapshot, err: err}
	}()
}
func (a *adapter) stopped(reason, text string) {
	a.event("stopped", object{"reason": reason, "description": text, "text": text, "threadId": 1, "allThreadsStopped": true})
}
func (a *adapter) end(code int) {
	if a.terminated {
		return
	}
	a.terminated = true
	a.event("exited", object{"exitCode": code})
	a.event("terminated", object{})
}
func (a *adapter) flushOutput() {
	output := strings.Join(a.snapshot.Runtime.Output, "")
	if len(output) > a.outputBytes {
		a.event("output", object{"category": "stdout", "output": output[a.outputBytes:]})
		a.outputBytes = len(output)
	}
}
func diagnosticsText(items []compiler.Diagnostic) string {
	parts := []string{}
	for _, item := range items {
		parts = append(parts, item.Code+": "+item.Message)
	}
	return strings.Join(parts, "\n")
}
