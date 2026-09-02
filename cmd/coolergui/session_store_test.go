// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
)

const (
	testSessionIDA = "0000000000000000000000000000000a"
	testSessionIDB = "0000000000000000000000000000000b"
	testSessionIDC = "0000000000000000000000000000000c"
)

func TestSessionLifecycleTransitionTable(t *testing.T) {
	tests := []struct {
		state      sessionState
		resettable bool
		allowed    []sessionOperation
	}{
		{sessionStateCreated, false, []sessionOperation{sessionOperationPipelineNext, sessionOperationPipelineComplete, sessionOperationStep, sessionOperationRun, sessionOperationStepInto, sessionOperationStepOver, sessionOperationStepOut, sessionOperationRunToCursor}},
		{sessionStateCompiling, false, []sessionOperation{sessionOperationPipelineNext, sessionOperationPipelineComplete, sessionOperationStep, sessionOperationRun, sessionOperationStepInto, sessionOperationStepOver, sessionOperationStepOut, sessionOperationRunToCursor}},
		{sessionStateReady, true, []sessionOperation{sessionOperationStep, sessionOperationRun, sessionOperationStepInto, sessionOperationStepOver, sessionOperationStepOut, sessionOperationRunToCursor, sessionOperationReset, sessionOperationHeap, sessionOperationStepBack, sessionOperationReverseContinue}},
		{sessionStatePaused, true, []sessionOperation{sessionOperationStep, sessionOperationRun, sessionOperationStepInto, sessionOperationStepOver, sessionOperationStepOut, sessionOperationRunToCursor, sessionOperationReset, sessionOperationHeap, sessionOperationStepBack, sessionOperationReverseContinue}},
		{sessionStateHalted, true, []sessionOperation{sessionOperationReset, sessionOperationHeap, sessionOperationStepBack, sessionOperationReverseContinue}},
		{sessionStateFailed, true, []sessionOperation{sessionOperationReset, sessionOperationHeap, sessionOperationStepBack, sessionOperationReverseContinue}},
		{sessionStateFailed, false, nil},
		{sessionStateWaitingInput, true, []sessionOperation{sessionOperationInput, sessionOperationReset, sessionOperationHeap, sessionOperationStepBack, sessionOperationReverseContinue}},
		{sessionStateDisposed, false, nil},
		{sessionStateEvicted, false, nil},
		{sessionStateExpired, false, nil},
	}
	operations := []sessionOperation{
		sessionOperationPipelineNext, sessionOperationPipelineComplete,
		sessionOperationStep, sessionOperationRun, sessionOperationStepInto, sessionOperationStepOver,
		sessionOperationStepOut, sessionOperationRunToCursor, sessionOperationInput, sessionOperationReset, sessionOperationHeap, sessionOperationStepBack, sessionOperationReverseContinue,
	}
	for _, test := range tests {
		entry := &sessionEntry{state: test.state, resettable: test.resettable}
		want := map[sessionOperation]bool{}
		for _, operation := range test.allowed {
			want[operation] = true
		}
		for _, operation := range operations {
			if got := entry.allows(operation); got != want[operation] {
				t.Errorf("state=%s resettable=%v operation=%s allowed=%v want=%v", test.state, test.resettable, operation, got, want[operation])
			}
		}
	}
}

func TestSessionLeasesSerializeSameSessionWithoutBlockingOthers(t *testing.T) {
	hooked := make(chan string, 4)
	store := newSessionStoreWithOptions(sessionStoreOptions{
		BeforeEntryLock: func(id string) { hooked <- id },
	})
	entryA := readySessionEntry(t)
	entryB := readySessionEntry(t)
	if inserted, err := store.insert(testSessionIDA, entryA); err != nil || !inserted {
		t.Fatalf("insert A = %v, %v", inserted, err)
	}
	if inserted, err := store.insert(testSessionIDB, entryB); err != nil || !inserted {
		t.Fatalf("insert B = %v, %v", inserted, err)
	}

	first, ok := store.acquire(testSessionIDA)
	if !ok {
		t.Fatal("first lease was rejected")
	}
	if id := <-hooked; id != testSessionIDA {
		t.Fatalf("first hook = %s", id)
	}
	activeMetrics := store.snapshot()
	if activeMetrics.ActiveOperations != 1 || activeMetrics.StateDistribution[sessionStateRunning] != 1 || activeMetrics.StateDistribution[sessionStateReady] != 1 {
		t.Fatalf("active metrics omitted state distribution: %+v", activeMetrics)
	}

	secondReady := make(chan *sessionLease, 1)
	go func() {
		lease, _ := store.acquire(testSessionIDA)
		secondReady <- lease
	}()
	if id := <-hooked; id != testSessionIDA {
		t.Fatalf("second hook = %s", id)
	}
	select {
	case <-secondReady:
		t.Fatal("same-session lease bypassed the entry lock")
	default:
	}

	otherReady := make(chan *sessionLease, 1)
	go func() {
		lease, _ := store.acquire(testSessionIDB)
		otherReady <- lease
	}()
	if id := <-hooked; id != testSessionIDB {
		t.Fatalf("other hook = %s", id)
	}
	other := receiveLease(t, otherReady)
	other.release()

	first.release()
	second := receiveLease(t, secondReady)
	second.release()
	metrics := store.snapshot()
	if metrics.ActiveOperations != 0 || metrics.ActiveSessions != 2 {
		t.Fatalf("unexpected lease metrics: %+v", metrics)
	}
}

func TestSessionTTLAndLRUSkipActiveEntriesAndCleanMemory(t *testing.T) {
	now := time.Unix(1_000, 0)
	store := newSessionStoreWithOptions(sessionStoreOptions{Max: 2, TTL: time.Minute, Now: func() time.Time { return now }})
	activeEntry := readySessionEntry(t)
	idleEntry := readySessionEntry(t)
	if inserted, err := store.insert(testSessionIDA, activeEntry); err != nil || !inserted {
		t.Fatal(err)
	}
	if inserted, err := store.insert(testSessionIDB, idleEntry); err != nil || !inserted {
		t.Fatal(err)
	}
	lease, ok := store.acquire(testSessionIDA)
	if !ok {
		t.Fatal("active lease rejected")
	}
	now = now.Add(2 * time.Minute)
	newEntry := readySessionEntry(t)
	if inserted, err := store.insert(testSessionIDC, newEntry); err != nil || !inserted {
		t.Fatalf("insert around active expiry = %v, %v", inserted, err)
	}
	if _, found := store.lookup(testSessionIDA); !found {
		t.Fatal("active session was expired or evicted")
	}
	if _, found := store.lookup(testSessionIDB); found {
		t.Fatal("expired idle session survived pruning")
	}
	if idleEntry.pipeline != nil || idleEntry.session != nil || idleEntry.state != sessionStateExpired {
		t.Fatalf("expired state was not released: %+v", idleEntry)
	}
	lease.release()

	now = now.Add(2 * time.Minute)
	if _, found := store.acquire(testSessionIDA); found {
		t.Fatal("idle session survived TTL after its operation finished")
	}
	if activeEntry.pipeline != nil || activeEntry.session != nil || activeEntry.state != sessionStateExpired {
		t.Fatalf("expired active state was not released after lease: %+v", activeEntry)
	}
	metrics := store.snapshot()
	if metrics.Expirations != 2 || metrics.RejectedOperations != 1 {
		t.Fatalf("unexpected TTL metrics: %+v", metrics)
	}
}

func TestSessionLRUTieIsDeterministicAndCapacityRejectsAllActive(t *testing.T) {
	now := time.Unix(2_000, 0)
	store := newSessionStoreWithOptions(sessionStoreOptions{Max: 2, TTL: time.Hour, Now: func() time.Time { return now }})
	entryA, entryB := readySessionEntry(t), readySessionEntry(t)
	_, _ = store.insert(testSessionIDA, entryA)
	_, _ = store.insert(testSessionIDB, entryB)
	if inserted, err := store.insert(testSessionIDC, readySessionEntry(t)); err != nil || !inserted {
		t.Fatalf("tie insertion = %v, %v", inserted, err)
	}
	if _, found := store.lookup(testSessionIDA); found {
		t.Fatal("lexically first ID was not selected for tied LRU eviction")
	}
	if entryA.state != sessionStateEvicted || entryA.pipeline != nil || entryA.session != nil {
		t.Fatalf("evicted entry retained state: %+v", entryA)
	}

	leaseB, _ := store.acquire(testSessionIDB)
	leaseC, _ := store.acquire(testSessionIDC)
	inserted, err := store.insert(strings.Repeat("d", 32), readySessionEntry(t))
	if inserted || !errors.Is(err, errSessionCapacity) {
		t.Fatalf("all-active capacity result = %v, %v", inserted, err)
	}
	leaseB.release()
	leaseC.release()
	if metrics := store.snapshot(); metrics.Evictions != 1 {
		t.Fatalf("unexpected eviction metrics: %+v", metrics)
	}
}

func TestSessionCreationPinPreventsPrematureEvictionAndAlwaysReleases(t *testing.T) {
	ids := []string{testSessionIDA, testSessionIDB}
	store := newSessionStoreWithOptions(sessionStoreOptions{Max: 1, NewSessionID: func() (string, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}})
	entryA := readySessionEntry(t)
	id, pin, err := store.createPinned(entryA)
	if err != nil || id != testSessionIDA || pin == nil {
		t.Fatalf("pinned creation = %s, %v, %v", id, pin, err)
	}
	if inserted, err := store.insert(testSessionIDB, readySessionEntry(t)); inserted || !errors.Is(err, errSessionCapacity) {
		t.Fatalf("creation pin allowed eviction: inserted=%v err=%v", inserted, err)
	}
	if _, found := store.lookup(testSessionIDA); !found {
		t.Fatal("creation response pin did not retain its session")
	}
	pin.release()
	if metrics := store.snapshot(); metrics.ActiveOperations != 0 {
		t.Fatalf("creation pin did not release: %+v", metrics)
	}

	ids = []string{testSessionIDB}
	request := httptest.NewRequest(http.MethodPost, "/api/pipeline/start", strings.NewReader(`{"source":"class Main { main() : Object { 0 }; };"}`))
	store.pipelineStartHandler(failingResponseWriter{}, request)
	if metrics := store.snapshot(); metrics.ActiveOperations != 0 {
		t.Fatalf("failed creation response leaked its pin: %+v", metrics)
	}
}

func TestSessionIDCollisionRetriesWithoutOverwrite(t *testing.T) {
	ids := []string{testSessionIDA, testSessionIDA, testSessionIDB}
	var mu sync.Mutex
	store := newSessionStoreWithOptions(sessionStoreOptions{NewSessionID: func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}})
	original := readySessionEntry(t)
	id, err := store.create(original)
	if err != nil || id != testSessionIDA {
		t.Fatalf("first ID = %s, %v", id, err)
	}
	replacement := readySessionEntry(t)
	id, err = store.create(replacement)
	if err != nil || id != testSessionIDB {
		t.Fatalf("collision retry ID = %s, %v", id, err)
	}
	if found, _ := store.lookup(testSessionIDA); found != original {
		t.Fatal("ID collision overwrote the original session")
	}

	full := newSessionStoreWithOptions(sessionStoreOptions{Max: 1})
	fullOriginal := readySessionEntry(t)
	_, _ = full.insert(testSessionIDA, fullOriginal)
	if inserted, err := full.insert(testSessionIDA, readySessionEntry(t)); err != nil || inserted {
		t.Fatalf("full-store collision = %v, %v", inserted, err)
	}
	if found, _ := full.lookup(testSessionIDA); found != fullOriginal || fullOriginal.state == sessionStateEvicted {
		t.Fatal("collision evicted the existing full-capacity session")
	}
}

func TestDisposeDuringActiveOperationDefersCleanupAndPreventsResurrection(t *testing.T) {
	store := newSessionStore()
	entry := readySessionEntry(t)
	_, _ = store.insert(testSessionIDA, entry)
	lease, ok := store.acquire(testSessionIDA)
	if !ok {
		t.Fatal("lease rejected")
	}
	if !store.remove(testSessionIDA, sessionStateDisposed) {
		t.Fatal("dispose did not find active session")
	}
	if entry.session == nil || entry.pipeline == nil {
		t.Fatal("dispose cleared state while an operation still owned it")
	}
	if _, found := store.acquire(testSessionIDA); found {
		t.Fatal("disposed ID was resurrected")
	}
	lease.release()
	if entry.session != nil || entry.pipeline != nil || entry.input != "" || entry.state != sessionStateDisposed {
		t.Fatalf("dispose did not release large state: %+v", entry)
	}
}

func TestCompleteRacingNextCannotExecuteAnotherPhase(t *testing.T) {
	hooked := make(chan string, 2)
	store := newSessionStoreWithOptions(sessionStoreOptions{BeforeEntryLock: func(id string) { hooked <- id }})
	pipeline := compiler.NewCompilationSession(apiProgram)
	firstSnapshot := pipeline.NextPhase()
	entry := &sessionEntry{pipeline: pipeline}
	entry.updatePipelineState(firstSnapshot)
	_, _ = store.insert(testSessionIDA, entry)

	completeEntered := make(chan struct{})
	releaseComplete := make(chan struct{})
	completeResponse := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		request := sessionRequest("/api/pipeline/complete", testSessionIDA, "")
		store.withPipeline(recorder, request, sessionOperationPipelineComplete, func(pipeline *compiler.CompilationSession) compiler.PipelineSnapshot {
			close(completeEntered)
			<-releaseComplete
			return pipeline.Complete()
		})
		completeResponse <- recorder
	}()
	<-hooked
	<-completeEntered

	nextResponse := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		request := sessionRequest("/api/pipeline/next", testSessionIDA, "")
		store.withPipeline(recorder, request, sessionOperationPipelineNext, func(pipeline *compiler.CompilationSession) compiler.PipelineSnapshot {
			return pipeline.NextPhase()
		})
		nextResponse <- recorder
	}()
	<-hooked
	close(releaseComplete)
	if recorder := receiveRecorder(t, completeResponse); recorder.Code != http.StatusOK {
		t.Fatalf("complete returned %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder := receiveRecorder(t, nextResponse); recorder.Code != http.StatusConflict {
		t.Fatalf("queued next returned %d: %s", recorder.Code, recorder.Body.String())
	}
	if entry.state != sessionStateReady {
		t.Fatalf("final pipeline state = %s", entry.state)
	}
}

func TestRunRacesSerializeStepResetAndInput(t *testing.T) {
	tests := []struct {
		name      string
		operation sessionOperation
		input     string
		action    func(*sessionEntry) compiler.DebugSnapshot
	}{
		{"step", sessionOperationStep, "", func(entry *sessionEntry) compiler.DebugSnapshot { return entry.session.Step() }},
		{"reset", sessionOperationReset, "", func(entry *sessionEntry) compiler.DebugSnapshot { return entry.session.Reset() }},
		{"input", sessionOperationStep, "replacement\n", func(entry *sessionEntry) compiler.DebugSnapshot { return entry.session.Step() }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hooked := make(chan string, 2)
			store := newSessionStoreWithOptions(sessionStoreOptions{BeforeEntryLock: func(id string) { hooked <- id }})
			entry := readySessionEntry(t)
			_, _ = store.insert(testSessionIDA, entry)
			entered := make(chan struct{})
			release := make(chan struct{})
			order := make(chan string, 2)
			firstDone := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				recorder := httptest.NewRecorder()
				store.withSession(recorder, sessionRequest("/api/vm/run", testSessionIDA, ""), sessionOperationRun, func(entry *sessionEntry, _ debugSessionRequest) compiler.DebugSnapshot {
					order <- "run"
					close(entered)
					<-release
					return entry.session.Snapshot(false, 0)
				})
				firstDone <- recorder
			}()
			<-hooked
			<-entered
			if got := <-order; got != "run" {
				t.Fatalf("first operation = %s", got)
			}
			secondDone := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				recorder := httptest.NewRecorder()
				store.withSession(recorder, sessionRequest("/api/vm/action", testSessionIDA, test.input), test.operation, func(entry *sessionEntry, _ debugSessionRequest) compiler.DebugSnapshot {
					order <- test.name
					return test.action(entry)
				})
				secondDone <- recorder
			}()
			<-hooked
			select {
			case got := <-order:
				t.Fatalf("queued %s ran before release; order item=%s", test.name, got)
			default:
			}
			close(release)
			if recorder := receiveRecorder(t, firstDone); recorder.Code != http.StatusOK {
				t.Fatalf("run status = %d", recorder.Code)
			}
			if recorder := receiveRecorder(t, secondDone); recorder.Code != http.StatusOK {
				t.Fatalf("%s status = %d body=%s", test.name, recorder.Code, recorder.Body.String())
			}
			if second := <-order; second != test.name {
				t.Fatalf("second operation = %s", second)
			}
			if test.input != "" && entry.input != test.input {
				t.Fatalf("serialized input = %q", entry.input)
			}
		})
	}
}

func TestPipelinePromotionIsAtomicAcrossQueuedVMRequests(t *testing.T) {
	hooked := make(chan string, 2)
	store := newSessionStoreWithOptions(sessionStoreOptions{BeforeEntryLock: func(id string) { hooked <- id }})
	pipeline := compiler.NewCompilationSession(apiProgram)
	entry := &sessionEntry{pipeline: pipeline, state: sessionStateCreated}
	_, _ = store.insert(testSessionIDA, entry)
	executed := make(chan int, 2)
	responses := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() {
			recorder := httptest.NewRecorder()
			store.withSession(recorder, sessionRequest("/api/vm/run", testSessionIDA, ""), sessionOperationRun, func(entry *sessionEntry, _ debugSessionRequest) compiler.DebugSnapshot {
				snapshot := entry.session.Run(1, nil)
				executed <- snapshot.Runtime.ExecutedSteps
				return snapshot
			})
			responses <- recorder
		}()
	}
	<-hooked
	<-hooked
	for range 2 {
		if recorder := receiveRecorder(t, responses); recorder.Code != http.StatusOK {
			t.Fatalf("promoted run status = %d body=%s", recorder.Code, recorder.Body.String())
		}
	}
	first, second := <-executed, <-executed
	if first > second {
		first, second = second, first
	}
	if first != 1 || second != 2 || entry.session == nil || entry.pipeline == nil {
		t.Fatalf("atomic promotion executed=%d,%d entry=%+v", first, second, entry)
	}
}

func TestSessionRandomizedModelStress(t *testing.T) {
	store := newSessionStore()
	entry := readySessionEntryForSource(t, `class Main { main() : Object { while true loop 0 pool }; };`)
	_, _ = store.insert(testSessionIDA, entry)
	model := sessionStateReady
	random := rand.New(rand.NewSource(33))
	for index := 0; index < 500; index++ {
		choice := random.Intn(5)
		operation := sessionOperationRun
		request := sessionRequest("/api/vm/run", testSessionIDA, "")
		action := func(entry *sessionEntry, _ debugSessionRequest) compiler.DebugSnapshot {
			return entry.session.Run(1, nil)
		}
		valid := model == sessionStateReady || model == sessionStatePaused
		wantState := sessionStatePaused
		switch choice {
		case 1:
			operation = sessionOperationStep
			action = func(entry *sessionEntry, _ debugSessionRequest) compiler.DebugSnapshot { return entry.session.Step() }
			wantState = sessionStateReady
		case 2:
			operation = sessionOperationReset
			action = func(entry *sessionEntry, _ debugSessionRequest) compiler.DebugSnapshot { return entry.session.Reset() }
			valid = model == sessionStateReady || model == sessionStatePaused || model == sessionStateFailed || model == sessionStateHalted
			wantState = sessionStateReady
		case 3:
			operation = sessionOperationPipelineNext
			valid = false
		case 4:
			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			request = request.WithContext(cancelled)
			action = func(entry *sessionEntry, _ debugSessionRequest) compiler.DebugSnapshot {
				return entry.session.RunWithBreakpointsContext(cancelled, 1, nil)
			}
			wantState = sessionStateFailed
		}
		recorder := httptest.NewRecorder()
		if operation == sessionOperationPipelineNext {
			store.withPipeline(recorder, request, operation, func(pipeline *compiler.CompilationSession) compiler.PipelineSnapshot { return pipeline.NextPhase() })
		} else {
			store.withSession(recorder, request, operation, action)
		}
		wantStatus := http.StatusConflict
		if valid {
			wantStatus = http.StatusOK
			model = wantState
		}
		if recorder.Code != wantStatus || entry.state != model {
			t.Fatalf("step=%d choice=%d HTTP=%d want=%d state=%s want=%s body=%s", index, choice, recorder.Code, wantStatus, entry.state, model, recorder.Body.String())
		}
	}
}

func TestServerShutdownWaitsForActiveSessionAndCleanupReleasesIt(t *testing.T) {
	store := newSessionStore()
	entry := readySessionEntry(t)
	_, _ = store.insert(testSessionIDA, entry)
	entered := make(chan struct{})
	release := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		lease, ok := store.acquire(testSessionIDA)
		if !ok {
			http.Error(w, "missing", http.StatusNotFound)
			return
		}
		close(entered)
		<-release
		lease.release()
		w.WriteHeader(http.StatusNoContent)
	})
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := newHTTPServer(handler)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	requestDone := make(chan error, 1)
	go func() {
		request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+listener.Addr().String(), nil)
		if err != nil {
			requestDone <- err
			return
		}
		response, err := http.DefaultClient.Do(request)
		if response != nil {
			response.Body.Close()
		}
		requestDone <- err
	}()
	<-entered
	shutdownStarted := make(chan struct{})
	server.RegisterOnShutdown(func() { close(shutdownStarted) })
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- server.Shutdown(context.Background()) }()
	<-shutdownStarted
	select {
	case err := <-shutdownDone:
		t.Fatalf("shutdown returned before active request: %v", err)
	default:
	}
	close(release)
	if err := <-requestDone; err != nil {
		t.Fatal(err)
	}
	if err := <-shutdownDone; err != nil {
		t.Fatal(err)
	}
	if err := <-serveDone; !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("serve result = %v", err)
	}
	store.close()
	if entry.pipeline != nil || entry.session != nil || entry.state != sessionStateDisposed {
		t.Fatalf("shutdown retained session state: %+v", entry)
	}
}

func TestCancellationAndResponseFailureReleaseSessionLock(t *testing.T) {
	store := newSessionStore()
	entry := readySessionEntryForSource(t, `class Main { main() : Object { while true loop 0 pool }; };`)
	_, _ = store.insert(testSessionIDA, entry)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodPost, "/api/vm/run", strings.NewReader(`{"id":"`+testSessionIDA+`","maxSteps":1000}`)).WithContext(ctx)
	recorder := httptest.NewRecorder()
	store.runHandler(recorder, request)
	if recorder.Code != http.StatusOK || entry.state != sessionStateFailed {
		t.Fatalf("cancelled run = HTTP %d state %s body=%s", recorder.Code, entry.state, recorder.Body.String())
	}
	if !entry.mu.TryLock() {
		t.Fatal("cancelled request leaked the session lock")
	}
	entry.mu.Unlock()

	resetRequest := httptest.NewRequest(http.MethodPost, "/api/vm/reset", strings.NewReader(`{"id":"`+testSessionIDA+`"}`))
	store.resetHandler(failingResponseWriter{}, resetRequest)
	if !entry.mu.TryLock() {
		t.Fatal("response write failure leaked the session lock")
	}
	entry.mu.Unlock()
}

func TestDecodeAndRuntimeFailuresReleaseLocksAndInvalidTransitionsAreStable(t *testing.T) {
	store := newSessionStore()
	entry := readySessionEntryForSource(t, `class Main { main() : Object { 1 / 0 }; };`)
	_, _ = store.insert(testSessionIDA, entry)

	malformed := httptest.NewRequest(http.MethodPost, "/api/vm/run", strings.NewReader(`{"id":`))
	malformedResponse := httptest.NewRecorder()
	store.runHandler(malformedResponse, malformed)
	if malformedResponse.Code != http.StatusBadRequest || !entry.mu.TryLock() {
		t.Fatalf("malformed request status=%d leaked lock", malformedResponse.Code)
	}
	entry.mu.Unlock()

	runtimeResponse := httptest.NewRecorder()
	store.runHandler(runtimeResponse, sessionRequest("/api/vm/run", testSessionIDA, ""))
	if runtimeResponse.Code != http.StatusOK || entry.state != sessionStateFailed || !entry.mu.TryLock() {
		t.Fatalf("runtime failure status=%d state=%s leaked lock", runtimeResponse.Code, entry.state)
	}
	entry.mu.Unlock()

	invalidResponse := httptest.NewRecorder()
	store.stepHandler(invalidResponse, sessionRequest("/api/vm/step", testSessionIDA, ""))
	if invalidResponse.Code != http.StatusConflict || !strings.Contains(invalidResponse.Body.String(), `"operation":"step"`) || !strings.Contains(invalidResponse.Body.String(), `"state":"failed"`) {
		t.Fatalf("invalid transition response = %d %s", invalidResponse.Code, invalidResponse.Body.String())
	}

	resetResponse := httptest.NewRecorder()
	store.resetHandler(resetResponse, sessionRequest("/api/vm/reset", testSessionIDA, ""))
	if resetResponse.Code != http.StatusOK || entry.state != sessionStateReady {
		t.Fatalf("reset after runtime failure = %d state=%s body=%s", resetResponse.Code, entry.state, resetResponse.Body.String())
	}
}

type failingResponseWriter struct{}

func (failingResponseWriter) Header() http.Header { return http.Header{} }
func (failingResponseWriter) WriteHeader(int)     {}
func (failingResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("injected response failure")
}

func readySessionEntry(t testing.TB) *sessionEntry {
	t.Helper()
	return readySessionEntryForSource(t, apiProgram)
}

func readySessionEntryForSource(t testing.TB, source string) *sessionEntry {
	t.Helper()
	pipeline := compiler.NewCompilationSession(source)
	snapshot := pipeline.Complete()
	if snapshot.Blocked {
		t.Fatalf("test pipeline blocked: %+v", snapshot.Result.Diagnostics)
	}
	session, ok := pipeline.DebugSession()
	if !ok {
		t.Fatal("test debug session unavailable")
	}
	entry := &sessionEntry{pipeline: pipeline, session: session}
	entry.updateDebugState(session.Snapshot(false, 0))
	return entry
}

func receiveLease(t testing.TB, channel <-chan *sessionLease) *sessionLease {
	t.Helper()
	select {
	case lease := <-channel:
		if lease == nil {
			t.Fatal("nil lease")
		}
		return lease
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for session lease")
		return nil
	}
}

func receiveRecorder(t testing.TB, channel <-chan *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	t.Helper()
	select {
	case recorder := <-channel:
		return recorder
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for session response")
		return nil
	}
}

func sessionRequest(path, id, input string) *http.Request {
	body := `{"id":"` + id + `"}`
	if input != "" {
		body = `{"id":"` + id + `","input":` + strconv.Quote(input) + `}`
	}
	return httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
}
