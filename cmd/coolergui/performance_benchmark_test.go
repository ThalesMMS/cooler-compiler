// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ThalesMMS/cooler-compiler/internal/benchwork"
	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
)

var benchmarkHTTPBytes []byte

func BenchmarkCompileAPI(b *testing.B) {
	for _, workload := range benchwork.StandardProjects()[:2] {
		payload := mustBenchmarkJSON(b, compileRequest{Sources: benchmarkSourceUnits(workload.Units)})
		b.Run(workload.Name, func(b *testing.B) {
			b.ReportMetric(float64(len(workload.Source)), "source-bytes/op")
			b.ReportAllocs()
			for b.Loop() {
				recorder := httptest.NewRecorder()
				compileHandler(recorder, httptest.NewRequest(http.MethodPost, "/api/compile", bytes.NewReader(payload)))
				if recorder.Code != http.StatusOK {
					b.Fatalf("compile API returned %d: %s", recorder.Code, recorder.Body.String())
				}
				benchmarkHTTPBytes = recorder.Body.Bytes()
			}
			b.ReportMetric(float64(len(benchmarkHTTPBytes)), "response-bytes/op")
		})
	}
}

func BenchmarkPipelinePhaseAPI(b *testing.B) {
	workload := benchwork.StandardProjects()[1]
	payload := mustBenchmarkJSON(b, compileRequest{Sources: benchmarkSourceUnits(workload.Units)})
	store := newSessionStore()
	defer store.close()
	b.ReportMetric(float64(len(workload.Units)), "source-units/op")
	b.ReportAllocs()
	for b.Loop() {
		recorder := httptest.NewRecorder()
		store.pipelineStartHandler(recorder, httptest.NewRequest(http.MethodPost, "/api/pipeline/start", bytes.NewReader(payload)))
		if recorder.Code != http.StatusOK {
			b.Fatalf("pipeline start returned %d: %s", recorder.Code, recorder.Body.String())
		}
		benchmarkHTTPBytes = recorder.Body.Bytes()
		var response pipelineResponse
		if err := json.Unmarshal(benchmarkHTTPBytes, &response); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		if !store.remove(response.ID, sessionStateDisposed) {
			b.Fatalf("pipeline session %q disappeared", response.ID)
		}
		b.StartTimer()
	}
	b.ReportMetric(float64(len(benchmarkHTTPBytes)), "response-bytes/op")
}

func BenchmarkVMRunBatchAPI(b *testing.B) {
	workload := benchwork.ArithmeticLoop(200)
	store := newSessionStore()
	defer store.close()
	id := benchmarkStartDebugSession(b, store, workload)
	resetPayload := mustBenchmarkJSON(b, debugSessionRequest{ID: id, Light: true})
	runPayload := mustBenchmarkJSON(b, debugSessionRequest{ID: id, MaxSteps: 100_000, Light: true})
	b.ReportAllocs()
	for b.Loop() {
		benchmarkSessionRequest(b, store.resetHandler, "/api/vm/reset", resetPayload)
		benchmarkHTTPBytes = benchmarkSessionRequest(b, store.runHandler, "/api/vm/run", runPayload)
	}
	b.ReportMetric(float64(len(benchmarkHTTPBytes)), "response-bytes/op")
}

func BenchmarkVMSnapshotPayload(b *testing.B) {
	workload := benchwork.ArithmeticLoop(200)
	store := newSessionStore()
	defer store.close()
	id := benchmarkStartDebugSession(b, store, workload)
	entry, found := store.lookup(id)
	if !found {
		b.Fatal("benchmark session disappeared")
	}
	entry.session.Run(10_000, nil)
	for _, light := range []bool{false, true} {
		name := "full"
		if light {
			name = "light"
		}
		b.Run(name, func(b *testing.B) {
			payload := debugSnapshotPayload(entry.session.Snapshot(light, 0), light)
			encoded := mustBenchmarkJSON(b, payload)
			b.ReportMetric(float64(len(encoded)), "payload-bytes/op")
			b.ReportAllocs()
			for b.Loop() {
				var err error
				benchmarkHTTPBytes, err = json.Marshal(payload)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func benchmarkStartDebugSession(b testing.TB, store *sessionStore, workload benchwork.Workload) string {
	b.Helper()
	payload := mustBenchmarkJSON(b, debugSessionRequest{Source: workload.Source, Input: &workload.Input, Light: true})
	recorder := httptest.NewRecorder()
	store.startHandler(recorder, httptest.NewRequest(http.MethodPost, "/api/vm/start", bytes.NewReader(payload)))
	if recorder.Code != http.StatusOK {
		b.Fatalf("VM start returned %d: %s", recorder.Code, recorder.Body.String())
	}
	var response debugSessionResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		b.Fatal(err)
	}
	return response.ID
}

func benchmarkSessionRequest(b testing.TB, handler http.HandlerFunc, path string, payload []byte) []byte {
	b.Helper()
	recorder := httptest.NewRecorder()
	handler(recorder, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload)))
	if recorder.Code != http.StatusOK {
		b.Fatalf("%s returned %d: %s", path, recorder.Code, recorder.Body.String())
	}
	return recorder.Body.Bytes()
}

func mustBenchmarkJSON(b testing.TB, value any) []byte {
	b.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		b.Fatal(err)
	}
	return payload
}

func benchmarkSourceUnits(units []benchwork.Unit) []compiler.SourceUnit {
	result := make([]compiler.SourceUnit, 0, len(units))
	for _, unit := range units {
		result = append(result, compiler.SourceUnit{ID: unit.ID, Name: unit.Name, Content: unit.Content})
	}
	return result
}
