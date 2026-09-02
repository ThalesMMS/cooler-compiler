// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
)

type sessionState string

const (
	sessionStateCreated      sessionState = "created"
	sessionStateCompiling    sessionState = "compiling"
	sessionStateReady        sessionState = "ready"
	sessionStateRunning      sessionState = "running"
	sessionStatePaused       sessionState = "paused"
	sessionStateWaitingInput sessionState = "waiting-input"
	sessionStateHalted       sessionState = "halted"
	sessionStateFailed       sessionState = "failed"
	sessionStateDisposed     sessionState = "disposed"
	sessionStateEvicted      sessionState = "evicted"
	sessionStateExpired      sessionState = "expired"
)

type sessionOperation string

const (
	sessionOperationPipelineNext     sessionOperation = "pipeline-next"
	sessionOperationPipelineComplete sessionOperation = "pipeline-complete"
	sessionOperationStep             sessionOperation = "step"
	sessionOperationRun              sessionOperation = "run"
	sessionOperationReset            sessionOperation = "reset"
	sessionOperationInput            sessionOperation = "input"
	sessionOperationStepInto         sessionOperation = "step-into"
	sessionOperationStepOver         sessionOperation = "step-over"
	sessionOperationStepOut          sessionOperation = "step-out"
	sessionOperationRunToCursor      sessionOperation = "run-to-cursor"
	sessionOperationHeap             sessionOperation = "heap"
	sessionOperationStepBack         sessionOperation = "step-back"
	sessionOperationReverseContinue  sessionOperation = "reverse-continue"
)

var (
	errSessionStoreClosed = errors.New("session store is closed")
	errSessionCapacity    = errors.New("session capacity is occupied by active operations")
	errSessionIDCollision = errors.New("could not allocate a unique session id")
)

type sessionEntry struct {
	mu          sync.Mutex
	pipeline    *compiler.CompilationSession
	session     *compiler.DebugSession
	input       string
	interactive bool
	state       sessionState
	resettable  bool

	// The store mutex guards lifecycle bookkeeping below. Mutable compiler/VM
	// state above is guarded by the entry mutex.
	lastUsed time.Time
	active   int
	removed  sessionState
}

type sessionStoreOptions struct {
	Max             int
	TTL             time.Duration
	Now             func() time.Time
	NewSessionID    func() (string, error)
	BeforeEntryLock func(string)
}

type sessionStoreSnapshot struct {
	ActiveSessions     int                  `json:"activeSessions"`
	ActiveOperations   int                  `json:"activeOperations"`
	StateDistribution  map[sessionState]int `json:"stateDistribution"`
	Evictions          uint64               `json:"evictions"`
	Expirations        uint64               `json:"expirations"`
	RejectedOperations uint64               `json:"rejectedOperations"`
}

type sessionStore struct {
	mu          sync.Mutex
	sessions    map[string]*sessionEntry
	max         int
	ttl         time.Duration
	now         func() time.Time
	newID       func() (string, error)
	beforeLock  func(string)
	closed      bool
	evictions   uint64
	expirations uint64
	rejections  uint64
}

type sessionLease struct {
	store    *sessionStore
	entry    *sessionEntry
	released bool
}

type sessionPin struct {
	store    *sessionStore
	entry    *sessionEntry
	released bool
}

func newSessionStore() *sessionStore {
	return newSessionStoreWithOptions(sessionStoreOptions{})
}

func newSessionStoreWithOptions(options sessionStoreOptions) *sessionStore {
	if options.Max <= 0 {
		options.Max = maxDebugSessions
	}
	if options.TTL <= 0 {
		options.TTL = debugSessionTTL
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.NewSessionID == nil {
		options.NewSessionID = newSessionID
	}
	return &sessionStore{
		sessions: map[string]*sessionEntry{}, max: options.Max, ttl: options.TTL,
		now: options.Now, newID: options.NewSessionID, beforeLock: options.BeforeEntryLock,
	}
}

func (s *sessionStore) currentTime() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *sessionStore) idGenerator() func() (string, error) {
	if s.newID != nil {
		return s.newID
	}
	return newSessionID
}

func (s *sessionStore) create(entry *sessionEntry) (string, error) {
	id, pin, err := s.createPinned(entry)
	if pin != nil {
		pin.release()
	}
	return id, err
}

func (s *sessionStore) createPinned(entry *sessionEntry) (string, *sessionPin, error) {
	if entry.state == "" {
		entry.state = sessionStateCreated
	}
	for range 16 {
		id, err := s.idGenerator()()
		if err != nil {
			return "", nil, err
		}
		if !validSessionID(id) {
			continue
		}
		inserted, err := s.insertEntry(id, entry, true)
		if err != nil {
			return "", nil, err
		}
		if inserted {
			return id, &sessionPin{store: s, entry: entry}, nil
		}
	}
	return "", nil, errSessionIDCollision
}

// insert returns false without replacing state when id already exists.
func (s *sessionStore) insert(id string, entry *sessionEntry) (bool, error) {
	return s.insertEntry(id, entry, false)
}

func (s *sessionStore) insertEntry(id string, entry *sessionEntry, pinned bool) (bool, error) {
	now := s.currentTime()
	s.mu.Lock()
	if s.sessions == nil {
		s.sessions = map[string]*sessionEntry{}
	}
	if s.closed {
		s.rejections++
		s.mu.Unlock()
		return false, errSessionStoreClosed
	}
	if _, exists := s.sessions[id]; exists {
		s.mu.Unlock()
		return false, nil
	}
	cleanup := s.pruneLocked(now)
	if len(s.sessions) >= s.maximum() {
		s.rejections++
		s.mu.Unlock()
		cleanupSessionEntries(cleanup)
		return false, errSessionCapacity
	}
	entry.lastUsed = now
	if pinned {
		entry.active++
	}
	s.sessions[id] = entry
	s.mu.Unlock()
	cleanupSessionEntries(cleanup)
	return true, nil
}

func (s *sessionStore) maximum() int {
	if s.max > 0 {
		return s.max
	}
	return maxDebugSessions
}

func (s *sessionStore) lifetime() time.Duration {
	if s.ttl > 0 {
		return s.ttl
	}
	return debugSessionTTL
}

func (s *sessionStore) acquire(id string) (*sessionLease, bool) {
	now := s.currentTime()
	s.mu.Lock()
	entry, found := s.sessions[id]
	cleanup := []*sessionEntry{}
	if found && entry.active == 0 && now.Sub(entry.lastUsed) > s.lifetime() {
		delete(s.sessions, id)
		entry.removed = sessionStateExpired
		s.expirations++
		cleanup = append(cleanup, entry)
		entry, found = nil, false
	}
	if s.closed {
		found = false
	}
	if !found {
		s.rejections++
		s.mu.Unlock()
		cleanupSessionEntries(cleanup)
		return nil, false
	}
	entry.active++
	s.mu.Unlock()
	cleanupSessionEntries(cleanup)
	if s.beforeLock != nil {
		s.beforeLock(id)
	}
	entry.mu.Lock()
	return &sessionLease{store: s, entry: entry}, true
}

func (lease *sessionLease) release() {
	if lease == nil || lease.released {
		return
	}
	lease.released = true
	lease.entry.mu.Unlock()
	lease.store.releaseActive(lease.entry)
}

func (pin *sessionPin) release() {
	if pin == nil || pin.released {
		return
	}
	pin.released = true
	pin.store.releaseActive(pin.entry)
}

func (s *sessionStore) releaseActive(entry *sessionEntry) {
	now := s.currentTime()
	cleanup := false
	s.mu.Lock()
	entry.active--
	if entry.active < 0 {
		panic("negative session active-operation count")
	}
	if entry.removed == "" {
		entry.lastUsed = now
	} else if entry.active == 0 {
		cleanup = true
	}
	s.mu.Unlock()
	if cleanup {
		cleanupSessionEntry(entry)
	}
}

func (s *sessionStore) remove(id string, reason sessionState) bool {
	s.mu.Lock()
	entry, found := s.sessions[id]
	if found {
		delete(s.sessions, id)
		entry.removed = reason
	}
	cleanup := found && entry.active == 0
	s.mu.Unlock()
	if cleanup {
		cleanupSessionEntry(entry)
	}
	return found
}

func (s *sessionStore) lookup(id string) (*sessionEntry, bool) {
	s.mu.Lock()
	entry, found := s.sessions[id]
	s.mu.Unlock()
	return entry, found
}

// pruneLocked removes expired idle entries and then the deterministic LRU idle
// entry until one capacity slot is available. The caller holds s.mu.
func (s *sessionStore) pruneLocked(now time.Time) []*sessionEntry {
	cleanup := []*sessionEntry{}
	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		entry := s.sessions[id]
		if entry.active == 0 && now.Sub(entry.lastUsed) > s.lifetime() {
			delete(s.sessions, id)
			entry.removed = sessionStateExpired
			s.expirations++
			cleanup = append(cleanup, entry)
		}
	}
	for len(s.sessions) >= s.maximum() {
		oldestID := ""
		var oldest time.Time
		for id, entry := range s.sessions {
			if entry.active != 0 {
				continue
			}
			if oldestID == "" || entry.lastUsed.Before(oldest) || entry.lastUsed.Equal(oldest) && id < oldestID {
				oldestID, oldest = id, entry.lastUsed
			}
		}
		if oldestID == "" {
			break
		}
		entry := s.sessions[oldestID]
		delete(s.sessions, oldestID)
		entry.removed = sessionStateEvicted
		s.evictions++
		cleanup = append(cleanup, entry)
	}
	return cleanup
}

func (s *sessionStore) reject() {
	s.mu.Lock()
	s.rejections++
	s.mu.Unlock()
}

func (s *sessionStore) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	cleanup := make([]*sessionEntry, 0, len(s.sessions))
	for id, entry := range s.sessions {
		delete(s.sessions, id)
		entry.removed = sessionStateDisposed
		if entry.active == 0 {
			cleanup = append(cleanup, entry)
		}
	}
	s.mu.Unlock()
	cleanupSessionEntries(cleanup)
}

func (s *sessionStore) snapshot() sessionStoreSnapshot {
	s.mu.Lock()
	entries := make([]*sessionEntry, 0, len(s.sessions))
	activeOperations := 0
	for _, entry := range s.sessions {
		entries = append(entries, entry)
		activeOperations += entry.active
	}
	snapshot := sessionStoreSnapshot{
		ActiveSessions: len(entries), ActiveOperations: activeOperations,
		StateDistribution: map[sessionState]int{}, Evictions: s.evictions,
		Expirations: s.expirations, RejectedOperations: s.rejections,
	}
	s.mu.Unlock()
	for _, entry := range entries {
		state := sessionStateRunning
		if entry.mu.TryLock() {
			state = entry.state
			entry.mu.Unlock()
		}
		snapshot.StateDistribution[state]++
	}
	return snapshot
}

func cleanupSessionEntries(entries []*sessionEntry) {
	for _, entry := range entries {
		cleanupSessionEntry(entry)
	}
}

func cleanupSessionEntry(entry *sessionEntry) {
	if entry == nil {
		return
	}
	entry.mu.Lock()
	entry.pipeline = nil
	entry.session = nil
	entry.input = ""
	entry.interactive = false
	entry.resettable = false
	if entry.removed != "" {
		entry.state = entry.removed
	}
	entry.mu.Unlock()
}

func (entry *sessionEntry) allows(operation sessionOperation) bool {
	switch operation {
	case sessionOperationPipelineNext, sessionOperationPipelineComplete:
		return entry.state == sessionStateCreated || entry.state == sessionStateCompiling
	case sessionOperationStep, sessionOperationRun, sessionOperationStepInto, sessionOperationStepOver, sessionOperationStepOut, sessionOperationRunToCursor:
		return entry.state == sessionStateCreated || entry.state == sessionStateCompiling || entry.state == sessionStateReady || entry.state == sessionStatePaused
	case sessionOperationInput:
		return entry.state == sessionStateWaitingInput
	case sessionOperationReset:
		return entry.resettable && (entry.state == sessionStateReady || entry.state == sessionStatePaused || entry.state == sessionStateWaitingInput || entry.state == sessionStateHalted || entry.state == sessionStateFailed)
	case sessionOperationHeap:
		return entry.resettable && (entry.state == sessionStateReady || entry.state == sessionStatePaused || entry.state == sessionStateWaitingInput || entry.state == sessionStateHalted || entry.state == sessionStateFailed)
	case sessionOperationStepBack, sessionOperationReverseContinue:
		return entry.resettable && (entry.state == sessionStateReady || entry.state == sessionStatePaused || entry.state == sessionStateWaitingInput || entry.state == sessionStateHalted || entry.state == sessionStateFailed)
	default:
		return false
	}
}

func (entry *sessionEntry) updatePipelineState(snapshot compiler.PipelineSnapshot) {
	switch {
	case snapshot.Blocked:
		entry.state = sessionStateFailed
		entry.resettable = false
	case snapshot.Complete:
		entry.state = sessionStateReady
		entry.resettable = true
	case snapshot.CurrentPhase == compiler.PhaseInitial:
		entry.state = sessionStateCreated
	default:
		entry.state = sessionStateCompiling
	}
}

func (entry *sessionEntry) updateDebugState(snapshot compiler.DebugSnapshot) {
	entry.resettable = snapshot.Status != "blocked"
	switch {
	case snapshot.Status == "blocked":
		entry.state = sessionStateFailed
	case hasRuntimeErrors(snapshot.Runtime.Diagnostics):
		entry.state = sessionStateFailed
	case snapshot.Runtime.Halted:
		entry.state = sessionStateHalted
	case snapshot.Runtime.WaitingInput != nil:
		entry.state = sessionStateWaitingInput
	case snapshot.Runtime.Paused:
		entry.state = sessionStatePaused
	default:
		entry.state = sessionStateReady
	}
}

func hasRuntimeErrors(diagnostics []compiler.Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == compiler.SeverityError {
			return true
		}
	}
	return false
}
