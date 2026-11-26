// SPDX-License-Identifier: AGPL-3.0-or-later

// Package compiler is the stable, supported Go facade for the Cooler compiler.
//
// The package deliberately exposes value projections rather than the mutable
// parser, class-table, executable, or VM objects used by the implementation.
// Result values are immutable and safe for concurrent readers. Compilation and
// DebugSession serialize their state-changing operations.
package compiler

import (
	"encoding/json"

	internal "github.com/ThalesMMS/cooler-compiler/internal/compiler"
)

const (
	// APIVersion follows semantic versioning independently from the compiler.
	APIVersion = "1.1.0"
	// ReportSchemaVersion identifies the JSON artifact contract.
	ReportSchemaVersion = internal.ReportSchemaVersion

	// OptimizationNone preserves the unoptimized generated IR.
	OptimizationNone = internal.OptimizationNone
	// OptimizationBasic enables the deterministic basic optimization pipeline.
	OptimizationBasic = internal.OptimizationBasic
)

// Mode selects whether compilation stops after semantic checking or executes.
type Mode string

const (
	// ModeRun compiles and executes Main.main.
	ModeRun Mode = "run"
	// ModeCheck stops after semantic analysis and never starts the VM.
	ModeCheck Mode = "check"
)

// SourceUnit is one ordered organizational input. Units do not introduce COOL
// namespaces; ID is the stable source identity used by every linked artifact.
type SourceUnit struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

// Project is an ordered collection of COOL source units compiled together.
type Project struct {
	Sources []SourceUnit
}

// Limits contains deterministic runtime ceilings. Zero values select the safe
// desktop defaults. Invalid or excessive values are rejected as Go errors.
type Limits struct {
	MaxInstructionsPerRun int `json:"maxInstructionsPerRun"`
	MaxInstructionsTotal  int `json:"maxInstructionsTotal"`
	MaxOperandStackDepth  int `json:"maxOperandStackDepth"`
	MaxCallFrameDepth     int `json:"maxCallFrameDepth"`
	MaxScopeDepth         int `json:"maxScopeDepth"`
	MaxObjectAllocations  int `json:"maxObjectAllocations"`
	MaxTraceEntries       int `json:"maxTraceEntries"`
	MaxTraceBytes         int `json:"maxTraceBytes"`
	MaxStringBytes        int `json:"maxStringBytes"`
	MaxOutputBytes        int `json:"maxOutputBytes"`
	MaxInputBytes         int `json:"maxInputBytes"`
	MaxInputLines         int `json:"maxInputLines"`
	MaxBreakpoints        int `json:"maxBreakpoints"`
}

// Options configures compilation, execution limits, optimization, and report
// provenance. Use NormalizeOptions to validate and fill zero-value defaults.
type Options struct {
	WarningRules string
	Mode         Mode
	Input        string
	MaxSteps     int
	Limits       Limits
	Optimization string
	// Frontend selects "handwritten" (default, also "") or "generated", the
	// spec-driven lexer and goyacc LALR parser.
	Frontend        string
	CompilerVersion string
	CompilerCommit  string
}

// Span identifies a UTF-16 source range. Lines and columns are one-based;
// offsets are zero-based UTF-16 code-unit offsets.
type Span struct {
	SourceID    string `json:"sourceId,omitempty"`
	StartLine   int    `json:"startLine"`
	StartColumn int    `json:"startColumn"`
	EndLine     int    `json:"endLine"`
	EndColumn   int    `json:"endColumn"`
	StartOffset int    `json:"startOffset"`
	EndOffset   int    `json:"endOffset"`
}

// DiagnosticSeverity classifies a compiler diagnostic's impact.
type DiagnosticSeverity string

const (
	// SeverityError identifies a diagnostic that blocks a later phase.
	SeverityError DiagnosticSeverity = "error"
	// SeverityWarning identifies a non-blocking correctness or quality warning.
	SeverityWarning DiagnosticSeverity = "warning"
	// SeverityInfo identifies non-blocking informational guidance.
	SeverityInfo DiagnosticSeverity = "info"
)

// RelatedSpan links a diagnostic to another relevant source location.
type RelatedSpan struct {
	Label  string `json:"label"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Span   Span   `json:"span"`
}

// QuickFix describes a bounded source replacement for a diagnostic.
type QuickFix struct {
	Title       string `json:"title"`
	Replacement string `json:"replacement"`
	Span        Span   `json:"span"`
}

// DiagnosticSuggestion describes a human-readable alternative without an
// automatic text edit.
type DiagnosticSuggestion struct {
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}

// Diagnostic is a stable, source-mapped compiler or runtime finding.
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

// TokenType is the stable lexical category name emitted in reports.
type TokenType string

// Token is a detached lexical token with its original source range.
type Token struct {
	Type    TokenType `json:"type"`
	Lexeme  string    `json:"lexeme"`
	Literal string    `json:"literal,omitempty"`
	Line    int       `json:"line"`
	Column  int       `json:"column"`
	Offset  int       `json:"offset"`
	Span    Span      `json:"span"`
}

// ASTNode is a detached, typed view of the compiler AST. It never references
// mutable parser nodes or semantic tables.
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

// Symbol is a detached semantic symbol-table entry.
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

// IRInstruction is one source-mapped instruction in the stack IR.
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

// ControlFlowBlock is a contiguous instruction range in one method.
type ControlFlowBlock struct {
	Name   string `json:"name"`
	Method string `json:"method,omitempty"`
	Start  int    `json:"start"`
	End    int    `json:"end"`
	Range  string `json:"range"`
	Span   Span   `json:"span"`
}

// ControlFlowEdge connects two basic blocks and records the transfer kind.
type ControlFlowEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
	Span Span   `json:"span"`
}

// ControlFlowGraph is the detached basic-block graph for compiled methods.
type ControlFlowGraph struct {
	Blocks []ControlFlowBlock `json:"blocks"`
	Edges  []ControlFlowEdge  `json:"edges"`
}

// Metrics contains deterministic counts produced by compiler phases.
type Metrics struct {
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

// RuntimeValue is a detached display projection of a VM value.
type RuntimeValue struct {
	Type    string `json:"type"`
	Display string `json:"display"`
	Object  string `json:"object,omitempty"`
}

// RuntimeFrame is a detached call frame captured at a VM stop.
type RuntimeFrame struct {
	FunctionName string                  `json:"functionName"`
	Owner        string                  `json:"owner,omitempty"`
	IP           int                     `json:"ip"`
	Self         RuntimeValue            `json:"self"`
	Locals       map[string]RuntimeValue `json:"locals"`
	StackBase    int                     `json:"stackBase"`
	Span         Span                    `json:"span"`
}

// VMState is one bounded execution-trace entry.
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

// InputRequest describes the live IO.in_* operation awaiting a line.
type InputRequest struct {
	Sequence             int      `json:"sequence"`
	Builtin              string   `json:"builtin"`
	Span                 Span     `json:"span"`
	Method               string   `json:"method"`
	Frame                int      `json:"frame"`
	Output               []string `json:"output"`
	InputPosition        int      `json:"inputPosition"`
	PreSuppliedRemaining bool     `json:"preSuppliedRemaining"`
}

// RuntimeResult contains the bounded VM state, output, trace, and diagnostics.
type RuntimeResult struct {
	Trace         []VMState      `json:"trace"`
	ExecutedSteps int            `json:"executedSteps"`
	TraceOffset   int            `json:"traceOffset,omitempty"`
	Output        []string       `json:"output"`
	Stack         []RuntimeValue `json:"stack"`
	Frames        []RuntimeFrame `json:"frames"`
	Halted        bool           `json:"halted"`
	Paused        bool           `json:"paused,omitempty"`
	WaitingInput  *InputRequest  `json:"waitingInput,omitempty"`
	InputEvents   int            `json:"inputEvents,omitempty"`
	Diagnostics   []Diagnostic   `json:"diagnostics,omitempty"`
}

// Stage summarizes one compiler pipeline phase.
type Stage struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

// ReportTarget records the platform and Go toolchain that produced a report.
type ReportTarget struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	GoVersion string `json:"goVersion"`
}

// ReportOptions records normalized execution and optimization settings.
type ReportOptions struct {
	WarningRules  string `json:"warningRules,omitempty"`
	Execute       bool   `json:"execute"`
	VMMaxSteps    int    `json:"vmMaxSteps"`
	InputProvided bool   `json:"inputProvided"`
	VMPolicy      Limits `json:"vmPolicy"`
	Optimization  string `json:"optimization"`
}

// SourceUnitMetadata identifies an input without embedding its content.
type SourceUnitMetadata struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Order    int    `json:"order"`
	ByteSize int    `json:"byteSize"`
	SHA256   string `json:"sha256"`
}

// Report is a mutable caller-owned copy of an immutable Result. Optimization
// and visualization payloads remain experimental extensions of the stable
// report and are intentionally represented as raw schema-versioned JSON.
type Report struct {
	SchemaVersion   string               `json:"schemaVersion"`
	CompilerVersion string               `json:"compilerVersion"`
	CompilerCommit  string               `json:"compilerCommit,omitempty"`
	Target          ReportTarget         `json:"target"`
	Options         ReportOptions        `json:"options"`
	SourceUnits     []SourceUnitMetadata `json:"sourceUnits"`
	Source          string               `json:"source"`
	Stages          []Stage              `json:"stages"`
	Tokens          []Token              `json:"tokens"`
	AST             *ASTNode             `json:"ast,omitempty"`
	Symbols         []Symbol             `json:"symbols"`
	IR              []IRInstruction      `json:"ir"`
	Assembly        []string             `json:"assembly"`
	AssemblySpans   []Span               `json:"assemblySpans"`
	Metrics         Metrics              `json:"metrics"`
	ControlFlow     ControlFlowGraph     `json:"controlFlow"`
	Runtime         RuntimeResult        `json:"runtime"`
	Diagnostics     []Diagnostic         `json:"diagnostics"`
	Visualizations  json.RawMessage      `json:"visualizations,omitempty"`
	Optimization    json.RawMessage      `json:"optimization"`
}

// CompilationPhase identifies a resumable compiler pipeline boundary.
type CompilationPhase string

const (
	// PhaseInitial is the state before source validation or compilation.
	PhaseInitial CompilationPhase = ""
	// PhaseLexer tokenizes all source units.
	PhaseLexer CompilationPhase = "lexer"
	// PhaseParser builds and merges source-unit syntax trees.
	PhaseParser CompilationPhase = "parser"
	// PhaseSemantic resolves types, inheritance, and symbols.
	PhaseSemantic CompilationPhase = "semantic"
	// PhaseIR generates and optionally optimizes stack IR.
	PhaseIR CompilationPhase = "ir"
	// PhaseCodegen verifies and materializes the executable artifacts.
	PhaseCodegen CompilationPhase = "codegen"
	// PhaseRuntime makes a stopped VM available to the debugger.
	PhaseRuntime CompilationPhase = "runtime"
)

// PipelineSnapshot is an immutable view of incremental compilation progress.
type PipelineSnapshot struct {
	CurrentPhase CompilationPhase `json:"currentPhase"`
	NextPhase    CompilationPhase `json:"nextPhase"`
	Complete     bool             `json:"complete"`
	Blocked      bool             `json:"blocked"`
	Result       Result           `json:"result"`
}
