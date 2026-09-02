// SPDX-License-Identifier: AGPL-3.0-or-later

// Package conformance loads, validates, and executes the repository-owned COOL
// conformance matrix and fixture corpus.
package conformance

import "github.com/ThalesMMS/cooler-compiler/internal/compiler"

type Matrix struct {
	SchemaVersion      string            `json:"schemaVersion"`
	Project            string            `json:"project"`
	NormativeHierarchy []NormativeSource `json:"normativeHierarchy"`
	StatusDefinitions  map[string]string `json:"statusDefinitions"`
	ManualCoverage     []ManualSection   `json:"manualCoverage"`
	Claims             []DocumentClaim   `json:"claims"`
	Rules              []Rule            `json:"rules"`
}

type ManualSection struct {
	Section       string   `json:"section"`
	Title         string   `json:"title"`
	Applicability string   `json:"applicability"`
	RuleIDs       []string `json:"ruleIds,omitempty"`
	Rationale     string   `json:"rationale,omitempty"`
}

type NormativeSource struct {
	Rank    int    `json:"rank"`
	ID      string `json:"id"`
	Title   string `json:"title"`
	Locator string `json:"locator"`
	Role    string `json:"role"`
}

type DocumentClaim struct {
	ID       string   `json:"id"`
	Document string   `json:"document"`
	Contains string   `json:"contains"`
	RuleIDs  []string `json:"ruleIds"`
}

type Citation struct {
	Source  string `json:"source"`
	Section string `json:"section"`
}

type Rule struct {
	ID                  string      `json:"id"`
	Name                string      `json:"name"`
	Citation            Citation    `json:"citation"`
	ExpectedStage       string      `json:"expectedStage"`
	Behavior            string      `json:"behavior"`
	Status              string      `json:"status"`
	PositiveFixtures    []string    `json:"positiveFixtures"`
	NegativeFixtures    []string    `json:"negativeFixtures"`
	ExpectedDiagnostics []string    `json:"expectedDiagnostics"`
	ExpectedOutput      string      `json:"expectedOutput,omitempty"`
	ReferenceComparison string      `json:"referenceComparison"`
	Divergence          *Divergence `json:"divergence,omitempty"`
	Issue               string      `json:"issue,omitempty"`
}

type Divergence struct {
	Boundary    string `json:"boundary"`
	Reason      string `json:"reason"`
	Consequence string `json:"consequence"`
	Assertion   string `json:"assertion"`
}

type Fixture struct {
	SchemaVersion                string               `json:"schemaVersion"`
	ID                           string               `json:"id"`
	Title                        string               `json:"title"`
	RuleIDs                      []string             `json:"ruleIds"`
	Validity                     string               `json:"validity"`
	RunThrough                   string               `json:"runThrough,omitempty"`
	ExpectedLastSuccessfulStage  string               `json:"expectedLastSuccessfulStage"`
	ExpectedDiagnostics          []ExpectedDiagnostic `json:"expectedDiagnostics"`
	ExpectedTokens               []ExpectedToken      `json:"expectedTokens,omitempty"`
	ExpectedAST                  []ExpectedASTNode    `json:"expectedAst,omitempty"`
	ExpectedOutput               string               `json:"expectedOutput,omitempty"`
	ExpectedRuntimeStatus        string               `json:"expectedRuntimeStatus,omitempty"`
	VMPolicy                     *compiler.VMPolicy   `json:"vmPolicy,omitempty"`
	RunSteps                     []int                `json:"runSteps,omitempty"`
	Breakpoints                  []int                `json:"breakpoints,omitempty"`
	ExpectedTraceOffsetMinimum   int                  `json:"expectedTraceOffsetMinimum,omitempty"`
	ExpectedRetainedTraceMaximum *int                 `json:"expectedRetainedTraceMaximum,omitempty"`
	DebuggerScenario             *DebuggerScenario    `json:"debuggerScenario,omitempty"`
	ExpectedArtifacts            []string             `json:"expectedArtifacts,omitempty"`
	ExpectedSourceUnits          []string             `json:"expectedSourceUnits,omitempty"`
	Sources                      []string             `json:"sources"`
	InputLines                   []string             `json:"inputLines,omitempty"`
	Provenance                   string               `json:"provenance"`
	metadataPath                 string
}

type DebuggerScenario struct {
	MaxActions      int                            `json:"maxActions"`
	MaxInstructions int                            `json:"maxInstructions"`
	ReverseLimits   *compiler.ReverseHistoryLimits `json:"reverseLimits,omitempty"`
	Actions         []DebuggerAction               `json:"actions"`
}

type DebuggerAction struct {
	Op          string                     `json:"op"`
	Steps       int                        `json:"steps,omitempty"`
	Count       int                        `json:"count,omitempty"`
	Input       string                     `json:"input,omitempty"`
	Interactive *bool                      `json:"interactive,omitempty"`
	Breakpoints []compiler.Breakpoint      `json:"breakpoints,omitempty"`
	Watches     []compiler.WatchExpression `json:"watches,omitempty"`
	Cursor      *compiler.SourcePosition   `json:"cursor,omitempty"`
	Heap        *compiler.HeapQuery        `json:"heap,omitempty"`
	ReverseMode compiler.ReverseMode       `json:"reverseMode,omitempty"`
	Expect      DebuggerExpectation        `json:"expect"`
}

type DebuggerExpectation struct {
	ErrorContains             string            `json:"errorContains,omitempty"`
	PipelinePhase             string            `json:"pipelinePhase,omitempty"`
	Complete                  *bool             `json:"complete,omitempty"`
	Blocked                   *bool             `json:"blocked,omitempty"`
	StopReason                string            `json:"stopReason,omitempty"`
	Status                    string            `json:"status,omitempty"`
	ExecutedSteps             *int              `json:"executedSteps,omitempty"`
	MinimumExecutedSteps      int               `json:"minimumExecutedSteps,omitempty"`
	Halted                    *bool             `json:"halted,omitempty"`
	Paused                    *bool             `json:"paused,omitempty"`
	WaitingInput              *bool             `json:"waitingInput,omitempty"`
	WaitingBuiltin            string            `json:"waitingBuiltin,omitempty"`
	WaitingInputPosition      *int              `json:"waitingInputPosition,omitempty"`
	WaitingSequence           *int              `json:"waitingSequence,omitempty"`
	InputEvents               *int              `json:"inputEvents,omitempty"`
	SourceID                  string            `json:"sourceId,omitempty"`
	Line                      int               `json:"line,omitempty"`
	Method                    string            `json:"method,omitempty"`
	Instruction               *int              `json:"instruction,omitempty"`
	FrameDepth                *int              `json:"frameDepth,omitempty"`
	Synthetic                 *bool             `json:"synthetic,omitempty"`
	Output                    []string          `json:"output,omitempty"`
	Stack                     []string          `json:"stack,omitempty"`
	Diagnostics               []string          `json:"diagnostics,omitempty"`
	DebugLog                  []string          `json:"debugLog,omitempty"`
	WatchValues               map[string]string `json:"watchValues,omitempty"`
	WatchDiagnostics          []string          `json:"watchDiagnostics,omitempty"`
	BreakpointHits            map[string]int    `json:"breakpointHits,omitempty"`
	BreakpointVerified        map[string]bool   `json:"breakpointVerified,omitempty"`
	MinimumTraceOffset        int               `json:"minimumTraceOffset,omitempty"`
	RetainedTraceMaximum      *int              `json:"retainedTraceMaximum,omitempty"`
	TimelineRevisionMinimum   uint64            `json:"timelineRevisionMinimum,omitempty"`
	TimelineCurrentStep       *int              `json:"timelineCurrentStep,omitempty"`
	EarliestReversibleMinimum int               `json:"earliestReversibleMinimum,omitempty"`
	TimelineCheckpointMaximum *int              `json:"timelineCheckpointMaximum,omitempty"`
	TimelineEventMaximum      *int              `json:"timelineEventMaximum,omitempty"`
	FutureDiscarded           *bool             `json:"futureDiscarded,omitempty"`
	StateHashMatchesRecorded  bool              `json:"stateHashMatchesRecorded,omitempty"`
	HeapObjectIDs             []string          `json:"heapObjectIds,omitempty"`
	HeapRootLabels            []string          `json:"heapRootLabels,omitempty"`
	HeapReferences            map[string]string `json:"heapReferences,omitempty"`
	HeapFieldOwners           map[string]string `json:"heapFieldOwners,omitempty"`
	HeapFieldValues           map[string]string `json:"heapFieldValues,omitempty"`
	HeapTruncated             *bool             `json:"heapTruncated,omitempty"`
	HeapTotalObjects          *int              `json:"heapTotalObjects,omitempty"`
	RequiredMethods           []string          `json:"requiredMethods,omitempty"`
	RequiredLines             []int             `json:"requiredLines,omitempty"`
	MinimumFrameDepth         int               `json:"minimumFrameDepth,omitempty"`
	NoSyntheticStops          bool              `json:"noSyntheticStops,omitempty"`
}

type ExpectedDiagnostic struct {
	Code     string                `json:"code"`
	Stage    string                `json:"stage"`
	Severity string                `json:"severity"`
	Source   string                `json:"source,omitempty"`
	Span     *ExpectedSpan         `json:"span,omitempty"`
	Related  []ExpectedRelatedSpan `json:"related,omitempty"`
}

type ExpectedRelatedSpan struct {
	Label  string        `json:"label"`
	Source string        `json:"source,omitempty"`
	Span   *ExpectedSpan `json:"span,omitempty"`
}

type ExpectedASTNode struct {
	Path  []int         `json:"path"`
	Kind  string        `json:"kind"`
	Text  string        `json:"text,omitempty"`
	Name  string        `json:"name,omitempty"`
	Type  string        `json:"type,omitempty"`
	Value string        `json:"value,omitempty"`
	Span  *ExpectedSpan `json:"span,omitempty"`
}

type ExpectedToken struct {
	Type    string        `json:"type"`
	Lexeme  string        `json:"lexeme"`
	Literal string        `json:"literal"`
	Span    *ExpectedSpan `json:"span,omitempty"`
}

type ExpectedSpan struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn"`
	EndLine     int `json:"endLine"`
	EndColumn   int `json:"endColumn"`
	StartOffset int `json:"startOffset"`
	EndOffset   int `json:"endOffset"`
}

type Report struct {
	SchemaVersion       string          `json:"schemaVersion"`
	MatrixSchemaVersion string          `json:"matrixSchemaVersion"`
	Summary             ReportSummary   `json:"summary"`
	Rules               []RuleResult    `json:"rules"`
	Fixtures            []FixtureResult `json:"fixtures"`
}

type ReportSummary struct {
	Rules     int `json:"rules"`
	Fixtures  int `json:"fixtures"`
	Passed    int `json:"passed"`
	Failed    int `json:"failed"`
	Partial   int `json:"partial"`
	Divergent int `json:"divergent"`
	Untested  int `json:"untested"`
}

type RuleResult struct {
	ID             string   `json:"id"`
	DeclaredStatus string   `json:"declaredStatus"`
	Outcome        string   `json:"outcome"`
	FixtureIDs     []string `json:"fixtureIds"`
	Failures       []string `json:"failures,omitempty"`
}

type FixtureResult struct {
	ID                  string                  `json:"id"`
	Passed              bool                    `json:"passed"`
	LastSuccessfulStage string                  `json:"lastSuccessfulStage"`
	Diagnostics         []string                `json:"diagnostics"`
	DiagnosticDetails   []ExpectedDiagnostic    `json:"diagnosticDetails,omitempty"`
	Tokens              []ExpectedToken         `json:"tokens,omitempty"`
	AST                 []ExpectedASTNode       `json:"ast,omitempty"`
	Output              string                  `json:"output"`
	RuntimeStatus       string                  `json:"runtimeStatus,omitempty"`
	SourceUnits         []string                `json:"sourceUnits,omitempty"`
	Failure             string                  `json:"failure,omitempty"`
	Replay              *DebuggerReplayEvidence `json:"replay,omitempty"`
}

// DebuggerReplayEvidence is emitted only for a failing debugger fixture. The
// action prefix is sufficient to replay the failure against the fixture's
// checked-in source units, while Actual contains only stable public fields.
type DebuggerReplayEvidence struct {
	ActionIndex int                    `json:"actionIndex"`
	Actions     []DebuggerAction       `json:"actions"`
	Actual      DebuggerActualEvidence `json:"actual"`
}

type DebuggerActualEvidence struct {
	PipelinePhase  string                         `json:"pipelinePhase,omitempty"`
	Complete       bool                           `json:"complete"`
	Blocked        bool                           `json:"blocked"`
	Error          string                         `json:"error,omitempty"`
	StopReason     string                         `json:"stopReason,omitempty"`
	Status         string                         `json:"status,omitempty"`
	ExecutedSteps  int                            `json:"executedSteps"`
	Halted         bool                           `json:"halted"`
	Paused         bool                           `json:"paused"`
	WaitingInput   bool                           `json:"waitingInput"`
	WaitingRequest *compiler.VMInputRequest       `json:"waitingRequest,omitempty"`
	InputEvents    int                            `json:"inputEvents"`
	Location       *compiler.DebugLocation        `json:"location,omitempty"`
	Output         []string                       `json:"output,omitempty"`
	Diagnostics    []string                       `json:"diagnostics,omitempty"`
	DebugLog       []string                       `json:"debugLog,omitempty"`
	Timeline       *compiler.ReverseTimelineState `json:"timeline,omitempty"`
	HeapObjectIDs  []string                       `json:"heapObjectIds,omitempty"`
	HeapReferences map[string]string              `json:"heapReferences,omitempty"`
}
