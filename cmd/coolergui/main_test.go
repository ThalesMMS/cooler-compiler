// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
)

func TestBrowserCommandHelper(t *testing.T) {
	if os.Getenv("COOLER_BROWSER_COMMAND_HELPER") != "1" {
		return
	}
	if os.Getenv("COOLER_BROWSER_COMMAND_SLEEP") == "1" {
		time.Sleep(10 * time.Second)
	}
	os.Exit(0)
}

func TestStartAndReapWaitsForBrowserCommand(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBrowserCommandHelper$")
	command.Env = append(os.Environ(), "COOLER_BROWSER_COMMAND_HELPER=1")
	done, err := startAndReap(command, cancel)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("browser command was not reaped")
	}
	if command.ProcessState == nil || !command.ProcessState.Exited() {
		t.Fatalf("browser command state = %+v", command.ProcessState)
	}
}

func TestStartAndReapKillsTimedOutBrowserCommand(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBrowserCommandHelper$")
	command.Env = append(os.Environ(), "COOLER_BROWSER_COMMAND_HELPER=1", "COOLER_BROWSER_COMMAND_SLEEP=1")
	done, err := startAndReap(command, cancel)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("timed-out browser command error=%v context=%v", err, ctx.Err())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed-out browser command was not killed and reaped")
	}
	if command.ProcessState == nil || !command.ProcessState.Exited() {
		t.Fatalf("timed-out browser command state = %+v", command.ProcessState)
	}
}

func TestStartAndReapReportsStartFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(ctx, filepath.Join(t.TempDir(), "missing-browser-command"))
	if done, err := startAndReap(command, cancel); err == nil || done != nil {
		t.Fatalf("missing browser command done=%v err=%v", done, err)
	}
}

const apiProgram = `class Main inherits IO {
  main() : Object { out_string("ok\n") };
};`

const echoInputProgram = `class Main inherits IO {
  main() : Object {
    {
      out_string(in_string());
      out_string("\n");
    }
  };
};`

const sourceStepAPIProgram = `class Helper { twice(v : Int) : Int { v + v }; };
class Main inherits IO {
  main() : Object {{
    out_int((new Helper).twice(2));
    out_string("done\n");
  }};
};`

const testCapability = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func testSecurityPolicy() securityPolicy {
	return securityPolicy{capability: testCapability}
}

func testHandler() http.Handler {
	return buildMux(newSessionStore(), testSecurityPolicy())
}

func TestCompileEndpoint(t *testing.T) {
	server := httptest.NewServer(testHandler())
	defer server.Close()

	response := postJSON(t, server.URL+"/api/compile", map[string]any{"source": apiProgram})
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d", response.StatusCode)
	}
	var result struct {
		SchemaVersion   string                        `json:"schemaVersion"`
		CompilerVersion string                        `json:"compilerVersion"`
		Target          compiler.ReportTarget         `json:"target"`
		Options         compiler.CompilationOptions   `json:"options"`
		SourceUnits     []compiler.SourceUnitMetadata `json:"sourceUnits"`
		Runtime         struct {
			Halted bool     `json:"halted"`
			Output []string `json:"output"`
		} `json:"runtime"`
		Diagnostics []any `json:"diagnostics"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if !result.Runtime.Halted || strings.Join(result.Runtime.Output, "") != "ok\n" {
		t.Fatalf("unexpected runtime: %+v", result.Runtime)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
	}
	if result.SchemaVersion != compiler.ReportSchemaVersion || result.CompilerVersion != version || result.Target.OS == "" {
		t.Fatalf("compile endpoint omitted report contract: %+v", result)
	}
	if len(result.SourceUnits) != 1 || result.Options.VMMaxSteps != maxAPIRunSteps || !result.Options.Execute {
		t.Fatalf("compile endpoint contract mismatch: %+v", result)
	}
}

func TestOptimizationAPISelectionAndValidation(t *testing.T) {
	const source = `class Main inherits IO { main() : Object { out_int((1 + 2) * 3) }; };`
	server := httptest.NewServer(testHandler())
	defer server.Close()

	response := postJSON(t, server.URL+"/api/compile", map[string]any{"source": source, "optimization": compiler.OptimizationBasic})
	var compiled compiler.Result
	if err := json.NewDecoder(response.Body).Decode(&compiled); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || compiled.Options.Optimization != compiler.OptimizationBasic || compiled.Optimization.ExecutionIR != "optimized" || !compiled.Optimization.OptimizedVerified {
		t.Fatalf("optimized compile status=%d report=%+v", response.StatusCode, compiled.Optimization)
	}

	started := postPipeline(t, server.URL+"/api/pipeline/start", map[string]any{"source": source, "optimization": compiler.OptimizationBasic})
	completed := postPipeline(t, server.URL+"/api/pipeline/complete", map[string]any{"id": started.ID})
	if completed.Snapshot.Result.Optimization.ExecutionIR != "optimized" {
		t.Fatalf("pipeline omitted optimization: %+v", completed.Snapshot.Result.Optimization)
	}

	debugResponse := postJSON(t, server.URL+"/api/vm/start", map[string]any{"source": source, "optimization": compiler.OptimizationBasic})
	var debug struct {
		Snapshot compiler.DebugSnapshot `json:"snapshot"`
	}
	if err := json.NewDecoder(debugResponse.Body).Decode(&debug); err != nil {
		debugResponse.Body.Close()
		t.Fatal(err)
	}
	debugResponse.Body.Close()
	if debugResponse.StatusCode != http.StatusOK || debug.Snapshot.Result.Options.Optimization != compiler.OptimizationBasic {
		t.Fatalf("debugger optimization status=%d report=%+v", debugResponse.StatusCode, debug.Snapshot.Result.Optimization)
	}

	for _, endpoint := range []string{"/api/compile", "/api/pipeline/start", "/api/vm/start"} {
		assertPostStatus(t, server.URL+endpoint, map[string]any{"source": source, "optimization": "unsafe"}, http.StatusBadRequest)
	}
}

func TestCompileEndpointWithInput(t *testing.T) {
	server := httptest.NewServer(testHandler())
	defer server.Close()

	response := postJSON(t, server.URL+"/api/compile", map[string]any{"source": echoInputProgram, "input": "hello\n"})
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d", response.StatusCode)
	}
	var result struct {
		Runtime struct {
			Halted bool     `json:"halted"`
			Output []string `json:"output"`
		} `json:"runtime"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if !result.Runtime.Halted || strings.Join(result.Runtime.Output, "") != "hello\n" {
		t.Fatalf("unexpected runtime: %+v", result.Runtime)
	}
}

func TestCompileEndpointRejectsStatelessInteractiveExecution(t *testing.T) {
	server := httptest.NewServer(testHandler())
	defer server.Close()

	response := postJSON(t, server.URL+"/api/compile", map[string]any{"source": echoInputProgram, "interactive": true})
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("interactive compile status = %d", response.StatusCode)
	}
}

func TestCompileEndpointAcceptsNativeProjectSources(t *testing.T) {
	server := httptest.NewServer(testHandler())
	defer server.Close()

	response := postJSON(t, server.URL+"/api/compile", map[string]any{"sources": []map[string]any{
		{"id": "helper", "name": "same.cl", "content": `class Helper { value() : Int { 7 }; };`},
		{"id": "main", "name": "same.cl", "content": `class Main inherits IO { main() : Object { out_int((new Helper).value()) }; };`},
	}})
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d", response.StatusCode)
	}
	var result compiler.Result
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 0 || strings.Join(result.Runtime.Output, "") != "7" || result.Source != "" {
		t.Fatalf("native project compilation failed: %+v", result)
	}
	if len(result.SourceUnits) != 2 || result.SourceUnits[0].ID != "helper" || result.SourceUnits[1].ID != "main" {
		t.Fatalf("source units changed: %+v", result.SourceUnits)
	}
}

func TestPipelineInputFeedsVMAndResetAcceptsUpdatedInput(t *testing.T) {
	server := httptest.NewServer(testHandler())
	defer server.Close()

	started := postPipeline(t, server.URL+"/api/pipeline/start", map[string]any{"source": echoInputProgram, "input": "first\n"})
	completed := postPipeline(t, server.URL+"/api/pipeline/complete", map[string]any{"id": started.ID})
	if !completed.Snapshot.Complete {
		t.Fatalf("pipeline was not completed: %+v", completed)
	}

	runResponse := postJSON(t, server.URL+"/api/vm/run", map[string]any{"id": started.ID, "maxSteps": 1000})
	defer runResponse.Body.Close()
	var first struct {
		Snapshot struct {
			Runtime struct {
				Halted bool     `json:"halted"`
				Output []string `json:"output"`
			} `json:"runtime"`
		} `json:"snapshot"`
	}
	if err := json.NewDecoder(runResponse.Body).Decode(&first); err != nil {
		t.Fatal(err)
	}
	if !first.Snapshot.Runtime.Halted || strings.Join(first.Snapshot.Runtime.Output, "") != "first\n" {
		t.Fatalf("stored pipeline input was not applied: %+v", first)
	}

	resetResponse := postJSON(t, server.URL+"/api/vm/reset", map[string]any{"id": started.ID, "input": "second\n"})
	defer resetResponse.Body.Close()
	if resetResponse.StatusCode != http.StatusOK {
		t.Fatalf("reset status %d", resetResponse.StatusCode)
	}

	secondResponse := postJSON(t, server.URL+"/api/vm/run", map[string]any{"id": started.ID, "maxSteps": 1000, "input": "second\n"})
	defer secondResponse.Body.Close()
	var second struct {
		Snapshot struct {
			Runtime struct {
				Halted bool     `json:"halted"`
				Output []string `json:"output"`
			} `json:"runtime"`
		} `json:"snapshot"`
	}
	if err := json.NewDecoder(secondResponse.Body).Decode(&second); err != nil {
		t.Fatal(err)
	}
	if !second.Snapshot.Runtime.Halted || strings.Join(second.Snapshot.Runtime.Output, "") != "second\n" {
		t.Fatalf("updated input was not applied on reset/run: %+v", second)
	}
}

func TestDebugSessionLifecycle(t *testing.T) {
	server := httptest.NewServer(testHandler())
	defer server.Close()

	startResponse := postJSON(t, server.URL+"/api/vm/start", map[string]any{"source": apiProgram})
	defer startResponse.Body.Close()
	var start struct {
		ID       string `json:"id"`
		Snapshot struct {
			Status  string `json:"status"`
			Runtime struct {
				ExecutedSteps int  `json:"executedSteps"`
				Halted        bool `json:"halted"`
			} `json:"runtime"`
		} `json:"snapshot"`
	}
	if err := json.NewDecoder(startResponse.Body).Decode(&start); err != nil {
		t.Fatal(err)
	}
	if start.ID == "" || start.Snapshot.Status != "ready" || start.Snapshot.Runtime.ExecutedSteps != 0 {
		t.Fatalf("unexpected start response: %+v", start)
	}

	stepResponse := postJSON(t, server.URL+"/api/vm/step", map[string]any{"id": start.ID})
	defer stepResponse.Body.Close()
	var step struct {
		Snapshot struct {
			Runtime struct {
				ExecutedSteps int `json:"executedSteps"`
			} `json:"runtime"`
		} `json:"snapshot"`
	}
	if err := json.NewDecoder(stepResponse.Body).Decode(&step); err != nil {
		t.Fatal(err)
	}
	if step.Snapshot.Runtime.ExecutedSteps != 1 {
		t.Fatalf("expected one step, got %+v", step)
	}

	runResponse := postJSON(t, server.URL+"/api/vm/run", map[string]any{"id": start.ID, "maxSteps": 1000})
	defer runResponse.Body.Close()
	var run struct {
		Snapshot struct {
			Runtime struct {
				Halted bool     `json:"halted"`
				Output []string `json:"output"`
			} `json:"runtime"`
		} `json:"snapshot"`
	}
	if err := json.NewDecoder(runResponse.Body).Decode(&run); err != nil {
		t.Fatal(err)
	}
	if !run.Snapshot.Runtime.Halted || strings.Join(run.Snapshot.Runtime.Output, "") != "ok\n" {
		t.Fatalf("unexpected run: %+v", run)
	}

	resetResponse := postJSON(t, server.URL+"/api/vm/reset", map[string]any{"id": start.ID})
	defer resetResponse.Body.Close()
	if resetResponse.StatusCode != http.StatusOK {
		t.Fatalf("reset status %d", resetResponse.StatusCode)
	}

	disposeResponse := postJSON(t, server.URL+"/api/vm/dispose", map[string]any{"id": start.ID})
	defer disposeResponse.Body.Close()
	if disposeResponse.StatusCode != http.StatusOK {
		t.Fatalf("dispose status %d", disposeResponse.StatusCode)
	}

	missingResponse := postJSON(t, server.URL+"/api/vm/step", map[string]any{"id": start.ID})
	defer missingResponse.Body.Close()
	if missingResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 after dispose, got %d", missingResponse.StatusCode)
	}
}

func TestHeapEndpointIsBoundedNavigableAndReadOnly(t *testing.T) {
	const source = `class Node {
  value : Int;
  next : Node;
  init(v : Int) : Node {{ value <- v; self; }};
  link(n : Node) : Node {{ next <- n; self; }};
};
class Main {
  first : Node;
  second : Node;
  main() : Object {{
    first <- (new Node).init(1);
    second <- (new Node).init(2);
    first.link(second);
    second.link(first);
    self;
  }};
};`
	server := httptest.NewServer(testHandler())
	defer server.Close()

	startedResponse := postJSON(t, server.URL+"/api/vm/start", map[string]any{"source": source})
	defer startedResponse.Body.Close()
	var started struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(startedResponse.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		response := postJSON(t, server.URL+"/api/vm/heap", map[string]any{"id": started.ID, "heap": map[string]any{"mode": "summary"}})
		var payload heapSessionResponse
		if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
			response.Body.Close()
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK || payload.ID != started.ID || payload.Heap.Mode != "summary" || payload.Heap.TotalObjects != 0 {
			t.Fatalf("ready heap summary status=%d payload=%+v", response.StatusCode, payload)
		}
	}
	stepped := postJSON(t, server.URL+"/api/vm/step", map[string]any{"id": started.ID})
	var stepPayload struct {
		Snapshot compiler.DebugSnapshot `json:"snapshot"`
	}
	if err := json.NewDecoder(stepped.Body).Decode(&stepPayload); err != nil {
		stepped.Body.Close()
		t.Fatal(err)
	}
	stepped.Body.Close()
	if stepPayload.Snapshot.Runtime.ExecutedSteps != 1 {
		t.Fatalf("heap query advanced VM before step: %+v", stepPayload.Snapshot.Runtime)
	}

	run := postJSON(t, server.URL+"/api/vm/run", map[string]any{"id": started.ID, "maxSteps": 100_000})
	run.Body.Close()
	if run.StatusCode != http.StatusOK {
		t.Fatalf("heap fixture run status=%d", run.StatusCode)
	}
	search := postJSON(t, server.URL+"/api/vm/heap", map[string]any{
		"id":   started.ID,
		"heap": map[string]any{"mode": "search", "class": "Node", "limits": map[string]any{"maxObjects": 1, "maxBytes": 4096}},
	})
	var searchPayload heapSessionResponse
	if err := json.NewDecoder(search.Body).Decode(&searchPayload); err != nil {
		search.Body.Close()
		t.Fatal(err)
	}
	search.Body.Close()
	if search.StatusCode != http.StatusOK || len(searchPayload.Heap.Objects) != 1 || searchPayload.Heap.Objects[0].Class != "Node" || searchPayload.Heap.NextOffset != 1 || !searchPayload.Heap.Truncated {
		t.Fatalf("heap search status=%d payload=%+v", search.StatusCode, searchPayload)
	}
	objectID := searchPayload.Heap.Objects[0].ID
	graph := postJSON(t, server.URL+"/api/vm/heap", map[string]any{"id": started.ID, "heap": map[string]any{"mode": "graph", "id": objectID}})
	var graphPayload heapSessionResponse
	if err := json.NewDecoder(graph.Body).Decode(&graphPayload); err != nil {
		graph.Body.Close()
		t.Fatal(err)
	}
	graph.Body.Close()
	if graph.StatusCode != http.StatusOK || len(graphPayload.Heap.Objects) != 2 || graphPayload.Heap.Objects[0].Class != "Node" || graphPayload.Heap.Objects[1].Class != "Node" {
		t.Fatalf("cycle-safe graph status=%d payload=%+v", graph.StatusCode, graphPayload)
	}
	encoded, err := json.Marshal(graphPayload.Heap)
	if err != nil || len(encoded) > graphPayload.Heap.Limits.MaxBytes {
		t.Fatalf("heap response exceeded its bound: bytes=%d limits=%+v err=%v", len(encoded), graphPayload.Heap.Limits, err)
	}

	missing := postJSON(t, server.URL+"/api/vm/heap", map[string]any{"id": started.ID, "heap": map[string]any{"mode": "object", "id": "Node#999"}})
	missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("missing heap object status=%d", missing.StatusCode)
	}
	invalid := postJSON(t, server.URL+"/api/vm/heap", map[string]any{"id": started.ID, "heap": map[string]any{"mode": "graph", "limits": map[string]any{"maxDepth": 33}}})
	invalid.Body.Close()
	if invalid.StatusCode != http.StatusBadRequest {
		t.Fatalf("unbounded heap request status=%d", invalid.StatusCode)
	}
}

func TestReverseDebuggerAPIRevisionBranchingAndMeaningfulStops(t *testing.T) {
	const source = `class Main { i : Int <- 0; main() : Object {{ while i < 6 loop i <- i + 1 pool; self; }}; };`
	server := httptest.NewServer(testHandler())
	defer server.Close()

	startedResponse := postJSON(t, server.URL+"/api/vm/start", map[string]any{"source": source})
	var started struct {
		ID       string                 `json:"id"`
		Snapshot compiler.DebugSnapshot `json:"snapshot"`
	}
	if err := json.NewDecoder(startedResponse.Body).Decode(&started); err != nil {
		startedResponse.Body.Close()
		t.Fatal(err)
	}
	startedResponse.Body.Close()
	if started.Snapshot.Timeline == nil || started.Snapshot.Timeline.Revision == 0 || started.Snapshot.Timeline.CanStepBack {
		t.Fatalf("initial reverse timeline = %+v", started.Snapshot.Timeline)
	}

	limitedResponse := postJSON(t, server.URL+"/api/vm/run", map[string]any{"id": started.ID, "maxSteps": 12, "light": true})
	var limited struct {
		Snapshot compiler.DebugSnapshot `json:"snapshot"`
	}
	if err := json.NewDecoder(limitedResponse.Body).Decode(&limited); err != nil {
		limitedResponse.Body.Close()
		t.Fatal(err)
	}
	limitedResponse.Body.Close()
	if limited.Snapshot.StopReason != compiler.StopReasonInstructionLimit || limited.Snapshot.Timeline == nil || !limited.Snapshot.Timeline.CanStepBack {
		t.Fatalf("bounded forward stop = %+v", limited.Snapshot)
	}
	oldRevision := limited.Snapshot.Timeline.Revision
	limitedStep := limited.Snapshot.Runtime.ExecutedSteps

	finishedResponse := postJSON(t, server.URL+"/api/vm/run", map[string]any{"id": started.ID, "maxSteps": 100_000, "light": true})
	var finished struct {
		Snapshot compiler.DebugSnapshot `json:"snapshot"`
	}
	if err := json.NewDecoder(finishedResponse.Body).Decode(&finished); err != nil {
		finishedResponse.Body.Close()
		t.Fatal(err)
	}
	finishedResponse.Body.Close()
	if !finished.Snapshot.Runtime.Halted {
		t.Fatalf("reverse fixture did not halt: %+v", finished.Snapshot.Runtime)
	}

	reverseResponse := postJSON(t, server.URL+"/api/vm/reverse-continue", map[string]any{
		"id": started.ID, "timelineRevision": oldRevision, "light": true,
	})
	var reversed struct {
		Snapshot compiler.DebugSnapshot `json:"snapshot"`
	}
	if err := json.NewDecoder(reverseResponse.Body).Decode(&reversed); err != nil {
		reverseResponse.Body.Close()
		t.Fatal(err)
	}
	reverseResponse.Body.Close()
	if reverseResponse.StatusCode != http.StatusOK || reversed.Snapshot.StopReason != compiler.StopReasonReverseContinue || reversed.Snapshot.Runtime.ExecutedSteps != limitedStep || reversed.Snapshot.Timeline.LatestStep <= limitedStep {
		t.Fatalf("reverse continue status=%d snapshot=%+v", reverseResponse.StatusCode, reversed.Snapshot)
	}

	backResponse := postJSON(t, server.URL+"/api/vm/step-back", map[string]any{
		"id": started.ID, "timelineRevision": oldRevision, "reverseMode": "instruction", "light": true,
	})
	var backed struct {
		Snapshot compiler.DebugSnapshot `json:"snapshot"`
	}
	if err := json.NewDecoder(backResponse.Body).Decode(&backed); err != nil {
		backResponse.Body.Close()
		t.Fatal(err)
	}
	backResponse.Body.Close()
	if backResponse.StatusCode != http.StatusOK || backed.Snapshot.Runtime.ExecutedSteps != limitedStep-1 {
		t.Fatalf("step back status=%d snapshot=%+v", backResponse.StatusCode, backed.Snapshot)
	}

	branchedResponse := postJSON(t, server.URL+"/api/vm/step", map[string]any{"id": started.ID, "light": true})
	var branched struct {
		Snapshot compiler.DebugSnapshot `json:"snapshot"`
	}
	if err := json.NewDecoder(branchedResponse.Body).Decode(&branched); err != nil {
		branchedResponse.Body.Close()
		t.Fatal(err)
	}
	branchedResponse.Body.Close()
	if branched.Snapshot.Timeline.Revision <= oldRevision || !branched.Snapshot.Timeline.FutureDiscarded {
		t.Fatalf("forward branch timeline = %+v", branched.Snapshot.Timeline)
	}

	stale := postJSON(t, server.URL+"/api/vm/step-back", map[string]any{
		"id": started.ID, "timelineRevision": oldRevision, "reverseMode": "instruction",
	})
	stale.Body.Close()
	if stale.StatusCode != http.StatusConflict {
		t.Fatalf("stale timeline revision status=%d", stale.StatusCode)
	}
	missingRevision := postJSON(t, server.URL+"/api/vm/step-back", map[string]any{"id": started.ID})
	missingRevision.Body.Close()
	if missingRevision.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing timeline revision status=%d", missingRevision.StatusCode)
	}
}

type testPipelineResponse struct {
	ID       string                    `json:"id"`
	Snapshot compiler.PipelineSnapshot `json:"snapshot"`
}

func TestPipelineLifecyclePromotesSameIDToVM(t *testing.T) {
	server := httptest.NewServer(testHandler())
	defer server.Close()

	started := postPipeline(t, server.URL+"/api/pipeline/start", map[string]any{"source": apiProgram})
	if started.ID == "" || started.Snapshot.CurrentPhase != compiler.PhaseLexer {
		t.Fatalf("start did not execute only Lexer: %+v", started)
	}
	if len(started.Snapshot.Result.Tokens) == 0 || started.Snapshot.Result.AST != nil {
		t.Fatal("start exposed artifacts beyond Lexer")
	}

	want := []compiler.CompilationPhase{
		compiler.PhaseParser,
		compiler.PhaseSemantic,
		compiler.PhaseIR,
		compiler.PhaseCodegen,
		compiler.PhaseRuntime,
	}
	current := started
	for _, phase := range want {
		current = postPipeline(t, server.URL+"/api/pipeline/next", map[string]any{"id": started.ID})
		if current.ID != started.ID || current.Snapshot.CurrentPhase != phase {
			t.Fatalf("next returned %+v, want phase %s under id %s", current, phase, started.ID)
		}
	}
	if !current.Snapshot.Complete || current.Snapshot.Result.Runtime.ExecutedSteps != 0 {
		t.Fatalf("runtime was not promoted in a stopped state: %+v", current.Snapshot)
	}

	stepResponse := postJSON(t, server.URL+"/api/vm/step", map[string]any{"id": started.ID})
	defer stepResponse.Body.Close()
	if stepResponse.StatusCode != http.StatusOK {
		t.Fatalf("step status %d", stepResponse.StatusCode)
	}
	var stepped struct {
		ID       string                 `json:"id"`
		Snapshot compiler.DebugSnapshot `json:"snapshot"`
	}
	if err := json.NewDecoder(stepResponse.Body).Decode(&stepped); err != nil {
		t.Fatal(err)
	}
	if stepped.ID != started.ID || stepped.Snapshot.Runtime.ExecutedSteps != 1 {
		t.Fatalf("same pipeline id did not execute one VM instruction: %+v", stepped)
	}
}

func TestLightVMSnapshotsOmitStaticArtifactsAndReturnTraceDeltas(t *testing.T) {
	server := httptest.NewServer(testHandler())
	defer server.Close()

	startResponse := postJSON(t, server.URL+"/api/vm/start", map[string]any{"source": apiProgram})
	defer startResponse.Body.Close()
	var started struct {
		ID       string `json:"id"`
		Snapshot struct {
			Runtime compiler.VMResult `json:"runtime"`
		} `json:"snapshot"`
	}
	if err := json.NewDecoder(startResponse.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	traceFrom := started.Snapshot.Runtime.TraceOffset + len(started.Snapshot.Runtime.Trace)

	runResponse := postJSON(t, server.URL+"/api/vm/run", map[string]any{
		"id": started.ID, "maxSteps": 2, "light": true, "traceFrom": traceFrom,
	})
	defer runResponse.Body.Close()
	var payload struct {
		ID       string                     `json:"id"`
		Snapshot map[string]json.RawMessage `json:"snapshot"`
	}
	if err := json.NewDecoder(runResponse.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if _, found := payload.Snapshot["result"]; found {
		t.Fatal("light snapshot must omit the static compilation result")
	}
	if _, found := payload.Snapshot["runtime"]; !found {
		t.Fatal("light snapshot must include runtime state")
	}
	var runtime compiler.VMResult
	if err := json.Unmarshal(payload.Snapshot["runtime"], &runtime); err != nil {
		t.Fatal(err)
	}
	if runtime.ExecutedSteps != 2 || runtime.TraceOffset < traceFrom || len(runtime.Trace) == 0 {
		t.Fatalf("unexpected incremental runtime: %+v (traceFrom %d)", runtime, traceFrom)
	}
}

func TestInteractiveInputAPIWaitsRejectsStaleAndResumesExactlyOnce(t *testing.T) {
	server := httptest.NewServer(testHandler())
	defer server.Close()

	startResponse := postJSON(t, server.URL+"/api/vm/start", map[string]any{"source": echoInputProgram, "interactive": true})
	defer startResponse.Body.Close()
	var started struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(startResponse.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	run := postJSON(t, server.URL+"/api/vm/run", map[string]any{"id": started.ID, "maxSteps": 100_000, "light": true})
	defer run.Body.Close()
	var waiting struct {
		Snapshot struct {
			Status  string            `json:"status"`
			Runtime compiler.VMResult `json:"runtime"`
		} `json:"snapshot"`
	}
	if err := json.NewDecoder(run.Body).Decode(&waiting); err != nil {
		t.Fatal(err)
	}
	request := waiting.Snapshot.Runtime.WaitingInput
	if run.StatusCode != http.StatusOK || waiting.Snapshot.Status != "waiting-input" || request == nil {
		t.Fatalf("waiting response status=%d payload=%+v", run.StatusCode, waiting)
	}

	blockedRun := postJSON(t, server.URL+"/api/vm/run", map[string]any{"id": started.ID, "maxSteps": 1})
	defer blockedRun.Body.Close()
	if blockedRun.StatusCode != http.StatusConflict {
		t.Fatalf("run while waiting status = %d", blockedRun.StatusCode)
	}
	stale := postJSON(t, server.URL+"/api/vm/input", map[string]any{"id": started.ID, "line": "stale", "inputSequence": request.Sequence + 1, "light": true})
	defer stale.Body.Close()
	if stale.StatusCode != http.StatusConflict {
		t.Fatalf("stale input status = %d", stale.StatusCode)
	}
	replacement := postJSON(t, server.URL+"/api/vm/input", map[string]any{"id": started.ID, "input": "replace\n", "line": "hello", "inputSequence": request.Sequence})
	defer replacement.Body.Close()
	if replacement.StatusCode != http.StatusBadRequest {
		t.Fatalf("input replacement status = %d", replacement.StatusCode)
	}
	accepted := postJSON(t, server.URL+"/api/vm/input", map[string]any{"id": started.ID, "line": "hello", "inputSequence": request.Sequence, "light": true})
	defer accepted.Body.Close()
	if accepted.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(accepted.Body)
		t.Fatalf("accepted input status = %d: %s", accepted.StatusCode, body)
	}
	duplicate := postJSON(t, server.URL+"/api/vm/input", map[string]any{"id": started.ID, "line": "again", "inputSequence": request.Sequence, "light": true})
	defer duplicate.Body.Close()
	if duplicate.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate input status = %d", duplicate.StatusCode)
	}
	completed := postJSON(t, server.URL+"/api/vm/run", map[string]any{"id": started.ID, "maxSteps": 100_000, "light": true})
	defer completed.Body.Close()
	var result struct {
		Snapshot struct {
			Runtime compiler.VMResult `json:"runtime"`
		} `json:"snapshot"`
	}
	if err := json.NewDecoder(completed.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if !result.Snapshot.Runtime.Halted || result.Snapshot.Runtime.InputEvents != 1 || strings.Join(result.Snapshot.Runtime.Output, "") != "hello\n" {
		t.Fatalf("completed interactive runtime = %+v", result.Snapshot.Runtime)
	}
}

func TestInteractiveInputAPIValidatesLineAndAllowsResetWhileWaiting(t *testing.T) {
	server := httptest.NewServer(testHandler())
	defer server.Close()
	started := postJSON(t, server.URL+"/api/vm/start", map[string]any{"source": echoInputProgram, "interactive": true})
	defer started.Body.Close()
	var start struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(started.Body).Decode(&start); err != nil {
		t.Fatal(err)
	}
	waiting := postJSON(t, server.URL+"/api/vm/run", map[string]any{"id": start.ID, "maxSteps": 100_000})
	waiting.Body.Close()
	oversized := postJSON(t, server.URL+"/api/vm/input", map[string]any{"id": start.ID, "line": strings.Repeat("x", maxLiveInputLineBytes+1), "inputSequence": 1})
	defer oversized.Body.Close()
	if oversized.StatusCode != http.StatusBadRequest {
		t.Fatalf("oversized input status = %d", oversized.StatusCode)
	}
	reset := postJSON(t, server.URL+"/api/vm/reset", map[string]any{"id": start.ID, "light": true})
	defer reset.Body.Close()
	if reset.StatusCode != http.StatusOK {
		t.Fatalf("reset while waiting status = %d", reset.StatusCode)
	}
}

func TestSourceStepAndRunToCursorAPI(t *testing.T) {
	server := httptest.NewServer(testHandler())
	defer server.Close()
	started := postJSON(t, server.URL+"/api/vm/start", map[string]any{"source": sourceStepAPIProgram})
	defer started.Body.Close()
	var start struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(started.Body).Decode(&start); err != nil {
		t.Fatal(err)
	}

	var entered compiler.DebugSnapshot
	for index := 0; index < 20; index++ {
		response := postJSON(t, server.URL+"/api/vm/step-into", map[string]any{"id": start.ID, "maxSteps": 100_000, "light": true})
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			t.Fatalf("step into status = %d", response.StatusCode)
		}
		var payload struct {
			Snapshot compiler.DebugSnapshot `json:"snapshot"`
		}
		if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
			response.Body.Close()
			t.Fatal(err)
		}
		response.Body.Close()
		entered = payload.Snapshot
		if entered.Location != nil && entered.Location.Method == "Helper.twice" {
			break
		}
	}
	if entered.StopReason != compiler.StopReasonStepInto || entered.Location == nil || entered.Location.Method != "Helper.twice" {
		t.Fatalf("API step into = %+v", entered)
	}

	reset := postJSON(t, server.URL+"/api/vm/reset", map[string]any{"id": start.ID, "light": true})
	reset.Body.Close()
	cursor := postJSON(t, server.URL+"/api/vm/run-to-cursor", map[string]any{
		"id": start.ID, "maxSteps": 100_000,
		"cursor": map[string]any{"sourceId": "source-1", "line": 5},
	})
	defer cursor.Body.Close()
	var stopped struct {
		Snapshot compiler.DebugSnapshot `json:"snapshot"`
	}
	if err := json.NewDecoder(cursor.Body).Decode(&stopped); err != nil {
		t.Fatal(err)
	}
	if cursor.StatusCode != http.StatusOK || stopped.Snapshot.StopReason != compiler.StopReasonRunToCursor || stopped.Snapshot.Location == nil || stopped.Snapshot.Location.Span.StartLine != 5 {
		t.Fatalf("run to cursor status=%d snapshot=%+v", cursor.StatusCode, stopped.Snapshot)
	}

	unresolved := postJSON(t, server.URL+"/api/vm/run-to-cursor", map[string]any{
		"id": start.ID, "maxSteps": 100_000,
		"cursor": map[string]any{"sourceId": "source-1", "line": 7},
	})
	defer unresolved.Body.Close()
	if unresolved.StatusCode != http.StatusConflict {
		t.Fatalf("unresolved cursor status = %d", unresolved.StatusCode)
	}
	completed := postJSON(t, server.URL+"/api/vm/run", map[string]any{"id": start.ID, "maxSteps": 100_000, "light": true})
	defer completed.Body.Close()
	var resumed struct {
		Snapshot compiler.DebugSnapshot `json:"snapshot"`
	}
	if err := json.NewDecoder(completed.Body).Decode(&resumed); err != nil {
		t.Fatal(err)
	}
	if completed.StatusCode != http.StatusOK || !resumed.Snapshot.Runtime.Halted || resumed.Snapshot.StopReason != compiler.StopReasonHalt {
		t.Fatalf("temporary cursor target leaked: status=%d snapshot=%+v", completed.StatusCode, resumed.Snapshot)
	}
}

func TestVMBreakpointResumeMakesProgressUnderSameSession(t *testing.T) {
	server := httptest.NewServer(testHandler())
	defer server.Close()

	startResponse := postJSON(t, server.URL+"/api/vm/start", map[string]any{"source": apiProgram})
	defer startResponse.Body.Close()
	var started struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(startResponse.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}

	pausedResponse := postJSON(t, server.URL+"/api/vm/run", map[string]any{
		"id": started.ID, "maxSteps": 25, "breakpoints": []int{2},
	})
	defer pausedResponse.Body.Close()
	var paused struct {
		Snapshot compiler.DebugSnapshot `json:"snapshot"`
	}
	if err := json.NewDecoder(pausedResponse.Body).Decode(&paused); err != nil {
		t.Fatal(err)
	}
	if !paused.Snapshot.Runtime.Paused || paused.Snapshot.Runtime.Trace[len(paused.Snapshot.Runtime.Trace)-1].Op != "BREAKPOINT" {
		t.Fatalf("expected a breakpoint pause: %+v", paused.Snapshot.Runtime)
	}

	resumedResponse := postJSON(t, server.URL+"/api/vm/run", map[string]any{
		"id": started.ID, "maxSteps": 1, "breakpoints": []int{2},
	})
	defer resumedResponse.Body.Close()
	var resumed struct {
		Snapshot compiler.DebugSnapshot `json:"snapshot"`
	}
	if err := json.NewDecoder(resumedResponse.Body).Decode(&resumed); err != nil {
		t.Fatal(err)
	}
	if resumed.Snapshot.Runtime.ExecutedSteps <= paused.Snapshot.Runtime.ExecutedSteps {
		t.Fatalf("resume did not execute past the breakpoint: before=%d after=%d", paused.Snapshot.Runtime.ExecutedSteps, resumed.Snapshot.Runtime.ExecutedSteps)
	}
}

func TestConditionalBreakpointWatchAndLogpointAPI(t *testing.T) {
	server := httptest.NewServer(testHandler())
	defer server.Close()
	const source = `class Main {
  main() : Object {
    let i : Int <- 0 in
      while i < 2 loop
        i <- i + 1
      pool
  };
};`
	startedResponse := postJSON(t, server.URL+"/api/vm/start", map[string]any{"source": source})
	var started struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(startedResponse.Body).Decode(&started); err != nil {
		startedResponse.Body.Close()
		t.Fatal(err)
	}
	startedResponse.Body.Close()

	conditional := []map[string]any{{"id": "loop", "line": 5, "condition": "i = 1", "enabled": true}}
	watches := []map[string]any{{"id": "i", "expression": "i", "enabled": true}, {"id": "unsafe", "expression": "i <- 9", "enabled": true}}
	pausedResponse := postJSON(t, server.URL+"/api/vm/run", map[string]any{
		"id": started.ID, "maxSteps": 100_000, "light": true, "breakpoints": conditional, "watches": watches,
	})
	defer pausedResponse.Body.Close()
	var paused struct {
		Snapshot compiler.DebugSnapshot `json:"snapshot"`
	}
	if err := json.NewDecoder(pausedResponse.Body).Decode(&paused); err != nil {
		t.Fatal(err)
	}
	if pausedResponse.StatusCode != http.StatusOK || paused.Snapshot.StopReason != compiler.StopReasonBreakpoint {
		t.Fatalf("conditional API status=%d snapshot=%+v", pausedResponse.StatusCode, paused.Snapshot)
	}
	if len(paused.Snapshot.Watches) != 2 || paused.Snapshot.Watches[0].Value == nil || paused.Snapshot.Watches[0].Value.Display != "1" || paused.Snapshot.Watches[1].Diagnostic == "" {
		t.Fatalf("light watch payload = %+v", paused.Snapshot.Watches)
	}
	if len(paused.Snapshot.Breakpoints) != 1 || !paused.Snapshot.Breakpoints[0].Verified || paused.Snapshot.Breakpoints[0].Hits == 0 {
		t.Fatalf("light breakpoint payload = %+v", paused.Snapshot.Breakpoints)
	}

	reset := postJSON(t, server.URL+"/api/vm/reset", map[string]any{"id": started.ID, "light": true, "watches": watches})
	reset.Body.Close()
	logpoint := []map[string]any{{"id": "log", "line": 5, "logMessage": "value={i}", "enabled": true}}
	finishedResponse := postJSON(t, server.URL+"/api/vm/run", map[string]any{
		"id": started.ID, "maxSteps": 100_000, "light": true, "breakpoints": logpoint, "watches": watches,
	})
	defer finishedResponse.Body.Close()
	var finished struct {
		Snapshot compiler.DebugSnapshot `json:"snapshot"`
	}
	if err := json.NewDecoder(finishedResponse.Body).Decode(&finished); err != nil {
		t.Fatal(err)
	}
	if !finished.Snapshot.Runtime.Halted || len(finished.Snapshot.DebugLog) == 0 || len(finished.Snapshot.Runtime.Output) != 0 {
		t.Fatalf("logpoint API mixed channels or paused: %+v", finished.Snapshot)
	}
}

func TestVMProjectBreakpointKeepsSourceIdentityAfterReorder(t *testing.T) {
	server := httptest.NewServer(testHandler())
	defer server.Close()

	helper := map[string]any{"id": "helper", "name": "same.cl", "content": `class Helper { value() : Int { 7 }; };`}
	mainUnit := map[string]any{"id": "main", "name": "same.cl", "content": "class Main inherits IO {\n  main() : Object { out_int((new Helper).value()) };\n};"}
	for _, sources := range [][]map[string]any{{helper, mainUnit}, {mainUnit, helper}} {
		startResponse := postJSON(t, server.URL+"/api/vm/start", map[string]any{"sources": sources})
		var started struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(startResponse.Body).Decode(&started); err != nil {
			startResponse.Body.Close()
			t.Fatal(err)
		}
		startResponse.Body.Close()

		pausedResponse := postJSON(t, server.URL+"/api/vm/run", map[string]any{
			"id": started.ID, "maxSteps": 100, "breakpoints": []map[string]any{{"sourceId": "main", "line": 2}},
		})
		var paused struct {
			Snapshot compiler.DebugSnapshot `json:"snapshot"`
		}
		if err := json.NewDecoder(pausedResponse.Body).Decode(&paused); err != nil {
			pausedResponse.Body.Close()
			t.Fatal(err)
		}
		pausedResponse.Body.Close()
		trace := paused.Snapshot.Runtime.Trace
		if !paused.Snapshot.Runtime.Paused || len(trace) == 0 || trace[len(trace)-1].Op != "BREAKPOINT" || trace[len(trace)-1].Span.SourceID != "main" {
			t.Fatalf("source-aware breakpoint changed after reorder: %+v", paused.Snapshot.Runtime)
		}
	}
}

func TestPipelineCompleteFromEveryIntermediatePhase(t *testing.T) {
	server := httptest.NewServer(testHandler())
	defer server.Close()

	phases := []compiler.CompilationPhase{
		compiler.PhaseLexer,
		compiler.PhaseParser,
		compiler.PhaseSemantic,
		compiler.PhaseIR,
		compiler.PhaseCodegen,
	}
	for advance, phase := range phases {
		t.Run(string(phase), func(t *testing.T) {
			started := postPipeline(t, server.URL+"/api/pipeline/start", map[string]any{"source": apiProgram})
			current := started
			for range advance {
				current = postPipeline(t, server.URL+"/api/pipeline/next", map[string]any{"id": started.ID})
			}
			if current.Snapshot.CurrentPhase != phase {
				t.Fatalf("setup phase = %s, want %s", current.Snapshot.CurrentPhase, phase)
			}
			completed := postPipeline(t, server.URL+"/api/pipeline/complete", map[string]any{"id": started.ID})
			if completed.ID != started.ID || !completed.Snapshot.Complete || completed.Snapshot.CurrentPhase != compiler.PhaseRuntime {
				t.Fatalf("complete from %s returned %+v", phase, completed)
			}
		})
	}
}

func TestPipelineDiagnosticsAreDomainResults(t *testing.T) {
	server := httptest.NewServer(testHandler())
	defer server.Close()

	started := postPipeline(t, server.URL+"/api/pipeline/start", map[string]any{"source": `class Main { main(): Object { # 0 }; };`})
	blockedResponse := postJSON(t, server.URL+"/api/pipeline/next", map[string]any{"id": started.ID})
	defer blockedResponse.Body.Close()
	if blockedResponse.StatusCode != http.StatusOK {
		t.Fatalf("compiler diagnostics returned HTTP %d", blockedResponse.StatusCode)
	}
	var blocked testPipelineResponse
	if err := json.NewDecoder(blockedResponse.Body).Decode(&blocked); err != nil {
		t.Fatal(err)
	}
	if !blocked.Snapshot.Blocked || len(blocked.Snapshot.Result.Diagnostics) == 0 {
		t.Fatalf("expected blocked diagnostic result: %+v", blocked)
	}
}

func TestPipelineDisposeAndExpiryReturnNotFound(t *testing.T) {
	store := newSessionStore()
	server := httptest.NewServer(buildMux(store, testSecurityPolicy()))
	defer server.Close()

	started := postPipeline(t, server.URL+"/api/pipeline/start", map[string]any{"source": apiProgram})
	disposed := postJSON(t, server.URL+"/api/pipeline/dispose", map[string]any{"id": started.ID})
	disposed.Body.Close()
	if disposed.StatusCode != http.StatusOK {
		t.Fatalf("dispose status %d", disposed.StatusCode)
	}
	assertPostStatus(t, server.URL+"/api/pipeline/next", map[string]any{"id": started.ID}, http.StatusNotFound)
	assertPostStatus(t, server.URL+"/api/vm/step", map[string]any{"id": started.ID}, http.StatusNotFound)

	expiring := postPipeline(t, server.URL+"/api/pipeline/start", map[string]any{"source": apiProgram})
	store.mu.Lock()
	store.sessions[expiring.ID].lastUsed = time.Now().Add(-store.ttl - time.Second)
	store.mu.Unlock()
	assertPostStatus(t, server.URL+"/api/pipeline/next", map[string]any{"id": expiring.ID}, http.StatusNotFound)
}

func TestConcurrentPipelineNextRequestsDoNotSkipState(t *testing.T) {
	server := httptest.NewServer(testHandler())
	defer server.Close()

	started := postPipeline(t, server.URL+"/api/pipeline/start", map[string]any{"source": apiProgram})
	phases := make(chan string, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			response := postJSON(t, server.URL+"/api/pipeline/next", map[string]any{"id": started.ID})
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Errorf("next status %d", response.StatusCode)
				return
			}
			var payload testPipelineResponse
			if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
				t.Errorf("decode next response: %v", err)
				return
			}
			phases <- string(payload.Snapshot.CurrentPhase)
		}()
	}
	wait.Wait()
	close(phases)
	got := []string{}
	for phase := range phases {
		got = append(got, phase)
	}
	sort.Strings(got)
	want := []string{string(compiler.PhaseParser), string(compiler.PhaseSemantic)}
	sort.Strings(want)
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("concurrent phases = %v, want %v", got, want)
	}
}

func TestPipelineEndpointValidation(t *testing.T) {
	server := httptest.NewServer(testHandler())
	defer server.Close()

	assertPostStatus(t, server.URL+"/api/pipeline/next", map[string]any{}, http.StatusBadRequest)
	assertPostStatus(t, server.URL+"/api/pipeline/next", map[string]any{"id": strings.Repeat("0", 32)}, http.StatusNotFound)
	assertPostStatus(t, server.URL+"/api/pipeline/start", map[string]any{"source": apiProgram, "unknown": true}, http.StatusBadRequest)
}

func TestAPIGuards(t *testing.T) {
	handler := testHandler()

	foreign := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	foreign.Host = "127.0.0.1"
	foreign.Header.Set("Origin", "https://example.com")
	foreignResponse := httptest.NewRecorder()
	handler.ServeHTTP(foreignResponse, foreign)
	if foreignResponse.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden origin, got %d", foreignResponse.Code)
	}

	wrongType := httptest.NewRequest(http.MethodPost, "/api/compile", strings.NewReader(`{"source":""}`))
	wrongType.Host = "127.0.0.1"
	wrongType.Header.Set(capabilityHeader, testCapability)
	wrongTypeResponse := httptest.NewRecorder()
	handler.ServeHTTP(wrongTypeResponse, wrongType)
	if wrongTypeResponse.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected unsupported media type, got %d", wrongTypeResponse.Code)
	}
}

func TestListenAddressRequiresExplicitRemoteMode(t *testing.T) {
	tests := []struct {
		name        string
		address     string
		allowRemote bool
		wantError   bool
	}{
		{name: "IPv4 loopback", address: "127.0.0.1:0"},
		{name: "IPv6 loopback", address: "[::1]:0"},
		{name: "localhost", address: "localhost:0"},
		{name: "IPv4 wildcard", address: "0.0.0.0:9090", wantError: true},
		{name: "IPv6 wildcard", address: "[::]:9090", wantError: true},
		{name: "LAN address", address: "192.168.1.20:9090", wantError: true},
		{name: "hostname", address: "compiler.example:9090", wantError: true},
		{name: "explicit remote", address: "0.0.0.0:9090", allowRemote: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateListenAddress(test.address, test.allowRemote)
			if (err != nil) != test.wantError {
				t.Fatalf("validateListenAddress(%q, %t) error = %v, wantError %t", test.address, test.allowRemote, err, test.wantError)
			}
		})
	}
}

func TestCapabilityBootstrapAndSecurityHeaders(t *testing.T) {
	server := httptest.NewServer(testHandler())
	defer server.Close()

	rootRequest, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	rootResponse, err := http.DefaultClient.Do(rootRequest)
	if err != nil {
		t.Fatal(err)
	}
	rootBody, err := io.ReadAll(rootResponse.Body)
	rootResponse.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rootBody), `name="cooler-capability" content="`+testCapability+`"`) {
		t.Fatal("initial HTML does not contain the per-process capability")
	}
	if strings.Contains(string(rootBody), "{{COOLER_CAPABILITY}}") {
		t.Fatal("initial HTML still contains the capability placeholder")
	}
	securityHeaders := map[string]string{
		"Content-Security-Policy": "script-src 'self'",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
		"Permissions-Policy":      "camera=()",
		"X-Frame-Options":         "DENY",
		"Cache-Control":           "no-store",
	}
	for name, want := range securityHeaders {
		if got := rootResponse.Header.Get(name); !strings.Contains(got, want) {
			t.Fatalf("%s = %q, want it to contain %q", name, got, want)
		}
	}

	for _, capability := range []string{"", "malformed", strings.Repeat("f", len(testCapability))} {
		response := postJSONWithCapability(t, server.URL+"/api/compile", map[string]any{"source": apiProgram}, capability)
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("capability %q returned HTTP %d, want 403", capability, response.StatusCode)
		}
	}

	response := postJSON(t, server.URL+"/api/compile", map[string]any{"source": apiProgram})
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("valid capability returned HTTP %d", response.StatusCode)
	}
	if strings.Contains(string(body), testCapability) {
		t.Fatal("capability leaked into the compile response")
	}

	staleHandler := buildMux(newSessionStore(), securityPolicy{capability: strings.Repeat("a", 64)})
	staleRequest := httptest.NewRequest(http.MethodPost, "/api/compile", strings.NewReader(`{"source":""}`))
	staleRequest.Host = "127.0.0.1"
	staleRequest.Header.Set("Content-Type", "application/json")
	staleRequest.Header.Set(capabilityHeader, testCapability)
	staleResponse := httptest.NewRecorder()
	staleHandler.ServeHTTP(staleResponse, staleRequest)
	if staleResponse.Code != http.StatusForbidden {
		t.Fatalf("stale capability returned HTTP %d, want 403", staleResponse.Code)
	}
}

func TestCapabilitiesAreRandomPerProcess(t *testing.T) {
	first, err := newCapability()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newCapability()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 64 || len(second) != 64 {
		t.Fatalf("capability lengths = %d and %d, want 64", len(first), len(second))
	}
	if first == second {
		t.Fatal("consecutive process capabilities must differ")
	}
	if _, err := hex.DecodeString(first); err != nil {
		t.Fatalf("capability is not hexadecimal: %v", err)
	}
}

func TestEveryStateChangingEndpointRequiresCapability(t *testing.T) {
	handler := testHandler()
	paths := []string{
		"/api/compile",
		"/api/pipeline/start",
		"/api/pipeline/next",
		"/api/pipeline/complete",
		"/api/pipeline/dispose",
		"/api/vm/start",
		"/api/vm/step",
		"/api/vm/step-into",
		"/api/vm/step-over",
		"/api/vm/step-out",
		"/api/vm/run-to-cursor",
		"/api/vm/run",
		"/api/vm/reset",
		"/api/vm/input",
		"/api/vm/heap",
		"/api/vm/step-back",
		"/api/vm/reverse-continue",
		"/api/vm/dispose",
	}
	for _, path := range paths {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		request.Host = "127.0.0.1"
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s without capability returned HTTP %d, want 403", path, response.Code)
		}
	}

	for _, path := range []string{"/api/examples", "/api/health"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Host = "127.0.0.1"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("read-only %s returned HTTP %d without capability", path, response.Code)
		}
	}
}

func TestHostOriginMethodAndContentTypeValidation(t *testing.T) {
	policy := securityPolicy{capability: testCapability, listenerPort: "9090"}
	handler := buildMux(newSessionStore(), policy)

	request := func(method, origin, contentType, capability string) *http.Request {
		r := httptest.NewRequest(method, "/api/compile", strings.NewReader(`{"source":""}`))
		r.Host = "127.0.0.1:9090"
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		if capability != "" {
			r.Header.Set(capabilityHeader, capability)
		}
		return r
	}
	serve := func(r *http.Request) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		return response
	}

	if got := serve(request(http.MethodGet, "", "", "")).Code; got != http.StatusMethodNotAllowed {
		t.Fatalf("GET compile status = %d, want 405", got)
	}
	for _, origin := range []string{
		"https://127.0.0.1:9090",
		"http://127.0.0.1:9091",
		"http://localhost:9090",
		"null",
	} {
		if got := serve(request(http.MethodPost, origin, "application/json", testCapability)).Code; got != http.StatusForbidden {
			t.Fatalf("origin %q status = %d, want 403", origin, got)
		}
	}
	if got := serve(request(http.MethodPost, "http://127.0.0.1:9090", "application/json", testCapability)).Code; got != http.StatusOK {
		t.Fatalf("same origin status = %d, want 200", got)
	}
	if got := serve(request(http.MethodPost, "", "application/json", "")).Code; got != http.StatusForbidden {
		t.Fatalf("originless request without capability status = %d, want 403", got)
	}
	if got := serve(request(http.MethodPost, "", "application/json", testCapability)).Code; got != http.StatusOK {
		t.Fatalf("originless request with capability status = %d, want 200", got)
	}
	for _, contentType := range []string{"", "text/plain", "application/json-patch+json"} {
		if got := serve(request(http.MethodPost, "", contentType, testCapability)).Code; got != http.StatusUnsupportedMediaType {
			t.Fatalf("Content-Type %q status = %d, want 415", contentType, got)
		}
	}
	if got := serve(request(http.MethodPost, "", "application/json; charset=utf-8", testCapability)).Code; got != http.StatusOK {
		t.Fatalf("JSON with charset status = %d, want 200", got)
	}

	wrongHost := request(http.MethodPost, "", "application/json", testCapability)
	wrongHost.Host = "example.com:9090"
	if got := serve(wrongHost).Code; got != http.StatusForbidden {
		t.Fatalf("foreign Host status = %d, want 403", got)
	}
	wrongPort := request(http.MethodPost, "", "application/json", testCapability)
	wrongPort.Host = "127.0.0.1:8080"
	if got := serve(wrongPort).Code; got != http.StatusForbidden {
		t.Fatalf("wrong listener port status = %d, want 403", got)
	}
}

func TestRemoteModeStillRestrictsHostToListenerIdentities(t *testing.T) {
	policy := securityPolicy{
		listenerPort: "9090",
		allowRemote:  true,
		allowedHosts: map[string]struct{}{
			"127.0.0.1":    {},
			"192.168.1.20": {},
		},
	}
	for _, authority := range []string{"127.0.0.1:9090", "192.168.1.20:9090"} {
		if !policy.hostAllowed(authority) {
			t.Fatalf("listener identity %q was rejected", authority)
		}
	}
	for _, authority := range []string{"evil.example:9090", "192.168.1.21:9090", "192.168.1.20:8080"} {
		if policy.hostAllowed(authority) {
			t.Fatalf("unrelated Host %q was accepted", authority)
		}
	}
}

func TestJSONShapeAndNumericLimits(t *testing.T) {
	server := httptest.NewServer(testHandler())
	defer server.Close()

	postRaw := func(path, body string) int {
		request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set(capabilityHeader, testCapability)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		return response.StatusCode
	}

	invalidBodies := []string{
		`{"source":""} {"source":""}`,
		`{"source":"","unknown":true}`,
		strings.Repeat(" ", maxRequestBodyBytes+1),
	}
	for _, body := range invalidBodies {
		if got := postRaw("/api/compile", body); got != http.StatusBadRequest {
			t.Fatalf("invalid compile body status = %d, want 400", got)
		}
	}

	validMissingID := strings.Repeat("0", 32)
	invalidSessionBodies := []map[string]any{
		{"id": "short"},
		{"id": strings.Repeat("A", 32)},
		{"id": validMissingID, "maxSteps": -1},
		{"id": validMissingID, "maxSteps": maxAPIRequestedRunSteps + 1},
		{"id": validMissingID, "traceFrom": maxTraceOffset + 1},
		{"id": validMissingID, "breakpoints": []int{0}},
		{"id": validMissingID, "breakpoints": []int{maxBreakpointLine + 1}},
		{"id": validMissingID, "breakpoints": make([]int, maxBreakpointCount+1)},
		{"id": validMissingID, "breakpoints": []map[string]any{{"line": 1, "hitCount": -1}}},
		{"id": validMissingID, "breakpoints": []map[string]any{{"line": 1, "condition": strings.Repeat("x", maxDebuggerTextBytes+1)}}},
		{"id": validMissingID, "breakpoints": []map[string]any{{"id": "same", "line": 1}, {"id": "same", "line": 2}}},
		{"id": validMissingID, "watches": []map[string]any{{"id": "same", "expression": "1"}, {"id": "same", "expression": "2"}}},
		{"id": validMissingID, "watches": make([]map[string]any, maxWatchCount+1)},
	}
	for _, payload := range invalidSessionBodies {
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		if got := postRaw("/api/vm/run", string(body)); got != http.StatusBadRequest {
			t.Fatalf("invalid session body %s status = %d, want 400", body, got)
		}
	}
	if got := postRaw("/api/vm/run", `{"id":"`+validMissingID+`"}`); got != http.StatusNotFound {
		t.Fatalf("well-formed missing session status = %d, want 404", got)
	}
	for _, body := range []string{
		`{"id":"` + validMissingID + `"}`,
		`{"id":"` + validMissingID + `","cursor":{"sourceId":"","line":1}}`,
		`{"id":"` + validMissingID + `","cursor":{"sourceId":"source-1","line":0}}`,
	} {
		if got := postRaw("/api/vm/run-to-cursor", body); got != http.StatusBadRequest {
			t.Fatalf("invalid cursor body %s status = %d, want 400", body, got)
		}
	}
	if got := postRaw("/api/vm/run", `{"id":"`+validMissingID+`","cursor":{"sourceId":"source-1","line":1}}`); got != http.StatusBadRequest {
		t.Fatalf("cursor on run status = %d, want 400", got)
	}
}

func TestHTTPServerTimeoutsAreBounded(t *testing.T) {
	server := newHTTPServer(testHandler())
	if server.ReadHeaderTimeout != 5*time.Second || server.ReadTimeout != 15*time.Second ||
		server.WriteTimeout != 30*time.Second || server.IdleTimeout != 60*time.Second || server.MaxHeaderBytes != maxHeaderBytes {
		t.Fatalf("unexpected HTTP timeouts: header=%s read=%s write=%s idle=%s",
			server.ReadHeaderTimeout, server.ReadTimeout, server.WriteTimeout, server.IdleTimeout)
	}
}

func TestHTTPServerShutsDownGracefully(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := newHTTPServer(testHandler())
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- server.Serve(listener)
	}()

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+listener.Addr().String()+"/api/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-serveResult; !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("Serve returned %v after graceful shutdown", err)
	}
}

func TestSessionPruning(t *testing.T) {
	store := &sessionStore{sessions: map[string]*sessionEntry{}, max: 2, ttl: time.Minute}
	now := time.Now()
	store.sessions["expired"] = &sessionEntry{lastUsed: now.Add(-2 * time.Minute)}
	store.sessions["old"] = &sessionEntry{lastUsed: now.Add(-30 * time.Second)}
	store.sessions["new"] = &sessionEntry{lastUsed: now.Add(-10 * time.Second)}
	store.mu.Lock()
	cleanup := store.pruneLocked(now)
	store.mu.Unlock()
	cleanupSessionEntries(cleanup)
	if _, found := store.sessions["expired"]; found {
		t.Fatal("expired session was not removed")
	}
	if len(store.sessions) >= store.max {
		t.Fatalf("store was not reduced below capacity: %d", len(store.sessions))
	}
	if _, found := store.sessions["new"]; !found {
		t.Fatal("newest session should remain")
	}
}

func postJSON(t *testing.T, endpoint string, payload any) *http.Response {
	return postJSONWithCapability(t, endpoint, payload, testCapability)
}

func postJSONWithCapability(t *testing.T, endpoint string, payload any, capability string) *http.Response {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if capability != "" {
		request.Header.Set(capabilityHeader, capability)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func postPipeline(t *testing.T, endpoint string, payload any) testPipelineResponse {
	t.Helper()
	response := postJSON(t, endpoint, payload)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("%s returned HTTP %d", endpoint, response.StatusCode)
	}
	var result testPipelineResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertPostStatus(t *testing.T, endpoint string, payload any, want int) {
	t.Helper()
	response := postJSON(t, endpoint, payload)
	defer response.Body.Close()
	if response.StatusCode != want {
		t.Fatalf("%s returned HTTP %d, want %d", endpoint, response.StatusCode, want)
	}
}

func TestVMPolicyAPIOverrideAndValidation(t *testing.T) {
	policy := compiler.DefaultVMPolicy()
	policy.MaxOutputBytes = 2
	contract := apiResultContract(apiProgram, "", true, &policy)
	result := compiler.CompileWithContract(apiProgram, "", contract)
	if result.Options.VMPolicy != policy {
		t.Fatalf("API policy omitted from report: %+v", result.Options.VMPolicy)
	}
	invalid := compiler.MaximumVMPolicy()
	invalid.MaxBreakpoints++
	if err := validateRequestedVMPolicy(&invalid); err == nil {
		t.Fatal("API accepted policy above local ceiling")
	}
	policy.MaxInstructionsPerRun = 200_000
	policy.MaxInstructionsTotal = 200_000
	if steps, err := validateCompileExecution(150_000, &policy); err != nil || steps != 150_000 {
		t.Fatalf("API rejected bounded custom run chunk: steps=%d err=%v", steps, err)
	}
	if _, err := validateCompileExecution(200_001, &policy); err == nil {
		t.Fatal("API accepted a chunk above the selected policy")
	}
	if _, err := validateCompileExecution(maxAPIRunSteps+1, nil); err == nil {
		t.Fatal("API accepted a chunk above the default policy")
	}
}

func TestSessionPanicBoundaryUnlocksAndHaltsVM(t *testing.T) {
	store := newSessionStore()
	id := strings.Repeat("a", 32)
	entry := &sessionEntry{session: compiler.NewDebugSession(apiProgram), state: sessionStateReady, resettable: true, lastUsed: time.Now()}
	store.sessions[id] = entry
	request := httptest.NewRequest(http.MethodPost, "/api/vm/run", strings.NewReader(`{"id":"`+id+`"}`))
	recorder := httptest.NewRecorder()
	store.withSession(recorder, request, sessionOperationRun, func(*sessionEntry, debugSessionRequest) compiler.DebugSnapshot {
		panic("injected request panic")
	})
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("panic response status = %d", recorder.Code)
	}
	if !entry.mu.TryLock() {
		t.Fatal("session lock remained held after panic")
	}
	entry.mu.Unlock()
	snapshot := entry.session.Snapshot(false, 0)
	found := false
	for _, diagnostic := range snapshot.Runtime.Diagnostics {
		found = found || diagnostic.Code == "COL6099"
	}
	if !snapshot.Runtime.Halted || !found {
		t.Fatalf("panic did not leave inspectable terminal state: %+v", snapshot.Runtime)
	}
}

func TestPipelinePanicBoundaryUnlocksAndDisposesSession(t *testing.T) {
	store := newSessionStore()
	id := strings.Repeat("b", 32)
	entry := &sessionEntry{pipeline: compiler.NewCompilationSession(apiProgram), state: sessionStateCreated, lastUsed: time.Now()}
	store.sessions[id] = entry
	request := httptest.NewRequest(http.MethodPost, "/api/pipeline/next", strings.NewReader(`{"id":"`+id+`"}`))
	recorder := httptest.NewRecorder()
	store.withPipeline(recorder, request, sessionOperationPipelineNext, func(*compiler.CompilationSession) compiler.PipelineSnapshot {
		panic("injected pipeline panic")
	})
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("panic response status = %d", recorder.Code)
	}
	if !entry.mu.TryLock() {
		t.Fatal("pipeline lock remained held after panic")
	}
	entry.mu.Unlock()
	if _, found := store.lookup(id); found {
		t.Fatal("corrupted pipeline session remained available")
	}
}

func TestHTTPRecoveryBoundaryReturnsControlledError(t *testing.T) {
	handler := recoverHTTP(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), "internal request failure") {
		t.Fatalf("unexpected recovered response: %d %q", recorder.Code, recorder.Body.String())
	}
}
