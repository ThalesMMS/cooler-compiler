// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"fmt"
	"strings"
	"unicode/utf16"
)

// Span identifies a source range. Lines and columns are 1-based, the end is
// exclusive, and offsets are UTF-16 code units so browser clients can select
// text without translating JavaScript string indexes.
type Span struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn"`
	EndLine     int `json:"endLine"`
	EndColumn   int `json:"endColumn"`
	StartOffset int `json:"startOffset"`
	EndOffset   int `json:"endOffset"`
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
	return Span{
		StartLine:   a.StartLine,
		StartColumn: a.StartColumn,
		EndLine:     b.EndLine,
		EndColumn:   b.EndColumn,
		StartOffset: a.StartOffset,
		EndOffset:   b.EndOffset,
	}
}

func spanForText(line, column, offset int, text string) Span {
	endLine, endColumn, endOffset := line, column, offset
	for _, r := range text {
		endOffset += utf16RuneLen(r)
		if r == '\n' {
			endLine++
			endColumn = 1
		} else {
			endColumn++
		}
	}
	return Span{
		StartLine: line, StartColumn: column,
		EndLine: endLine, EndColumn: endColumn,
		StartOffset: offset, EndOffset: endOffset,
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
	Index   int      `json:"index"`
	Method  string   `json:"method,omitempty"`
	Op      string   `json:"op"`
	Args    []string `json:"args,omitempty"`
	Result  string   `json:"result,omitempty"`
	Comment string   `json:"comment,omitempty"`
	Line    int      `json:"line,omitempty"`
	Column  int      `json:"column,omitempty"`
	Span    Span     `json:"span"`
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
	Trace         []VMState      `json:"trace"`
	ExecutedSteps int            `json:"executedSteps"`
	TraceOffset   int            `json:"traceOffset,omitempty"`
	Output        []string       `json:"output"`
	Stack         []RuntimeValue `json:"stack"`
	Frames        []RuntimeFrame `json:"frames"`
	Halted        bool           `json:"halted"`
	Paused        bool           `json:"paused,omitempty"`
	Diagnostics   []Diagnostic   `json:"diagnostics,omitempty"`
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
}

type ControlFlowGraph struct {
	Blocks []ControlFlowBlock `json:"blocks"`
	Edges  []ControlFlowEdge  `json:"edges"`
}

type Result struct {
	Source      string           `json:"source"`
	Stages      []Stage          `json:"stages"`
	Tokens      []Token          `json:"tokens"`
	AST         *ASTNode         `json:"ast,omitempty"`
	Symbols     []Symbol         `json:"symbols"`
	IR          []IRInstruction  `json:"ir"`
	Assembly    []string         `json:"assembly"`
	Metrics     ResultMetrics    `json:"metrics"`
	ControlFlow ControlFlowGraph `json:"controlFlow"`
	Runtime     VMResult         `json:"runtime"`
	Diagnostics []Diagnostic     `json:"diagnostics"`
}

type DebugSnapshot struct {
	Result  Result   `json:"result"`
	Runtime VMResult `json:"runtime"`
	Status  string   `json:"status"`
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

func stageStatus(diags []Diagnostic, stage string) string {
	for _, d := range diags {
		if d.Stage == stage && d.Severity == SeverityError {
			return "error"
		}
	}
	return "ok"
}

func summarizeCount(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}
