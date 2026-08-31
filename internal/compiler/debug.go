// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

// DebugSession owns one compiled program and one mutable VM. The HTTP layer
// serializes access to a session; compiler values themselves stay independent
// from transport concerns.
type DebugSession struct {
	source  string
	base    Result
	exec    *executable
	vm      *VM
	blocked bool
	input   string
}

func NewDebugSession(source string) *DebugSession {
	pipeline := NewCompilationSession(source)
	snapshot := pipeline.Complete()
	if session, ok := pipeline.DebugSession(); ok {
		return session
	}
	return &DebugSession{source: source, base: snapshot.Result, blocked: true}
}

func (s *DebugSession) SetInput(text string) {
	s.input = text
	if s.vm != nil {
		s.vm.SetInput(text)
	}
}

func (s *DebugSession) Step() DebugSnapshot {
	if s.vm != nil && !s.vm.halted {
		s.vm.Step()
	}
	return s.Snapshot(false, 0)
}

func (s *DebugSession) Run(maxSteps int, breakpoints []int) DebugSnapshot {
	if s.vm != nil && !s.vm.halted {
		set := map[int]bool{}
		for _, line := range breakpoints {
			if line > 0 {
				set[line] = true
			}
		}
		s.vm.Run(maxSteps, set)
	}
	return s.Snapshot(false, 0)
}

func (s *DebugSession) Reset() DebugSnapshot {
	if s.exec != nil {
		s.vm = newVM(s.exec)
		s.vm.SetInput(s.input)
	}
	return s.Snapshot(false, 0)
}

func (s *DebugSession) Snapshot(light bool, traceFrom int) DebugSnapshot {
	result := cloneResult(s.base)
	status := "blocked"
	if s.vm != nil {
		runtime := s.vm.result(traceFrom)
		result.Runtime = runtime
		result.Diagnostics = append(result.Diagnostics, runtime.Diagnostics...)
		result.Stages = updateRuntimeStage(result.Stages, runtime)
		status = "ready"
		if runtime.Halted {
			status = "halted"
		} else if runtime.Paused {
			status = "paused"
		} else if runtime.ExecutedSteps > 0 {
			status = "running"
		}
		if light {
			result.Source = ""
			result.Tokens = nil
			result.AST = nil
			result.Symbols = nil
			result.IR = nil
			result.Assembly = nil
			result.ControlFlow = ControlFlowGraph{}
		}
		return DebugSnapshot{Result: result, Runtime: runtime, Status: status}
	}
	result.Runtime = VMResult{Halted: true}
	return DebugSnapshot{Result: result, Runtime: result.Runtime, Status: status}
}

func updateRuntimeStage(stages []Stage, runtime VMResult) []Stage {
	result := append([]Stage(nil), stages...)
	for index := range result {
		if result[index].ID != string(PhaseRuntime) {
			continue
		}
		result[index].Summary = summarizeCount(runtime.ExecutedSteps, "executed instruction", "executed instructions")
		switch {
		case hasStageErrors(runtime.Diagnostics, string(PhaseRuntime)):
			result[index].Status = "error"
		case runtime.Paused:
			result[index].Status = "paused"
		case runtime.Halted:
			result[index].Status = "ok"
		case runtime.ExecutedSteps > 0:
			result[index].Status = "ready"
		}
		break
	}
	return result
}

func cloneResult(source Result) Result {
	result := source
	result.Stages = append([]Stage(nil), source.Stages...)
	result.Tokens = append([]Token(nil), source.Tokens...)
	result.Symbols = append([]Symbol(nil), source.Symbols...)
	result.IR = append([]IRInstruction(nil), source.IR...)
	result.Assembly = append([]string(nil), source.Assembly...)
	result.Diagnostics = append([]Diagnostic(nil), source.Diagnostics...)
	result.ControlFlow.Blocks = append([]ControlFlowBlock(nil), source.ControlFlow.Blocks...)
	result.ControlFlow.Edges = append([]ControlFlowEdge(nil), source.ControlFlow.Edges...)
	return result
}
