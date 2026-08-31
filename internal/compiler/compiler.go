// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

// Compile runs the complete educational COOL pipeline and executes Main.main.
func Compile(source string) Result {
	pipeline := NewCompilationSession(source)
	snapshot := pipeline.Complete()
	debug, ok := pipeline.DebugSession()
	if !ok {
		result := snapshot.Result
		result.Runtime = VMResult{Halted: true}
		return result
	}
	return debug.Run(defaultVMMaxSteps, nil).Result
}

// CompileWithInput is like Compile, but feeds program-input lines to IO.in_string
// and IO.in_int. Compile itself keeps empty input so the GUI and existing
// examples stay deterministic.
func CompileWithInput(source, input string) Result {
	pipeline := NewCompilationSession(source)
	snapshot := pipeline.Complete()
	debug, ok := pipeline.DebugSession()
	if !ok {
		result := snapshot.Result
		result.Runtime = VMResult{Halted: true}
		return result
	}
	debug.SetInput(input)
	return debug.Run(defaultVMMaxSteps, nil).Result
}

func hasStageErrors(diagnostics []Diagnostic, stage string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Stage == stage && diagnostic.Severity == SeverityError {
			return true
		}
	}
	return false
}
