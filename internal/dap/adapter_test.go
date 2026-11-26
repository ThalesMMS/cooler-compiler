// SPDX-License-Identifier: AGPL-3.0-or-later
package dap

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type client struct {
	t        *testing.T
	in       *io.PipeWriter
	messages chan object
	done     chan error
	seq      int
	events   []object
}

func newClient(t *testing.T) *client {
	t.Helper()
	input, writer := io.Pipe()
	reader, output := io.Pipe()
	c := &client{t: t, in: writer, messages: make(chan object, 128), done: make(chan error, 1)}
	go func() { c.done <- Serve(context.Background(), input, output); _ = output.Close() }()
	go func() {
		r := bufio.NewReader(reader)
		for {
			body, err := readMessage(r)
			if err != nil {
				return
			}
			var message object
			if json.Unmarshal(body, &message) != nil {
				return
			}
			c.messages <- message
		}
	}()
	t.Cleanup(func() {
		_ = writer.Close()
		select {
		case err := <-c.done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("adapter leaked")
		}
		_ = reader.Close()
	})
	return c
}
func (c *client) next() object {
	c.t.Helper()
	select {
	case message := <-c.messages:
		return message
	case <-time.After(3 * time.Second):
		c.t.Fatalf("DAP timed out; queued=%#v", c.events)
		return nil
	}
}
func (c *client) request(command string, args any) object {
	c.t.Helper()
	c.seq++
	body, _ := json.Marshal(object{"seq": c.seq, "type": "request", "command": command, "arguments": args})
	if _, err := fmt.Fprintf(c.in, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
		c.t.Fatal(err)
	}
	for {
		message := c.next()
		if message["type"] == "event" {
			c.events = append(c.events, message)
			continue
		}
		if message["request_seq"] != float64(c.seq) {
			c.t.Fatal(message)
		}
		return message
	}
}
func (c *client) ok(command string, args any) object {
	c.t.Helper()
	message := c.request(command, args)
	if message["success"] != true {
		c.t.Fatal(message)
	}
	body, _ := message["body"].(map[string]any)
	return body
}
func (c *client) event(name string) object {
	c.t.Helper()
	for {
		for index, message := range c.events {
			if message["event"] == name {
				c.events = append(c.events[:index], c.events[index+1:]...)
				body, _ := message["body"].(map[string]any)
				return body
			}
		}
		c.events = append(c.events, c.next())
	}
}
func source(t *testing.T, dir, name, text string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func (c *client) launch(paths []string, extra object) {
	c.t.Helper()
	caps := c.ok("initialize", object{"pathFormat": "path", "linesStartAt1": true, "columnsStartAt1": true})
	if caps["supportsStepBack"] != false {
		c.t.Fatal(caps)
	}
	args := object{"sources": paths}
	for key, value := range extra {
		args[key] = value
	}
	c.ok("launch", args)
	c.event("initialized")
}
func TestFullSessionMultifileInput(t *testing.T) {
	dir := t.TempDir()
	main := source(t, dir, "main.cl", "class Main inherits IO {\n main():Object { {\n out_int((new Helper).inc(7));\n out_int(in_int());\n } };\n};")
	helper := source(t, dir, "helper.cl", "class Helper {\n inc(n:Int):Int { n+1 };\n};")
	c := newClient(t)
	c.launch([]string{main, helper}, nil)
	points := c.ok("setBreakpoints", object{"source": object{"path": helper}, "breakpoints": []any{object{"line": 2}}})
	if points["breakpoints"].([]any)[0].(map[string]any)["verified"] != true {
		t.Fatal(points)
	}
	c.ok("configurationDone", nil)
	if stop := c.event("stopped"); stop["reason"] != "breakpoint" {
		t.Fatal(stop)
	}
	stack := c.ok("stackTrace", object{"threadId": 1})
	frames := stack["stackFrames"].([]any)
	if len(frames) < 2 {
		t.Fatal(stack)
	}
	top := frames[0].(map[string]any)
	if top["source"].(map[string]any)["path"] != helper {
		t.Fatal(top)
	}
	id := top["id"]
	scopes := c.ok("scopes", object{"frameId": id})
	ref := scopes["scopes"].([]any)[0].(map[string]any)["variablesReference"]
	vars := c.ok("variables", object{"variablesReference": ref})
	encoded, _ := json.Marshal(vars)
	if !strings.Contains(string(encoded), `"name":"n"`) {
		t.Fatalf("locals: %s", encoded)
	}
	c.ok("stepIn", object{"threadId": 1})
	c.event("stopped")
	if c.request("variables", object{"variablesReference": ref})["success"] != false {
		t.Fatal("stale reference accepted")
	}
	c.ok("setBreakpoints", object{"source": object{"path": helper}, "breakpoints": []any{}})
	c.ok("continue", object{"threadId": 1})
	stop := c.event("stopped")
	if !strings.Contains(fmt.Sprint(stop["text"]), "Input required") {
		t.Fatal(stop)
	}
	if c.request("evaluate", object{"expression": "n", "context": "watch"})["success"] != false {
		t.Fatal("watch advertised")
	}
	c.ok("evaluate", object{"expression": ":input 42", "context": "repl"})
	c.event("stopped")
	c.ok("continue", object{"threadId": 1})
	c.event("terminated")
	output := ""
	for _, event := range c.events {
		if event["event"] == "output" {
			body := event["body"].(map[string]any)
			if body["category"] == "stdout" {
				output += fmt.Sprint(body["output"])
			}
		}
	}
	if output != "842" {
		t.Fatalf("output %q", output)
	}
	c.ok("disconnect", nil)
}
func TestRuntimeLimitPauseAndDisconnect(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		limit      int
		reason     string
	}{{"runtime", "1/0", 0, "exception"}, {"limit", "while true loop 0 pool", 5, "pause"}} {
		t.Run(tc.name, func(t *testing.T) {
			path := source(t, t.TempDir(), "main.cl", "class Main { main():Object { "+tc.code+" }; };")
			c := newClient(t)
			c.launch([]string{path}, object{"maxSteps": tc.limit})
			c.ok("configurationDone", nil)
			if stop := c.event("stopped"); stop["reason"] != tc.reason {
				t.Fatal(stop)
			}
			c.ok("stackTrace", nil)
			c.ok("disconnect", nil)
		})
	}
	t.Run("pause", func(t *testing.T) {
		path := source(t, t.TempDir(), "main.cl", "class Main { main():Object { while true loop 0 pool }; };")
		c := newClient(t)
		c.launch([]string{path}, object{"stopOnEntry": true, "maxSteps": 4096})
		c.ok("configurationDone", nil)
		c.event("stopped")
		c.ok("continue", nil)
		c.request("pause", nil)
		c.event("stopped")
		stack := c.ok("stackTrace", nil)
		if len(stack["stackFrames"].([]any)) == 0 {
			t.Fatal("cancel lost frames")
		}
		c.ok("continue", nil)
		if stop := c.event("stopped"); stop["reason"] != "pause" {
			t.Fatal("paused execution did not resume", stop)
		}
		c.ok("disconnect", nil)
	})
}
func TestLaunchBoundsAndErrors(t *testing.T) {
	c := newClient(t)
	c.ok("initialize", nil)
	if c.request("launch", object{"sources": make([]string, 65)})["success"] != false {
		t.Fatal("source bound")
	}
	path := source(t, t.TempDir(), "main.cl", "class Main {")
	if c.request("launch", object{"program": path})["success"] != false {
		t.Fatal("invalid program accepted")
	}
	c.ok("disconnect", nil)
}

func TestDisplayBounds(t *testing.T) {
	text := strings.Repeat("😀", 400)
	bounded := boundedText(text, 1023)
	if len(bounded) > 1026 || !strings.HasSuffix(bounded, "…") {
		t.Fatal("unbounded display")
	}
	if boundedText("short", 20) != "short" {
		t.Fatal("short display changed")
	}
}

func TestSymlinkedSourceKeepsClientPath(t *testing.T) {
	real := t.TempDir()
	linked := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(real, linked); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	source(t, real, "main.cl", "class Main inherits IO {\n main():Object {\n out_int(1)\n };\n};")
	main := filepath.Join(linked, "main.cl")
	c := newClient(t)
	c.launch([]string{main}, nil)
	points := c.ok("setBreakpoints", object{"source": object{"path": main}, "breakpoints": []any{object{"line": 3}}})
	point := points["breakpoints"].([]any)[0].(map[string]any)
	if point["verified"] != true || point["source"].(map[string]any)["path"] != main {
		t.Fatal(points)
	}
	c.ok("configurationDone", nil)
	if stop := c.event("stopped"); stop["reason"] != "breakpoint" {
		t.Fatal(stop)
	}
	frames := c.ok("stackTrace", object{"threadId": 1})["stackFrames"].([]any)
	if path := frames[0].(map[string]any)["source"].(map[string]any)["path"]; path != main {
		t.Fatalf("stack frame path %v, want the launched path %s", path, main)
	}
}
