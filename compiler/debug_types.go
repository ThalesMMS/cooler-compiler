// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import "context"

// Breakpoint describes a source stop, condition, hit threshold, or logpoint.
type Breakpoint struct {
	ID         string `json:"id,omitempty"`
	SourceID   string `json:"sourceId,omitempty"`
	Line       int    `json:"line"`
	Column     int    `json:"column,omitempty"`
	Condition  string `json:"condition,omitempty"`
	HitCount   int    `json:"hitCount,omitempty"`
	LogMessage string `json:"logMessage,omitempty"`
	Pause      bool   `json:"pause,omitempty"`
	Enabled    *bool  `json:"enabled,omitempty"`
}

// BreakpointState reports breakpoint verification and observed hit count.
type BreakpointState struct {
	Breakpoint Breakpoint `json:"breakpoint"`
	Verified   bool       `json:"verified"`
	Diagnostic string     `json:"diagnostic,omitempty"`
	Hits       int        `json:"hits"`
}

// SourcePosition selects a source line and optional column within a unit.
type SourcePosition struct {
	SourceID string `json:"sourceId"`
	Line     int    `json:"line"`
	Column   int    `json:"column,omitempty"`
}

// DebugLocation maps the current VM instruction back to source and frame depth.
type DebugLocation struct {
	SourceID    string `json:"sourceId,omitempty"`
	Span        Span   `json:"span"`
	Method      string `json:"method"`
	Instruction int    `json:"instruction"`
	FrameDepth  int    `json:"frameDepth"`
	Synthetic   bool   `json:"synthetic"`
}

// StopReason identifies why debugger execution returned control to the caller.
type StopReason string

const (
	// StopReady means the VM is compiled and has not started execution.
	StopReady StopReason = "ready"
	// StopInstruction means one VM instruction completed.
	StopInstruction StopReason = "instruction-step"
	// StopStepInto means source stepping reached the next executable position.
	StopStepInto StopReason = "step-into"
	// StopStepOver means execution reached the next position outside deeper calls.
	StopStepOver StopReason = "step-over"
	// StopStepOut means execution returned from the starting frame.
	StopStepOut StopReason = "step-out"
	// StopRunToCursor means execution reached the requested source position.
	StopRunToCursor StopReason = "run-to-cursor"
	// StopBreakpoint means an enabled pausing breakpoint matched.
	StopBreakpoint StopReason = "breakpoint"
	// StopWaitingInput means the VM requires a live input line.
	StopWaitingInput StopReason = "waiting-input"
	// StopInstructionLimit means the requested execution budget was exhausted.
	StopInstructionLimit StopReason = "instruction-limit"
	// StopRuntimeError means a COOL runtime diagnostic halted execution.
	StopRuntimeError StopReason = "runtime-error"
	// StopHalt means Main.main completed normally.
	StopHalt StopReason = "halt"
	// StopCancelled means context cancellation or Pause interrupted execution.
	StopCancelled StopReason = "cancelled"
	// StopReverseStep means history navigation restored an earlier stop.
	StopReverseStep StopReason = "reverse-step"
	// StopReverseContinue means reverse execution reached an earlier meaningful
	// stop.
	StopReverseContinue StopReason = "reverse-continue"
)

// WatchExpression is an expression evaluated whenever the debugger stops.
type WatchExpression struct {
	ID         string `json:"id"`
	Expression string `json:"expression"`
	Enabled    *bool  `json:"enabled,omitempty"`
}

// WatchResult is the value or diagnostic produced for one watch expression.
type WatchResult struct {
	ID           string         `json:"id"`
	Expression   string         `json:"expression"`
	Enabled      bool           `json:"enabled"`
	Value        *RuntimeValue  `json:"value,omitempty"`
	StaticType   string         `json:"staticType,omitempty"`
	DynamicType  string         `json:"dynamicType,omitempty"`
	Diagnostic   string         `json:"diagnostic,omitempty"`
	Dependencies []string       `json:"dependencies,omitempty"`
	Location     *DebugLocation `json:"location,omitempty"`
}

// DebugLogEntry is a deterministic logpoint message emitted during execution.
type DebugLogEntry struct {
	Order    int    `json:"order"`
	Message  string `json:"message"`
	SourceID string `json:"sourceId,omitempty"`
	Line     int    `json:"line"`
	Method   string `json:"method,omitempty"`
	Step     int    `json:"step"`
}

// ReverseMode selects instruction-level or source-level history navigation.
type ReverseMode string

const (
	// ReverseInstruction restores exactly one earlier VM instruction.
	ReverseInstruction ReverseMode = "instruction"
	// ReverseSource restores the preceding distinct executable source position.
	ReverseSource ReverseMode = "source"
)

// ReverseHistoryLimits bounds checkpoint and event memory retained per session.
type ReverseHistoryLimits struct {
	CheckpointInterval int `json:"checkpointInterval"`
	MaxCheckpoints     int `json:"maxCheckpoints"`
	MaxCheckpointBytes int `json:"maxCheckpointBytes"`
	MaxEvents          int `json:"maxEvents"`
	MaxEventBytes      int `json:"maxEventBytes"`
}

// ReverseTimeline describes the retained history window and current branch.
type ReverseTimeline struct {
	Revision               uint64               `json:"revision"`
	CurrentStep            int                  `json:"currentStep"`
	LatestStep             int                  `json:"latestStep"`
	EarliestReversibleStep int                  `json:"earliestReversibleStep"`
	CanStepBack            bool                 `json:"canStepBack"`
	CanReverseContinue     bool                 `json:"canReverseContinue"`
	FutureDiscarded        bool                 `json:"futureDiscarded,omitempty"`
	CheckpointCount        int                  `json:"checkpointCount"`
	CheckpointBytes        int                  `json:"checkpointBytes"`
	EventCount             int                  `json:"eventCount"`
	EventBytes             int                  `json:"eventBytes"`
	StateHash              string               `json:"stateHash"`
	ExecutableHash         string               `json:"executableHash"`
	Limits                 ReverseHistoryLimits `json:"limits"`
}

// DebugSnapshot is a detached debugger stop including runtime and optional
// heap-independent debugger metadata.
type DebugSnapshot struct {
	Result      Result            `json:"result"`
	Runtime     RuntimeResult     `json:"runtime"`
	Status      string            `json:"status"`
	Location    *DebugLocation    `json:"location,omitempty"`
	StopReason  StopReason        `json:"stopReason,omitempty"`
	Breakpoints []BreakpointState `json:"breakpoints,omitempty"`
	Watches     []WatchResult     `json:"watches,omitempty"`
	DebugLog    []DebugLogEntry   `json:"debugLog,omitempty"`
	Timeline    *ReverseTimeline  `json:"timeline,omitempty"`
}

// InputEvent records a supplied or synthesized input line. Line is omitted
// unless the caller explicitly requests input content.
type InputEvent struct {
	Sequence      int    `json:"sequence"`
	Builtin       string `json:"builtin"`
	Line          string `json:"line,omitempty"`
	Span          Span   `json:"span"`
	InputPosition int    `json:"inputPosition"`
	Live          bool   `json:"live"`
}

// HeapLimits bounds object traversal, field expansion, strings, and bytes.
type HeapLimits struct {
	MaxObjects    int `json:"maxObjects"`
	MaxFields     int `json:"maxFieldsPerObject"`
	MaxDepth      int `json:"maxDepth"`
	StringPreview int `json:"stringPreview"`
	MaxBytes      int `json:"maxBytes"`
}

// HeapQuery selects a bounded heap view by mode, object ID, class, and offset.
type HeapQuery struct {
	Mode   string     `json:"mode"`
	ID     string     `json:"id,omitempty"`
	Class  string     `json:"class,omitempty"`
	Offset int        `json:"offset,omitempty"`
	Limits HeapLimits `json:"limits"`
}

// HeapValue is a bounded scalar or object reference in a heap snapshot.
type HeapValue struct {
	Kind        string `json:"kind"`
	Type        string `json:"type,omitempty"`
	Display     string `json:"display"`
	ObjectID    string `json:"objectId,omitempty"`
	FullLength  int    `json:"fullLength,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
	Unavailable bool   `json:"unavailable,omitempty"`
}

// HeapField is one declared object field and its current value.
type HeapField struct {
	Name       string    `json:"name"`
	Owner      string    `json:"owner"`
	StaticType string    `json:"staticType"`
	Span       Span      `json:"span"`
	Value      HeapValue `json:"value"`
}

// HeapObject is a bounded, read-only object projection with stable session ID.
type HeapObject struct {
	ID              string      `json:"id"`
	Class           string      `json:"class"`
	AllocationOrder int         `json:"allocationOrder"`
	AllocationSpan  Span        `json:"allocationSpan"`
	Fields          []HeapField `json:"fields"`
	Summary         string      `json:"summary"`
	Reachable       bool        `json:"reachable"`
	Truncated       bool        `json:"truncated,omitempty"`
	OmittedFields   int         `json:"omittedFields,omitempty"`
}

// HeapRoot identifies a frame, scope, or operand-stack reference into the heap.
type HeapRoot struct {
	Kind       string    `json:"kind"`
	Label      string    `json:"label"`
	Frame      *int      `json:"frame,omitempty"`
	Scope      *int      `json:"scope,omitempty"`
	StackIndex *int      `json:"stackIndex,omitempty"`
	Value      HeapValue `json:"value"`
}

// HeapSnapshot is a bounded, paginated, read-only heap projection.
type HeapSnapshot struct {
	Mode         string       `json:"mode"`
	Objects      []HeapObject `json:"objects"`
	Roots        []HeapRoot   `json:"roots"`
	TotalObjects int          `json:"totalObjects"`
	Reachable    int          `json:"reachableObjects"`
	Truncated    bool         `json:"truncated"`
	NextOffset   int          `json:"nextOffset,omitempty"`
	Serialized   int          `json:"serializedBytes"`
	Limits       HeapLimits   `json:"limits"`
}

// HeapInspector is an optional debugger capability kept separate so future
// implementations can add heap inspection without expanding Debugger's core.
type HeapInspector interface {
	Heap(context.Context, HeapQuery) (HeapSnapshot, error)
}

// WatchController is an optional debugger capability for managing expressions
// evaluated after each stop.
type WatchController interface {
	SetWatches(context.Context, []WatchExpression) error
}

// ReverseDebugger is an optional debugger capability for history navigation.
type ReverseDebugger interface {
	StepBack(context.Context, ReverseMode) (DebugSnapshot, error)
	ReverseContinue(context.Context) (DebugSnapshot, error)
}
