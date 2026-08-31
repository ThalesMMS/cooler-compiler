// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
)

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

func TestCompileEndpoint(t *testing.T) {
	server := httptest.NewServer(buildMux(newSessionStore()))
	defer server.Close()

	response := postJSON(t, server.URL+"/api/compile", map[string]any{"source": apiProgram})
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d", response.StatusCode)
	}
	var result struct {
		Runtime struct {
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
}

func TestCompileEndpointWithInput(t *testing.T) {
	server := httptest.NewServer(buildMux(newSessionStore()))
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

func TestPipelineInputFeedsVMAndResetAcceptsUpdatedInput(t *testing.T) {
	server := httptest.NewServer(buildMux(newSessionStore()))
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
	server := httptest.NewServer(buildMux(newSessionStore()))
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

type testPipelineResponse struct {
	ID       string                    `json:"id"`
	Snapshot compiler.PipelineSnapshot `json:"snapshot"`
}

func TestPipelineLifecyclePromotesSameIDToVM(t *testing.T) {
	server := httptest.NewServer(buildMux(newSessionStore()))
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
	server := httptest.NewServer(buildMux(newSessionStore()))
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

func TestVMBreakpointResumeMakesProgressUnderSameSession(t *testing.T) {
	server := httptest.NewServer(buildMux(newSessionStore()))
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

func TestPipelineCompleteFromEveryIntermediatePhase(t *testing.T) {
	server := httptest.NewServer(buildMux(newSessionStore()))
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
	server := httptest.NewServer(buildMux(newSessionStore()))
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
	server := httptest.NewServer(buildMux(store))
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
	server := httptest.NewServer(buildMux(newSessionStore()))
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
	server := httptest.NewServer(buildMux(newSessionStore()))
	defer server.Close()

	assertPostStatus(t, server.URL+"/api/pipeline/next", map[string]any{}, http.StatusBadRequest)
	assertPostStatus(t, server.URL+"/api/pipeline/next", map[string]any{"id": "missing"}, http.StatusNotFound)
	assertPostStatus(t, server.URL+"/api/pipeline/start", map[string]any{"source": apiProgram, "unknown": true}, http.StatusBadRequest)
}

func TestAPIGuards(t *testing.T) {
	handler := buildMux(newSessionStore())

	foreign := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	foreign.Header.Set("Origin", "https://example.com")
	foreignResponse := httptest.NewRecorder()
	handler.ServeHTTP(foreignResponse, foreign)
	if foreignResponse.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden origin, got %d", foreignResponse.Code)
	}

	wrongType := httptest.NewRequest(http.MethodPost, "/api/compile", strings.NewReader(`{"source":""}`))
	wrongTypeResponse := httptest.NewRecorder()
	handler.ServeHTTP(wrongTypeResponse, wrongType)
	if wrongTypeResponse.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected unsupported media type, got %d", wrongTypeResponse.Code)
	}
}

func TestSessionPruning(t *testing.T) {
	store := &sessionStore{sessions: map[string]*sessionEntry{}, max: 2, ttl: time.Minute}
	now := time.Now()
	store.sessions["expired"] = &sessionEntry{lastUsed: now.Add(-2 * time.Minute)}
	store.sessions["old"] = &sessionEntry{lastUsed: now.Add(-30 * time.Second)}
	store.sessions["new"] = &sessionEntry{lastUsed: now.Add(-10 * time.Second)}
	store.pruneLocked(now)
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
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
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
