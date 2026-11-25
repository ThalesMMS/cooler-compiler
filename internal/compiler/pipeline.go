// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"fmt"
	"strings"
)

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
	source     string
	units      []SourceUnit
	current    CompilationPhase
	result     Result
	tokens     []Token
	unitTokens [][]Token
	program    *Program
	analysis   Analysis
	exec       *executable
	debug      *DebugSession
	complete   bool
	blocked    bool
}

// NewCompilationSession creates an idle pipeline without executing the Lexer.
func NewCompilationSession(source string) *CompilationSession {
	return NewCompilationSessionWithContract(source, ResultContract{})
}

// NewCompilationSessionWithContract creates an idle pipeline whose serialized
// results carry the version, source-unit, option, and runtime contract supplied
// by the transport that invoked the compiler.
func NewCompilationSessionWithContract(source string, contract ResultContract) *CompilationSession {
	return NewProjectCompilationSession([]SourceUnit{{ID: "source-1", Name: "program.cl", Content: source}}, contract)
}

// NewProjectCompilationSession creates one program from independently lexed and
// parsed source units. Unit order controls declaration and diagnostic order but
// never creates a namespace.
func NewProjectCompilationSession(units []SourceUnit, contract ResultContract) *CompilationSession {
	rawPolicy := contract.Options.VMPolicy
	policy, policyErr := NormalizeVMPolicy(rawPolicy)
	contract.Options.VMPolicy = policy
	optimization, optimizationErr := NormalizeOptimizationMode(contract.Options.Optimization)
	contract.Options.Optimization = optimization
	cloned := append([]SourceUnit(nil), units...)
	legacySource := ""
	if len(cloned) == 1 {
		legacySource = cloned[0].Content
	}
	if len(contract.SourceUnits) == 0 {
		contract.SourceUnits = make([]SourceUnitMetadata, 0, len(cloned))
		for index, unit := range cloned {
			contract.SourceUnits = append(contract.SourceUnits, NewSourceUnitMetadataWithID(unit.ID, unit.Name, unit.Content, index))
		}
	}
	session := &CompilationSession{source: legacySource, units: cloned, result: newResult(legacySource, contract)}
	session.result.Diagnostics = validateSourceUnits(cloned)
	if err := ValidateWarningRules(contract.Options.WarningRules); err != nil {
		session.result.Diagnostics = append(session.result.Diagnostics, errorDiagnostic("input", "COL0007", err.Error(), "Use all, none, or comma-separated warning codes.", emptySpan()))
		session.blocked = true
	}
	if policyErr != nil {
		session.result.Diagnostics = append(session.result.Diagnostics, errorDiagnostic(
			"input", "COL0005", "invalid VM execution policy: "+policyErr.Error(),
			"Use zero for a safe default or keep every override within its documented ceiling.", emptySpan(),
		))
		session.blocked = true
	}
	if optimizationErr != nil {
		session.result.Diagnostics = append(session.result.Diagnostics, errorDiagnostic(
			"input", "COL0006", "invalid optimization mode: "+optimizationErr.Error(),
			"Use none for the baseline pipeline or basic for conservative educational passes.", emptySpan(),
		))
		session.blocked = true
	}
	if session.blocked {
		session.refreshDebugSession()
	}
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
	s.tokens = nil
	s.unitTokens = nil
	for _, unit := range s.units {
		tokens, diagnostics := LexSource(unit.ID, unit.Content)
		s.unitTokens = append(s.unitTokens, tokens)
		s.tokens = append(s.tokens, tokens...)
		s.result.Diagnostics = append(s.result.Diagnostics, diagnostics...)
	}
	s.result.Tokens = append([]Token(nil), s.tokens...)
	s.result.Metrics.UsefulTokens = usefulTokenCount(s.tokens)
	s.current = PhaseLexer
}

func (s *CompilationSession) runParser() {
	program := &Program{}
	for _, tokens := range s.unitTokens {
		if usefulTokenCount(tokens) == 0 {
			continue
		}
		unitProgram, diagnostics := Parse(tokens)
		s.result.Diagnostics = append(s.result.Diagnostics, diagnostics...)
		if unitProgram != nil {
			program.Classes = append(program.Classes, unitProgram.Classes...)
		}
	}
	if len(program.Classes) == 0 && !hasStageErrors(s.result.Diagnostics, "input") && !hasStageErrors(s.result.Diagnostics, string(PhaseLexer)) && !hasStageErrors(s.result.Diagnostics, string(PhaseParser)) {
		span := emptySpan()
		if len(s.units) > 0 {
			span.SourceID = s.units[0].ID
		}
		s.result.Diagnostics = append(s.result.Diagnostics, errorDiagnostic("parser", "COL2002", "program contains no class declarations", "Declare at least 'class Main { main() : Object { 0 }; };'.", span))
	}
	if len(program.Classes) > 0 {
		program.Span = program.Classes[0].Span
		if program.Classes[0].Span.SourceID == program.Classes[len(program.Classes)-1].Span.SourceID {
			program.Span = joinSpans(program.Classes[0].Span, program.Classes[len(program.Classes)-1].Span)
		}
	} else {
		program.Span = emptySpan()
		if len(s.units) > 0 {
			program.Span.SourceID = s.units[0].ID
		}
	}
	s.program = program
	s.result.AST = program.ToAST()
	s.result.Metrics.ASTNodes = countASTNodes(s.result.AST)
	s.current = PhaseParser
	if hasStageErrors(s.result.Diagnostics, "input") || hasStageErrors(s.result.Diagnostics, string(PhaseLexer)) || hasStageErrors(s.result.Diagnostics, string(PhaseParser)) {
		s.blocked = true
	}
}

func validateSourceUnits(units []SourceUnit) []Diagnostic {
	diagnostics := []Diagnostic{}
	if len(units) == 0 {
		return []Diagnostic{errorDiagnostic("input", "COL0001", "project contains no source units", "Provide at least one COOL source unit.", emptySpan())}
	}
	seen := map[string]bool{}
	for _, unit := range units {
		span := emptySpan()
		span.SourceID = unit.ID
		switch {
		case strings.TrimSpace(unit.ID) == "":
			diagnostics = append(diagnostics, errorDiagnostic("input", "COL0002", "source unit ID is empty", "Assign every source unit a stable non-empty ID.", span))
		case seen[unit.ID]:
			diagnostics = append(diagnostics, errorDiagnostic("input", "COL0003", fmt.Sprintf("duplicate source unit ID %s", unit.ID), "Use a unique stable ID even when display names are duplicated.", span))
		default:
			seen[unit.ID] = true
		}
		if strings.TrimSpace(unit.Name) == "" {
			diagnostics = append(diagnostics, errorDiagnostic("input", "COL0004", "source unit name is empty", "Give the source unit a display name.", span))
		}
	}
	return diagnostics
}

func (s *CompilationSession) runSemantic() {
	s.analysis = Analyze(s.program)
	s.analysis.Diagnostics = selectWarnings(s.analysis.Diagnostics, s.result.Options.WarningRules)
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
	s.refreshVisualizations()
	s.current = PhaseSemantic
	if hasStageErrors(s.result.Diagnostics, string(PhaseSemantic)) {
		s.blocked = true
	}
}

func (s *CompilationSession) runIR() {
	baseline := generateExecutable(s.program, s.analysis)
	materializeIR(baseline)
	materializeAssembly(baseline)
	selected, optimization, verificationDiagnostics := optimizeExecutable(baseline, s.result.Options.Optimization)
	s.exec = selected
	s.result.Optimization = optimization
	s.result.Diagnostics = append(s.result.Diagnostics, baseline.Diagnostics...)
	s.result.Diagnostics = append(s.result.Diagnostics, verificationDiagnostics...)
	s.result.IR = append([]IRInstruction(nil), s.exec.IR...)
	s.result.ControlFlow = cloneControlFlow(s.exec.ControlFlow)
	s.result.Metrics.IRInstructions = len(s.result.IR)
	s.result.Metrics.BasicBlocks = len(s.result.ControlFlow.Blocks)
	s.result.Metrics.FlowEdges = len(s.result.ControlFlow.Edges)
	s.refreshVisualizations()
	s.current = PhaseIR
	if hasStageErrors(s.result.Diagnostics, string(PhaseIR)) || hasErrors(verificationDiagnostics) {
		s.blocked = true
	}
}

func (s *CompilationSession) runCodegen() {
	if len(s.exec.Assembly) == 0 {
		materializeAssembly(s.exec)
	}
	s.result.Assembly = append([]string(nil), s.exec.Assembly...)
	s.result.AssemblySpans = append([]Span(nil), s.exec.AssemblySpans...)
	s.result.Metrics.AssemblyLines = len(s.result.Assembly)
	if !s.exec.verified {
		verificationDiagnostics := verifyExecutable(s.exec)
		s.exec.Diagnostics = append(s.exec.Diagnostics, verificationDiagnostics...)
		s.exec.verified = !hasErrors(verificationDiagnostics)
		s.result.Diagnostics = append(s.result.Diagnostics, verificationDiagnostics...)
	}
	s.refreshVisualizations()
	s.current = PhaseCodegen
	if hasStageErrors(s.result.Diagnostics, string(PhaseCodegen)) {
		s.blocked = true
	}
}

func (s *CompilationSession) runRuntime() {
	vm := newVMWithPolicy(s.exec, s.result.Options.VMPolicy)
	s.result.Runtime = vm.result(0)
	s.refreshVisualizations()
	s.current = PhaseRuntime
	s.complete = true
	s.debug = &DebugSession{source: s.source, exec: s.exec, vm: vm, policy: s.result.Options.VMPolicy}
}

func (s *CompilationSession) refreshVisualizations() {
	s.result.Visualizations = BuildVisualizations(s.program, s.analysis, s.result)
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
