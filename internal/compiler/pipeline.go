// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import "fmt"

// CompilationPhase identifies one user-visible compiler phase. The empty phase
// represents a session that has not performed any work yet.
type CompilationPhase string

const (
	PhaseInitial  CompilationPhase = ""
	PhaseLexer    CompilationPhase = "lexer"
	PhaseParser   CompilationPhase = "parser"
	PhaseSemantic CompilationPhase = "semantic"
	PhaseIR       CompilationPhase = "ir"
	PhaseCodegen  CompilationPhase = "codegen"
	PhaseRuntime  CompilationPhase = "runtime"
)

var compilationPhases = []CompilationPhase{
	PhaseLexer,
	PhaseParser,
	PhaseSemantic,
	PhaseIR,
	PhaseCodegen,
	PhaseRuntime,
}

// PipelineSnapshot is the serializable state exposed to incremental clients.
type PipelineSnapshot struct {
	CurrentPhase CompilationPhase `json:"currentPhase"`
	NextPhase    CompilationPhase `json:"nextPhase"`
	Complete     bool             `json:"complete"`
	Blocked      bool             `json:"blocked"`
	Result       Result           `json:"result"`
}

// CompilationSession retains the private outputs needed by the next compiler
// phase. Transport layers are responsible for serializing access to a session.
type CompilationSession struct {
	source   string
	current  CompilationPhase
	result   Result
	tokens   []Token
	program  *Program
	analysis Analysis
	exec     *executable
	debug    *DebugSession
	complete bool
	blocked  bool
}

// NewCompilationSession creates an idle pipeline without executing the Lexer.
func NewCompilationSession(source string) *CompilationSession {
	session := &CompilationSession{source: source, result: Result{Source: source}}
	session.refreshStages()
	return session
}

// Snapshot returns the accumulated public artifacts without advancing work.
func (s *CompilationSession) Snapshot() PipelineSnapshot {
	return PipelineSnapshot{
		CurrentPhase: s.current,
		NextPhase:    s.nextPhase(),
		Complete:     s.complete,
		Blocked:      s.blocked,
		Result:       cloneResult(s.result),
	}
}

// NextPhase performs exactly one compiler phase unless the session is terminal.
func (s *CompilationSession) NextPhase() PipelineSnapshot {
	if s.complete || s.blocked {
		return s.Snapshot()
	}

	switch s.current {
	case PhaseInitial:
		s.runLexer()
	case PhaseLexer:
		s.runParser()
	case PhaseParser:
		s.runSemantic()
	case PhaseSemantic:
		s.runIR()
	case PhaseIR:
		s.runCodegen()
	case PhaseCodegen:
		s.runRuntime()
	}
	s.refreshStages()
	s.refreshDebugSession()
	return s.Snapshot()
}

// Complete executes every remaining phase until success or a diagnostic barrier.
func (s *CompilationSession) Complete() PipelineSnapshot {
	for !s.complete && !s.blocked {
		before := s.current
		s.NextPhase()
		if s.current == before {
			break
		}
	}
	return s.Snapshot()
}

// DebugSession returns the stopped VM or a terminal blocked debug result.
func (s *CompilationSession) DebugSession() (*DebugSession, bool) {
	if (!s.complete && !s.blocked) || s.debug == nil {
		return nil, false
	}
	return s.debug, true
}

func (s *CompilationSession) runLexer() {
	s.tokens, s.result.Diagnostics = Lex(s.source)
	s.result.Tokens = append([]Token(nil), s.tokens...)
	s.result.Metrics.UsefulTokens = usefulTokenCount(s.tokens)
	s.current = PhaseLexer
}

func (s *CompilationSession) runParser() {
	program, diagnostics := Parse(s.tokens)
	s.program = program
	s.result.Diagnostics = append(s.result.Diagnostics, diagnostics...)
	if program != nil {
		s.result.AST = program.ToAST()
		s.result.Metrics.ASTNodes = countASTNodes(s.result.AST)
	}
	s.current = PhaseParser
	if program == nil || hasStageErrors(s.result.Diagnostics, string(PhaseLexer)) || hasStageErrors(s.result.Diagnostics, string(PhaseParser)) {
		s.blocked = true
	}
}

func (s *CompilationSession) runSemantic() {
	s.analysis = Analyze(s.program)
	s.result.Symbols = append([]Symbol(nil), s.analysis.Symbols...)
	s.result.Diagnostics = append(s.result.Diagnostics, s.analysis.Diagnostics...)
	if s.program != nil {
		s.result.AST = s.program.ToAST()
	}
	s.result.Metrics.ASTNodes = countASTNodes(s.result.AST)
	s.result.Metrics.Classes = s.analysis.Classes
	s.result.Metrics.Methods = s.analysis.Methods
	s.result.Metrics.Attributes = s.analysis.Attributes
	s.result.Metrics.Symbols = len(s.result.Symbols)
	s.current = PhaseSemantic
	if hasStageErrors(s.result.Diagnostics, string(PhaseSemantic)) {
		s.blocked = true
	}
}

func (s *CompilationSession) runIR() {
	s.exec = generateExecutable(s.program, s.analysis)
	s.result.Diagnostics = append(s.result.Diagnostics, s.exec.Diagnostics...)
	materializeIR(s.exec)
	s.result.IR = append([]IRInstruction(nil), s.exec.IR...)
	s.result.ControlFlow = cloneControlFlow(s.exec.ControlFlow)
	s.result.Metrics.IRInstructions = len(s.result.IR)
	s.result.Metrics.BasicBlocks = len(s.result.ControlFlow.Blocks)
	s.result.Metrics.FlowEdges = len(s.result.ControlFlow.Edges)
	s.current = PhaseIR
	if hasStageErrors(s.result.Diagnostics, string(PhaseIR)) {
		s.blocked = true
	}
}

func (s *CompilationSession) runCodegen() {
	materializeAssembly(s.exec)
	s.result.Assembly = append([]string(nil), s.exec.Assembly...)
	s.result.Metrics.AssemblyLines = len(s.result.Assembly)
	s.current = PhaseCodegen
	if hasStageErrors(s.result.Diagnostics, string(PhaseCodegen)) {
		s.blocked = true
	}
}

func (s *CompilationSession) runRuntime() {
	vm := newVM(s.exec)
	s.result.Runtime = vm.result(0)
	s.current = PhaseRuntime
	s.complete = true
	s.debug = &DebugSession{source: s.source, exec: s.exec, vm: vm}
}

func (s *CompilationSession) refreshDebugSession() {
	if !s.complete && !s.blocked {
		return
	}
	if s.debug == nil {
		s.debug = &DebugSession{source: s.source, exec: s.exec, blocked: true}
	}
	s.debug.base = cloneResult(s.result)
}

func (s *CompilationSession) nextPhase() CompilationPhase {
	if s.complete || s.blocked {
		return PhaseInitial
	}
	switch s.current {
	case PhaseInitial:
		return PhaseLexer
	case PhaseLexer:
		return PhaseParser
	case PhaseParser:
		return PhaseSemantic
	case PhaseSemantic:
		return PhaseIR
	case PhaseIR:
		return PhaseCodegen
	case PhaseCodegen:
		return PhaseRuntime
	default:
		return PhaseInitial
	}
}

func (s *CompilationSession) refreshStages() {
	s.result.Stages = buildPipelineStages(s.result, s.current, s.nextPhase(), s.blocked)
}

func buildPipelineStages(result Result, current, next CompilationPhase, blocked bool) []Stage {
	stages := make([]Stage, 0, len(compilationPhases))
	currentIndex := phaseIndex(current)
	for _, phase := range compilationPhases {
		status := "waiting"
		summary := "waiting"
		index := phaseIndex(phase)
		switch {
		case index <= currentIndex:
			status = "ok"
			if hasStageErrors(result.Diagnostics, string(phase)) {
				status = "error"
			}
			summary = completedPhaseSummary(result, phase)
		case blocked:
			status = "blocked"
			summary = "blocked by diagnostics"
		case phase == next:
			status = "ready"
			summary = "ready"
		}
		stages = append(stages, Stage{ID: string(phase), Name: phaseName(phase), Status: status, Summary: summary})
	}
	return stages
}

func phaseIndex(phase CompilationPhase) int {
	for index, candidate := range compilationPhases {
		if candidate == phase {
			return index + 1
		}
	}
	return 0
}

func phaseName(phase CompilationPhase) string {
	switch phase {
	case PhaseLexer:
		return "Lexer"
	case PhaseParser:
		return "Parser"
	case PhaseSemantic:
		return "Semantic analysis"
	case PhaseIR:
		return "Stack IR"
	case PhaseCodegen:
		return "Pseudo-assembly"
	case PhaseRuntime:
		return "Educational VM"
	default:
		return ""
	}
}

func completedPhaseSummary(result Result, phase CompilationPhase) string {
	switch phase {
	case PhaseLexer:
		return summarizeCount(result.Metrics.UsefulTokens, "token", "tokens")
	case PhaseParser:
		return summarizeCount(result.Metrics.ASTNodes, "AST node", "AST nodes")
	case PhaseSemantic:
		return fmt.Sprintf("%d classes · %d methods · %d attributes", result.Metrics.Classes, result.Metrics.Methods, result.Metrics.Attributes)
	case PhaseIR:
		return summarizeCount(result.Metrics.IRInstructions, "instruction", "instructions")
	case PhaseCodegen:
		return summarizeCount(result.Metrics.AssemblyLines, "line", "lines")
	case PhaseRuntime:
		return summarizeCount(result.Runtime.ExecutedSteps, "executed instruction", "executed instructions")
	default:
		return "waiting"
	}
}

func usefulTokenCount(tokens []Token) int {
	count := 0
	for _, token := range tokens {
		if token.Type != TokenEOF && token.Type != TokenError {
			count++
		}
	}
	return count
}

func cloneControlFlow(source ControlFlowGraph) ControlFlowGraph {
	return ControlFlowGraph{
		Blocks: append([]ControlFlowBlock(nil), source.Blocks...),
		Edges:  append([]ControlFlowEdge(nil), source.Edges...),
	}
}
