// SPDX-License-Identifier: AGPL-3.0-or-later
package dap

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ThalesMMS/cooler-compiler/compiler"
)

func (a *adapter) setBreakpoints(req request) {
	var args struct {
		Source      struct{ Path string }
		Breakpoints []struct {
			Line         int
			Column       int
			Condition    string
			HitCondition string
			LogMessage   string
		}
	}
	if err := decode(req, &args); err != nil {
		a.reply(req, nil, err)
		return
	}
	path, err := filepath.Abs(args.Source.Path)
	if err == nil {
		path, err = filepath.EvalSymlinks(path)
	}
	known := false
	for _, source := range a.project.Sources {
		if source.ID == path {
			known = true
			break
		}
	}
	if err != nil || !known {
		a.reply(req, nil, fmt.Errorf("breakpoint source is not in the launch project"))
		return
	}
	count := len(args.Breakpoints)
	for id, points := range a.breakpoints {
		if id != path {
			count += len(points)
		}
	}
	if count > 256 {
		a.reply(req, nil, fmt.Errorf("at most 256 breakpoints"))
		return
	}
	points := []compiler.Breakpoint{}
	result := []any{}
	for _, point := range args.Breakpoints {
		line := 0
		if point.Line > 0 && point.Condition == "" && point.HitCondition == "" && point.LogMessage == "" && point.Column == 0 {
			for _, instruction := range a.ir {
				span := instruction.Span
				if span.SourceID != path || span.StartLine < point.Line || strings.HasSuffix(instruction.Method, ".$init") || instruction.Method == "$program.$entry" {
					continue
				}
				if line == 0 || span.StartLine < line {
					line = span.StartLine
				}
			}
		}
		item := object{"verified": line > 0, "line": point.Line, "source": object{"path": path, "name": filepath.Base(path)}}
		if line > 0 {
			item["line"] = line
			points = append(points, compiler.Breakpoint{ID: fmt.Sprintf("%s:%d", path, line), SourceID: path, Line: line})
		} else {
			item["message"] = "No executable line at or after this position, or unsupported breakpoint options."
		}
		result = append(result, item)
	}
	a.breakpoints[path] = points
	a.reply(req, object{"breakpoints": result}, nil)
}
func (a *adapter) frameID(index int) int { return a.generation*1024 + index + 1 }
func (a *adapter) frameIndex(id int) (int, error) {
	index := id - a.generation*1024 - 1
	if index < 0 || index >= len(a.snapshot.Runtime.Frames) {
		return 0, fmt.Errorf("stale or invalid frame reference")
	}
	return index, nil
}
func (a *adapter) stackTrace(req request) {
	var args struct {
		ThreadID   int
		StartFrame int
		Levels     int
	}
	if err := decode(req, &args); err != nil || args.StartFrame < 0 || args.Levels < 0 || args.ThreadID != 0 && args.ThreadID != 1 {
		a.reply(req, nil, fmt.Errorf("invalid stack request"))
		return
	}
	frames := a.snapshot.Runtime.Frames
	items := []any{}
	end := len(frames)
	if args.Levels > 0 && args.Levels < end-args.StartFrame {
		end = args.StartFrame + args.Levels
	}
	for position := args.StartFrame; position < end; position++ {
		index := len(frames) - 1 - position
		frame := frames[index]
		span := frame.Span
		if position == 0 && a.snapshot.Location != nil {
			span = a.snapshot.Location.Span
		}
		item := object{"id": a.frameID(index), "name": boundedText(frame.FunctionName, 256), "line": max(1, span.StartLine), "column": max(1, span.StartColumn)}
		if span.SourceID != "" {
			item["source"] = object{"name": filepath.Base(span.SourceID), "path": span.SourceID}
		}
		items = append(items, item)
	}
	a.reply(req, object{"stackFrames": items, "totalFrames": len(frames)}, nil)
}
func (a *adapter) scopes(req request) {
	var args struct{ FrameID int }
	err := decode(req, &args)
	if err == nil {
		_, err = a.frameIndex(args.FrameID)
	}
	a.reply(req, object{"scopes": []any{object{"name": "Locals and self", "variablesReference": args.FrameID, "expensive": false}}}, err)
}
func (a *adapter) variables(req request) {
	var args struct{ VariablesReference int }
	if err := decode(req, &args); err != nil {
		a.reply(req, nil, err)
		return
	}
	index, err := a.frameIndex(args.VariablesReference)
	if err != nil {
		a.reply(req, nil, err)
		return
	}
	frame := a.snapshot.Runtime.Frames[index]
	names := []string{}
	for name := range frame.Locals {
		names = append(names, name)
	}
	sort.Strings(names)
	items := []any{variable("self", frame.Self)}
	for _, name := range names {
		if len(items) >= 513 {
			break
		}
		items = append(items, variable(name, frame.Locals[name]))
	}
	a.reply(req, object{"variables": items}, nil)
}
func variable(name string, value compiler.RuntimeValue) object {
	return object{"name": boundedText(name, 256), "value": boundedText(value.Display, 1024), "type": boundedText(value.Type, 128), "variablesReference": 0, "presentationHint": object{"attributes": []string{"readOnly"}}}
}
func (a *adapter) input(ctx context.Context, req request) {
	var args struct {
		Expression string
		Context    string
		Line       string
	}
	if err := decode(req, &args); err != nil {
		a.reply(req, nil, err)
		return
	}
	line := args.Line
	if req.Command == "evaluate" {
		if args.Context != "repl" || !strings.HasPrefix(args.Expression, ":input ") {
			a.reply(req, nil, fmt.Errorf("only Debug Console :input <line> is supported; watches are unavailable"))
			return
		}
		line = strings.TrimPrefix(args.Expression, ":input ")
	}
	if a.snapshot.Runtime.WaitingInput == nil {
		a.reply(req, nil, fmt.Errorf("program is not waiting for input"))
		return
	}
	operation, cancel := context.WithTimeout(ctx, 5*time.Second)
	a.cancel = cancel
	a.running = true
	debugger, sequence := a.debugger, a.snapshot.Runtime.WaitingInput.Sequence
	go func() {
		snapshot, err := debugger.ProvideInputLine(operation, line, sequence)
		cancel()
		a.done <- completion{request: req, snapshot: snapshot, err: err, input: true}
	}()
}

func boundedText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	end := limit
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end] + "…"
}
