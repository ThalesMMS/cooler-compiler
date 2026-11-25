// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const ReportSchemaVersion = "1.5.0"

// Span identifies a source range. Lines and columns are 1-based, the end is
// exclusive, and offsets are UTF-16 code units so browser clients can select
// text without translating JavaScript string indexes.
type Span struct {
	SourceID    string `json:"sourceId,omitempty"`
	StartLine   int    `json:"startLine"`
	StartColumn int    `json:"startColumn"`
	EndLine     int    `json:"endLine"`
	EndColumn   int    `json:"endColumn"`
	StartOffset int    `json:"startOffset"`
	EndOffset   int    `json:"endOffset"`
}

func emptySpan() Span {
	return Span{StartLine: 1, StartColumn: 1, EndLine: 1, EndColumn: 1}
}

func joinSpans(a, b Span) Span {
	if a.StartLine == 0 {
		return b
	}
	if b.StartLine == 0 {
		return a
	}
	if a.SourceID != "" && b.SourceID != "" && a.SourceID != b.SourceID {
		return a
	}
	return Span{
		SourceID:    a.SourceID,
		StartLine:   a.StartLine,
		StartColumn: a.StartColumn,
		EndLine:     b.EndLine,
		EndColumn:   b.EndColumn,
		StartOffset: a.StartOffset,
		EndOffset:   b.EndOffset,
	}
}

func utf16RuneLen(r rune) int {
	if r <= 0xFFFF {
		return 1
	}
	return len(utf16.Encode([]rune{r}))
}

type DiagnosticSeverity string

const (
	SeverityError   DiagnosticSeverity = "error"
	SeverityWarning DiagnosticSeverity = "warning"
	SeverityInfo    DiagnosticSeverity = "info"
)

type RelatedSpan struct {
	Label  string `json:"label"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Span   Span   `json:"span"`
}

type QuickFix struct {
	Title       string `json:"title"`
	Replacement string `json:"replacement"`
	Span        Span   `json:"span"`
}

type DiagnosticSuggestion struct {
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}

type Diagnostic struct {
	Code        string                 `json:"code,omitempty"`
	Stage       string                 `json:"stage"`
	Severity    DiagnosticSeverity     `json:"severity"`
	Message     string                 `json:"message"`
	Hint        string                 `json:"hint,omitempty"`
	Line        int                    `json:"line"`
	Column      int                    `json:"column"`
	Span        Span                   `json:"span"`
	Related     []RelatedSpan          `json:"related,omitempty"`
	QuickFix    []QuickFix             `json:"quickFixes,omitempty"`
	Suggestions []DiagnosticSuggestion `json:"suggestions,omitempty"`
}

func diagnostic(stage string, severity DiagnosticSeverity, code, message, hint string, span Span) Diagnostic {
	if span.StartLine == 0 {
		span = emptySpan()
	}
	d := Diagnostic{
		Code: code, Stage: stage, Severity: severity, Message: message, Hint: hint,
		Line: span.StartLine, Column: span.StartColumn, Span: span,
	}
	if strings.TrimSpace(hint) != "" {
		d.Suggestions = []DiagnosticSuggestion{{Title: hint}}
	}
	return d
}

func errorDiagnostic(stage, code, message, hint string, span Span) Diagnostic {
	return diagnostic(stage, SeverityError, code, message, hint, span)
}

func warningDiagnostic(stage, code, message, hint string, span Span) Diagnostic {
	return diagnostic(stage, SeverityWarning, code, message, hint, span)
}

func withRelated(d Diagnostic, label string, span Span) Diagnostic {
	if span.StartLine == 0 {
		span = emptySpan()
	}
	d.Related = append(d.Related, RelatedSpan{Label: label, Line: span.StartLine, Column: span.StartColumn, Span: span})
	return d
}

func withQuickFix(d Diagnostic, title, replacement string, span Span) Diagnostic {
	d.QuickFix = append(d.QuickFix, QuickFix{Title: title, Replacement: replacement, Span: span})
	return d
}

type ASTNode struct {
	Kind     string    `json:"kind"`
	Text     string    `json:"text,omitempty"`
	Name     string    `json:"name,omitempty"`
	Type     string    `json:"type,omitempty"`
	Value    string    `json:"value,omitempty"`
	Children []ASTNode `json:"children,omitempty"`
	Line     int       `json:"line,omitempty"`
	Column   int       `json:"column,omitempty"`
	Span     Span      `json:"span"`
}

type Symbol struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Type        string `json:"type"`
	Mutable     bool   `json:"mutable"`
	Initialized bool   `json:"initialized"`
	Scope       string `json:"scope"`
	Owner       string `json:"owner,omitempty"`
	Line        int    `json:"line"`
	Column      int    `json:"column"`
	Span        Span   `json:"span"`
}

type IRInstruction struct {
	Index           int      `json:"index"`
	Method          string   `json:"method,omitempty"`
	Op              string   `json:"op"`
	Args            []string `json:"args,omitempty"`
	Result          string   `json:"result,omitempty"`
	Comment         string   `json:"comment,omitempty"`
	Line            int      `json:"line,omitempty"`
	Column          int      `json:"column,omitempty"`
	Span            Span     `json:"span"`
	Synthetic       bool     `json:"synthetic,omitempty"`
	OriginalIndexes []int    `json:"originalIndexes"`
}

type OptimizationPassContract struct {
	Name                  string   `json:"name"`
	Preconditions         []string `json:"preconditions"`
	Preserves             []string `json:"preserves"`
	Analysis              string   `json:"analysis"`
	CanAlterInstructions  bool     `json:"canAlterInstructions"`
	CanAlterCFG           bool     `json:"canAlterCFG"`
	CanAlterAllocation    bool     `json:"canAlterAllocation"`
	CanAlterSourceMapping bool     `json:"canAlterSourceMapping"`
}

type OptimizationChange struct {
	Method          string `json:"method"`
	Kind            string `json:"kind"`
	Before          string `json:"before"`
	After           string `json:"after"`
	Explanation     string `json:"explanation"`
	OriginalIndexes []int  `json:"originalIndexes"`
	Span            Span   `json:"span"`
}

type OptimizationRejection struct {
	Method          string `json:"method"`
	Opportunity     string `json:"opportunity"`
	Reason          string `json:"reason"`
	OriginalIndexes []int  `json:"originalIndexes"`
	Span            Span   `json:"span"`
}

type OptimizationPassReport struct {
	Contract           OptimizationPassContract `json:"contract"`
	BeforeInstructions int                      `json:"beforeInstructions"`
	AfterInstructions  int                      `json:"afterInstructions"`
	Transformations    int                      `json:"transformations"`
	Rejected           int                      `json:"rejected"`
	Verified           bool                     `json:"verified"`
	Changes            []OptimizationChange     `json:"changes"`
	Rejections         []OptimizationRejection  `json:"rejections"`
}

type OptimizationReport struct {
	Mode              string                   `json:"mode"`
	Enabled           bool                     `json:"enabled"`
	ExecutionIR       string                   `json:"executionIR"`
	PassOrder         []string                 `json:"passOrder"`
	BaselineVerified  bool                     `json:"baselineVerified"`
	OptimizedVerified bool                     `json:"optimizedVerified"`
	BeforeIR          []IRInstruction          `json:"beforeIR"`
	AfterIR           []IRInstruction          `json:"afterIR"`
	BeforeCFG         ControlFlowGraph         `json:"beforeCFG"`
	AfterCFG          ControlFlowGraph         `json:"afterCFG"`
	Passes            []OptimizationPassReport `json:"passes"`
}

// VisualizationLimits makes the educational relationship model safe to
// render even when a generated project contains many thousands of artifacts.
type VisualizationLimits struct {
	MaxClasses   int `json:"maxClasses"`
	MaxArtifacts int `json:"maxArtifacts"`
	MaxGroups    int `json:"maxGroups"`
	MaxRelations int `json:"maxRelations"`
}

type InheritanceNode struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Parent          string   `json:"parent,omitempty"`
	RecoveredParent string   `json:"recoveredParent,omitempty"`
	Kind            string   `json:"kind"`
	Status          string   `json:"status"`
	Span            Span     `json:"span"`
	Diagnostics     []string `json:"diagnostics,omitempty"`
}

type InheritanceEdge struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Status    string `json:"status"`
	Recovered bool   `json:"recovered,omitempty"`
	Cycle     bool   `json:"cycle,omitempty"`
	Span      Span   `json:"span"`
}

type LogicalAttribute struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Owner     string `json:"owner"`
	Inherited bool   `json:"inherited"`
	Slot      int    `json:"slot"`
	Builtin   bool   `json:"builtin,omitempty"`
	Span      Span   `json:"span"`
}

type DispatchSlot struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Signature       string   `json:"signature"`
	Owner           string   `json:"owner"`
	Inherited       bool     `json:"inherited"`
	Overrides       string   `json:"overrides,omitempty"`
	Slot            int      `json:"slot"`
	ReturnsSelfType bool     `json:"returnsSelfType,omitempty"`
	LookupStarts    []string `json:"lookupStarts"`
	Builtin         bool     `json:"builtin,omitempty"`
	Span            Span     `json:"span"`
}

type LogicalClassModel struct {
	Class       string             `json:"class"`
	Parent      string             `json:"parent,omitempty"`
	Status      string             `json:"status"`
	Attributes  []LogicalAttribute `json:"attributes"`
	Dispatch    []DispatchSlot     `json:"dispatch"`
	LookupOrder []string           `json:"lookupOrder"`
	Span        Span               `json:"span"`
}

type ProvenanceArtifact struct {
	ID        string `json:"id"`
	Phase     string `json:"phase"`
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	Span      Span   `json:"span"`
	Synthetic bool   `json:"synthetic,omitempty"`
}

type ProvenanceGroup struct {
	ID          string   `json:"id"`
	SourceID    string   `json:"sourceId,omitempty"`
	Span        Span     `json:"span"`
	ASTKind     string   `json:"astKind"`
	StaticType  string   `json:"staticType,omitempty"`
	ArtifactIDs []string `json:"artifactIds"`
}

// VisualizationBundle is a compiler-produced, deterministic educational
// model. Its layouts are logical COOL layouts, never a native/reference ABI.
type VisualizationBundle struct {
	SchemaVersion   string               `json:"schemaVersion"`
	CompilerVersion string               `json:"compilerVersion"`
	SourceUnitIDs   []string             `json:"sourceUnitIds"`
	Model           string               `json:"model"`
	Limits          VisualizationLimits  `json:"limits"`
	Truncated       bool                 `json:"truncated"`
	Inheritance     []InheritanceNode    `json:"inheritance"`
	Edges           []InheritanceEdge    `json:"edges"`
	Classes         []LogicalClassModel  `json:"classes"`
	Artifacts       []ProvenanceArtifact `json:"artifacts"`
	Groups          []ProvenanceGroup    `json:"groups"`
}

type Stage struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

type RuntimeValue struct {
	Type    string `json:"type"`
	Display string `json:"display"`
	Object  string `json:"object,omitempty"`
}

type RuntimeFrame struct {
	FunctionName string                  `json:"functionName"`
	Owner        string                  `json:"owner,omitempty"`
	IP           int                     `json:"ip"`
	Self         RuntimeValue            `json:"self"`
	Locals       map[string]RuntimeValue `json:"locals"`
	StackBase    int                     `json:"stackBase"`
	Span         Span                    `json:"span"`
}

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

type BreakpointState struct {
	Breakpoint Breakpoint `json:"breakpoint"`
	Verified   bool       `json:"verified"`
	Diagnostic string     `json:"diagnostic,omitempty"`
	Hits       int        `json:"hits"`
}

type WatchExpression struct {
	ID         string `json:"id"`
	Expression string `json:"expression"`
	Enabled    *bool  `json:"enabled,omitempty"`
}

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

type DebugLogEntry struct {
	Order    int    `json:"order"`
	Message  string `json:"message"`
	SourceID string `json:"sourceId,omitempty"`
	Line     int    `json:"line"`
	Method   string `json:"method,omitempty"`
	Step     int    `json:"step"`
}

type HeapLimits struct {
	MaxObjects    int `json:"maxObjects"`
	MaxFields     int `json:"maxFieldsPerObject"`
	MaxDepth      int `json:"maxDepth"`
	StringPreview int `json:"stringPreview"`
	MaxBytes      int `json:"maxBytes"`
}

type HeapQuery struct {
	Mode   string     `json:"mode"`
	ID     string     `json:"id,omitempty"`
	Class  string     `json:"class,omitempty"`
	Offset int        `json:"offset,omitempty"`
	Limits HeapLimits `json:"limits,omitempty"`
}

type HeapValue struct {
	Kind        string `json:"kind"`
	Type        string `json:"type,omitempty"`
	Display     string `json:"display"`
	ObjectID    string `json:"objectId,omitempty"`
	FullLength  int    `json:"fullLength,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
	Unavailable bool   `json:"unavailable,omitempty"`
}

type HeapField struct {
	Name       string    `json:"name"`
	Owner      string    `json:"owner"`
	StaticType string    `json:"staticType"`
	Span       Span      `json:"span"`
	Value      HeapValue `json:"value"`
}

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

type HeapRoot struct {
	Kind       string    `json:"kind"`
	Label      string    `json:"label"`
	Frame      *int      `json:"frame,omitempty"`
	Scope      *int      `json:"scope,omitempty"`
	StackIndex *int      `json:"stackIndex,omitempty"`
	Value      HeapValue `json:"value"`
}

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

type ReverseMode string

const (
	ReverseModeInstruction ReverseMode = "instruction"
	ReverseModeSource      ReverseMode = "source"
)

type ReverseHistoryLimits struct {
	CheckpointInterval int `json:"checkpointInterval"`
	MaxCheckpoints     int `json:"maxCheckpoints"`
	MaxCheckpointBytes int `json:"maxCheckpointBytes"`
	MaxEvents          int `json:"maxEvents"`
	MaxEventBytes      int `json:"maxEventBytes"`
}

type ReverseTimelineState struct {
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

type DebugStopReason string

const (
	StopReasonReady            DebugStopReason = "ready"
	StopReasonInstruction      DebugStopReason = "instruction-step"
	StopReasonStepInto         DebugStopReason = "step-into"
	StopReasonStepOver         DebugStopReason = "step-over"
	StopReasonStepOut          DebugStopReason = "step-out"
	StopReasonRunToCursor      DebugStopReason = "run-to-cursor"
	StopReasonBreakpoint       DebugStopReason = "breakpoint"
	StopReasonWaitingInput     DebugStopReason = "waiting-input"
	StopReasonInstructionLimit DebugStopReason = "instruction-limit"
	StopReasonRuntimeError     DebugStopReason = "runtime-error"
	StopReasonHalt             DebugStopReason = "halt"
	StopReasonCancelled        DebugStopReason = "cancelled"
	StopReasonReverseStep      DebugStopReason = "reverse-step"
	StopReasonReverseContinue  DebugStopReason = "reverse-continue"
)

// SourcePosition identifies a requested user source position. Column zero
// means any executable construct on the line.
type SourcePosition struct {
	SourceID string `json:"sourceId"`
	Line     int    `json:"line"`
	Column   int    `json:"column,omitempty"`
}

// DebugLocation identifies the next executable instruction and its source
// mapping. Synthetic locations are inspectable but skipped by source stepping.
type DebugLocation struct {
	SourceID    string `json:"sourceId,omitempty"`
	Span        Span   `json:"span"`
	Method      string `json:"method"`
	Instruction int    `json:"instruction"`
	FrameDepth  int    `json:"frameDepth"`
	Synthetic   bool   `json:"synthetic"`
}

func (breakpoint *Breakpoint) UnmarshalJSON(data []byte) error {
	var line int
	if err := json.Unmarshal(data, &line); err == nil {
		breakpoint.Line = line
		return nil
	}
	type breakpointAlias Breakpoint
	var decoded breakpointAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*breakpoint = Breakpoint(decoded)
	return nil
}

type VMState struct {
	Step   int            `json:"step"`
	IP     int            `json:"ip"`
	Method string         `json:"method,omitempty"`
	Op     string         `json:"op"`
	Stack  []RuntimeValue `json:"stack"`
	Frames []RuntimeFrame `json:"frames"`
	Output []string       `json:"output"`
	Note   string         `json:"note"`
	Span   Span           `json:"span"`
}

type VMResult struct {
	Trace         []VMState       `json:"trace"`
	ExecutedSteps int             `json:"executedSteps"`
	TraceOffset   int             `json:"traceOffset,omitempty"`
	Output        []string        `json:"output"`
	Stack         []RuntimeValue  `json:"stack"`
	Frames        []RuntimeFrame  `json:"frames"`
	Halted        bool            `json:"halted"`
	Paused        bool            `json:"paused,omitempty"`
	WaitingInput  *VMInputRequest `json:"waitingInput,omitempty"`
	InputEvents   int             `json:"inputEvents,omitempty"`
	Diagnostics   []Diagnostic    `json:"diagnostics,omitempty"`
}

// VMInputRequest describes an input opcode that has not executed yet. Output is
// copied at the wait boundary so clients can render the preceding prompt.
type VMInputRequest struct {
	Sequence             int      `json:"sequence"`
	Builtin              string   `json:"builtin"`
	Span                 Span     `json:"span"`
	Method               string   `json:"method"`
	Frame                int      `json:"frame"`
	Output               []string `json:"output"`
	InputPosition        int      `json:"inputPosition"`
	PreSuppliedRemaining bool     `json:"preSuppliedRemaining"`
}

// VMInputEvent is retained by a debug session for deterministic replay. Line
// is omitted unless a caller explicitly requests sensitive content.
type VMInputEvent struct {
	Sequence      int    `json:"sequence"`
	Builtin       string `json:"builtin"`
	Line          string `json:"line,omitempty"`
	Span          Span   `json:"span"`
	InputPosition int    `json:"inputPosition"`
	Live          bool   `json:"live"`
}

func (result VMResult) MarshalJSON() ([]byte, error) {
	type alias VMResult
	if result.Trace == nil {
		result.Trace = []VMState{}
	}
	if result.Output == nil {
		result.Output = []string{}
	}
	if result.Stack == nil {
		result.Stack = []RuntimeValue{}
	}
	if result.Frames == nil {
		result.Frames = []RuntimeFrame{}
	}
	return json.Marshal(alias(result))
}

type ResultMetrics struct {
	UsefulTokens   int `json:"usefulTokens"`
	ASTNodes       int `json:"astNodes"`
	Classes        int `json:"classes"`
	Methods        int `json:"methods"`
	Attributes     int `json:"attributes"`
	Symbols        int `json:"symbols"`
	IRInstructions int `json:"irInstructions"`
	AssemblyLines  int `json:"assemblyLines"`
	BasicBlocks    int `json:"basicBlocks"`
	FlowEdges      int `json:"flowEdges"`
}

type ControlFlowBlock struct {
	Name   string `json:"name"`
	Method string `json:"method,omitempty"`
	Start  int    `json:"start"`
	End    int    `json:"end"`
	Range  string `json:"range"`
	Span   Span   `json:"span"`
}

type ControlFlowEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
	Span Span   `json:"span"`
}

type ControlFlowGraph struct {
	Blocks []ControlFlowBlock `json:"blocks"`
	Edges  []ControlFlowEdge  `json:"edges"`
}

type ReportTarget struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	GoVersion string `json:"goVersion"`
}

type CompilationOptions struct {
	WarningRules  string   `json:"warningRules,omitempty"`
	Execute       bool     `json:"execute"`
	VMMaxSteps    int      `json:"vmMaxSteps"`
	InputProvided bool     `json:"inputProvided"`
	VMPolicy      VMPolicy `json:"vmPolicy"`
	Optimization  string   `json:"optimization"`
}

type SourceUnitMetadata struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Order    int    `json:"order"`
	ByteSize int    `json:"byteSize"`
	SHA256   string `json:"sha256"`
}

// SourceUnit is one ordered organizational input to a COOL project. Units do
// not create namespaces or modules; ID is the stable identity used by every
// source-linked artifact.
type SourceUnit struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

type ProjectOptions struct {
	Input           string
	VMMaxSteps      int
	VMPolicy        VMPolicy
	CompilerVersion string
	CompilerCommit  string
	Optimization    string
}

type ResultContract struct {
	CompilerVersion string
	CompilerCommit  string
	Options         CompilationOptions
	SourceUnits     []SourceUnitMetadata
}

func NewSourceUnitMetadata(name, source string, order int) SourceUnitMetadata {
	return NewSourceUnitMetadataWithID(fmt.Sprintf("source-%d", order+1), name, source, order)
}

func NewSourceUnitMetadataWithID(id, name, source string, order int) SourceUnitMetadata {
	digest := sha256.Sum256([]byte(source))
	return SourceUnitMetadata{
		ID:       id,
		Name:     name,
		Order:    order,
		ByteSize: len([]byte(source)),
		SHA256:   hex.EncodeToString(digest[:]),
	}
}

func normalizeResultContract(source string, contract ResultContract) ResultContract {
	if strings.TrimSpace(contract.CompilerVersion) == "" {
		contract.CompilerVersion = "1.0.0"
	}
	if contract.Options.VMMaxSteps <= 0 {
		contract.Options.VMMaxSteps = 100_000
	}
	policy, err := NormalizeVMPolicy(contract.Options.VMPolicy)
	if err != nil {
		policy = DefaultVMPolicy()
	}
	contract.Options.VMPolicy = policy
	mode, err := NormalizeOptimizationMode(contract.Options.Optimization)
	if err != nil {
		mode = OptimizationNone
	}
	contract.Options.Optimization = mode
	if len(contract.SourceUnits) == 0 {
		contract.SourceUnits = []SourceUnitMetadata{NewSourceUnitMetadata("program.cl", source, 0)}
	} else {
		contract.SourceUnits = append([]SourceUnitMetadata(nil), contract.SourceUnits...)
	}
	return contract
}

type Result struct {
	SchemaVersion   string               `json:"schemaVersion"`
	CompilerVersion string               `json:"compilerVersion"`
	CompilerCommit  string               `json:"compilerCommit,omitempty"`
	Target          ReportTarget         `json:"target"`
	Options         CompilationOptions   `json:"options"`
	SourceUnits     []SourceUnitMetadata `json:"sourceUnits"`
	Source          string               `json:"source"`
	Stages          []Stage              `json:"stages"`
	Tokens          []Token              `json:"tokens"`
	AST             *ASTNode             `json:"ast,omitempty"`
	Symbols         []Symbol             `json:"symbols"`
	IR              []IRInstruction      `json:"ir"`
	Assembly        []string             `json:"assembly"`
	AssemblySpans   []Span               `json:"assemblySpans"`
	Metrics         ResultMetrics        `json:"metrics"`
	ControlFlow     ControlFlowGraph     `json:"controlFlow"`
	Runtime         VMResult             `json:"runtime"`
	Diagnostics     []Diagnostic         `json:"diagnostics"`
	Visualizations  *VisualizationBundle `json:"visualizations,omitempty"`
	Optimization    OptimizationReport   `json:"optimization"`
}

func (result Result) MarshalJSON() ([]byte, error) {
	type alias Result
	if !utf8.ValidString(result.Source) {
		result.Source = string([]rune(result.Source))
	}
	if result.Stages == nil {
		result.Stages = []Stage{}
	}
	if result.Tokens == nil {
		result.Tokens = []Token{}
	}
	if result.Symbols == nil {
		result.Symbols = []Symbol{}
	}
	if result.IR == nil {
		result.IR = []IRInstruction{}
	}
	if result.Assembly == nil {
		result.Assembly = []string{}
	}
	if result.AssemblySpans == nil {
		result.AssemblySpans = []Span{}
	}
	if result.ControlFlow.Blocks == nil {
		result.ControlFlow.Blocks = []ControlFlowBlock{}
	}
	if result.ControlFlow.Edges == nil {
		result.ControlFlow.Edges = []ControlFlowEdge{}
	}
	if result.Diagnostics == nil {
		result.Diagnostics = []Diagnostic{}
	}
	if result.Optimization.PassOrder == nil {
		result.Optimization.PassOrder = []string{}
	}
	if result.Optimization.BeforeIR == nil {
		result.Optimization.BeforeIR = []IRInstruction{}
	}
	if result.Optimization.AfterIR == nil {
		result.Optimization.AfterIR = []IRInstruction{}
	}
	if result.Optimization.BeforeCFG.Blocks == nil {
		result.Optimization.BeforeCFG.Blocks = []ControlFlowBlock{}
	}
	if result.Optimization.BeforeCFG.Edges == nil {
		result.Optimization.BeforeCFG.Edges = []ControlFlowEdge{}
	}
	if result.Optimization.AfterCFG.Blocks == nil {
		result.Optimization.AfterCFG.Blocks = []ControlFlowBlock{}
	}
	if result.Optimization.AfterCFG.Edges == nil {
		result.Optimization.AfterCFG.Edges = []ControlFlowEdge{}
	}
	if result.Optimization.Passes == nil {
		result.Optimization.Passes = []OptimizationPassReport{}
	}
	return json.Marshal(alias(result))
}

func newResult(source string, contract ResultContract) Result {
	contract = normalizeResultContract(source, contract)
	return Result{
		SchemaVersion:   ReportSchemaVersion,
		CompilerVersion: contract.CompilerVersion,
		CompilerCommit:  contract.CompilerCommit,
		Target: ReportTarget{
			OS:        runtime.GOOS,
			Arch:      runtime.GOARCH,
			GoVersion: runtime.Version(),
		},
		Options:     contract.Options,
		SourceUnits: contract.SourceUnits,
		Source:      source,
		Optimization: OptimizationReport{
			Mode: contract.Options.Optimization, Enabled: contract.Options.Optimization == OptimizationBasic,
			ExecutionIR: "unoptimized",
		},
	}
}

type DebugSnapshot struct {
	Result      Result                `json:"result"`
	Runtime     VMResult              `json:"runtime"`
	Status      string                `json:"status"`
	Location    *DebugLocation        `json:"location,omitempty"`
	StopReason  DebugStopReason       `json:"stopReason,omitempty"`
	Breakpoints []BreakpointState     `json:"breakpoints,omitempty"`
	Watches     []WatchResult         `json:"watches,omitempty"`
	DebugLog    []DebugLogEntry       `json:"debugLog,omitempty"`
	Timeline    *ReverseTimelineState `json:"timeline,omitempty"`
}

func hasErrors(diags []Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == SeverityError {
			return true
		}
	}
	return false
}

func countASTNodes(node *ASTNode) int {
	if node == nil {
		return 0
	}
	count := 1
	for i := range node.Children {
		child := node.Children[i]
		count += countASTNodes(&child)
	}
	return count
}

func summarizeCount(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}
