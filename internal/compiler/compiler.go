// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

// Compile runs the complete educational COOL pipeline and executes Main.main.
func Compile(source string) Result {
	return CompileWithContract(source, "", ResultContract{
		Options: CompilationOptions{Execute: true, VMMaxSteps: defaultVMMaxSteps},
	})
}

// CompileWithInput is like Compile, but feeds program-input lines to IO.in_string
// and IO.in_int. Compile itself keeps empty input so the GUI and existing
// examples stay deterministic.
func CompileWithInput(source, input string) Result {
	return CompileWithContract(source, input, ResultContract{
		Options: CompilationOptions{Execute: true, VMMaxSteps: defaultVMMaxSteps, InputProvided: input != ""},
	})
}

// CompileProject compiles ordered source units as one COOL program without
// concatenating their contents. Units remain organizational inputs rather than
// modules or namespaces.
func CompileProject(units []SourceUnit, options ProjectOptions) Result {
	contract := ResultContract{
		CompilerVersion: options.CompilerVersion,
		CompilerCommit:  options.CompilerCommit,
		Options: CompilationOptions{
			Execute: true, VMMaxSteps: options.VMMaxSteps, InputProvided: options.Input != "", VMPolicy: options.VMPolicy,
			Optimization: options.Optimization,
		},
	}
	pipeline := NewProjectCompilationSession(units, contract)
	snapshot := pipeline.Complete()
	debug, ok := pipeline.DebugSession()
	if !ok {
		result := snapshot.Result
		result.Runtime = VMResult{Halted: true}
		return result
	}
	debug.SetInput(options.Input)
	return debug.Run(options.VMMaxSteps, nil).Result
}

// CompileWithContract compiles and executes source while preserving the report
// metadata supplied by a CLI, API, or other transport.
func CompileWithContract(source, input string, contract ResultContract) Result {
	if input != "" {
		contract.Options.InputProvided = true
	}
	maxSteps := contract.Options.VMMaxSteps
	if maxSteps <= 0 {
		maxSteps = defaultVMMaxSteps
	}
	contract.Options.VMMaxSteps = maxSteps
	pipeline := NewCompilationSessionWithContract(source, contract)
	snapshot := pipeline.Complete()
	if !contract.Options.Execute {
		return snapshot.Result
	}
	debug, ok := pipeline.DebugSession()
	if !ok {
		result := snapshot.Result
		result.Runtime = VMResult{Halted: true}
		return result
	}
	debug.SetInput(input)
	return debug.Run(maxSteps, nil).Result
}

func hasStageErrors(diagnostics []Diagnostic, stage string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Stage == stage && diagnostic.Severity == SeverityError {
			return true
		}
	}
	return false
}
