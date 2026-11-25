// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	OptimizationNone        = "none"
	OptimizationBasic       = "basic"
	maxOptimizationEvidence = 250
)

func NormalizeOptimizationMode(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return OptimizationNone, nil
	}
	if value != OptimizationNone && value != OptimizationBasic {
		return OptimizationNone, errors.New("expected none or basic")
	}
	return value, nil
}

func optimizeExecutable(baseline *executable, mode string) (*executable, OptimizationReport, []Diagnostic) {
	report := OptimizationReport{
		Mode: mode, Enabled: mode == OptimizationBasic, ExecutionIR: "unoptimized",
		PassOrder: []string{}, Passes: []OptimizationPassReport{},
		BeforeIR: cloneIRInstructions(baseline.IR), BeforeCFG: cloneControlFlow(baseline.ControlFlow),
	}
	baselineDiagnostics := verifyExecutable(baseline)
	report.BaselineVerified = !hasErrors(baselineDiagnostics)
	baseline.verified = report.BaselineVerified
	if !report.BaselineVerified || mode == OptimizationNone {
		report.AfterIR = cloneIRInstructions(baseline.IR)
		report.AfterCFG = cloneControlFlow(baseline.ControlFlow)
		return baseline, report, baselineDiagnostics
	}

	optimized := cloneExecutableForOptimization(baseline)
	passes := []func(*executable) OptimizationPassReport{
		foldConstantPass,
		algebraicIdentityPass,
		simplifyConstantConditionPass,
		cleanupControlFlowPass,
		deadComputationPass,
	}
	diagnostics := append([]Diagnostic(nil), baselineDiagnostics...)
	for _, pass := range passes {
		passReport := pass(optimized)
		materializeIR(optimized)
		materializeAssembly(optimized)
		verification := verifyExecutable(optimized)
		passReport.Verified = !hasErrors(verification)
		report.PassOrder = append(report.PassOrder, passReport.Contract.Name)
		report.Passes = append(report.Passes, passReport)
		diagnostics = append(diagnostics, verification...)
		if !passReport.Verified {
			diagnostics = append(diagnostics, optimizationFailureDiagnostic(passReport.Contract.Name, verification))
			report.AfterIR = cloneIRInstructions(optimized.IR)
			report.AfterCFG = cloneControlFlow(optimized.ControlFlow)
			return baseline, report, diagnostics
		}
	}
	report.ExecutionIR = "optimized"
	report.OptimizedVerified = true
	report.AfterIR = cloneIRInstructions(optimized.IR)
	report.AfterCFG = cloneControlFlow(optimized.ControlFlow)
	optimized.verified = true
	return optimized, report, diagnostics
}

func optimizationContract(name, analysis string, cfg bool) OptimizationPassContract {
	return OptimizationPassContract{
		Name:          name,
		Preconditions: []string{"input executable passes Cooler's bytecode verifier", "typed operand categories are preserved"},
		Preserves: []string{
			"left-to-right evaluation order", "program output and input consumption",
			"runtime-error category and source attribution", "allocation, copy, identity, and dispatch behavior",
		},
		Analysis: analysis, CanAlterInstructions: true, CanAlterCFG: cfg,
		CanAlterAllocation: false, CanAlterSourceMapping: false,
	}
}

func foldConstantPass(exec *executable) OptimizationPassReport {
	report := OptimizationPassReport{
		Contract:           optimizationContract("constant-fold", "local adjacent literal stack simulation", false),
		BeforeInstructions: executableInstructionCount(exec), Changes: []OptimizationChange{}, Rejections: []OptimizationRejection{},
	}
	for _, method := range exec.MethodOrder {
		for {
			beforeTransformations := report.Transformations
			code := method.Code
			targets := instructionTargets(code)
			mapping := make([]int, len(code))
			output := make([]byteInstruction, 0, len(code))
			for index := 0; index < len(code); {
				if index+2 < len(code) && !targets[index+1] && !targets[index+2] {
					if folded, ok, rejection := foldBinary(code[index], code[index+1], code[index+2]); ok {
						newIndex := len(output)
						mapping[index], mapping[index+1], mapping[index+2] = newIndex, newIndex, newIndex
						output = append(output, folded)
						report.Transformations++
						appendOptimizationChange(&report, method.DisplayName, "constant-fold", code[index:index+3], folded, "pure literal operation evaluated at compile time")
						index += 3
						continue
					} else if rejection != "" {
						if appendOptimizationRejection(&report, method.DisplayName, formatByteInstructions(code[index:index+3]), rejection, code[index:index+3]) {
							report.Rejected++
						}
					}
				}
				if index+1 < len(code) && !targets[index+1] {
					if folded, ok := foldUnary(code[index], code[index+1]); ok {
						newIndex := len(output)
						mapping[index], mapping[index+1] = newIndex, newIndex
						output = append(output, folded)
						report.Transformations++
						appendOptimizationChange(&report, method.DisplayName, "constant-fold", code[index:index+2], folded, "pure literal unary operation evaluated at compile time")
						index += 2
						continue
					}
				}
				mapping[index] = len(output)
				output = append(output, cloneByteInstruction(code[index]))
				index++
			}
			retargetInstructions(output, mapping)
			method.Code = output
			if report.Transformations == beforeTransformations {
				break
			}
		}
	}
	report.AfterInstructions = executableInstructionCount(exec)
	return report
}

func foldBinary(left, right, operation byteInstruction) (byteInstruction, bool, string) {
	origins := mergeInstructionOrigins(left, right, operation)
	result := byteInstruction{Span: operation.Span, OperandSpan: operation.OperandSpan, OriginalIndexes: origins}
	switch {
	case left.Op == opPushInt && right.Op == opPushInt:
		switch operation.Op {
		case opAdd:
			result.Op, result.Int = opPushInt, left.Int.add(right.Int)
		case opSub:
			result.Op, result.Int = opPushInt, left.Int.subtract(right.Int)
		case opMul:
			result.Op, result.Int = opPushInt, left.Int.multiply(right.Int)
		case opDiv:
			value, valid := left.Int.divide(right.Int)
			if !valid {
				return byteInstruction{}, false, "division by zero must remain a runtime error at the original source span"
			}
			result.Op, result.Int = opPushInt, value
		case opLess:
			result.Op, result.Bool = opPushBool, left.Int.less(right.Int)
		case opLessEqual:
			result.Op, result.Bool = opPushBool, left.Int.lessEqual(right.Int)
		case opEqual:
			result.Op, result.Bool = opPushBool, left.Int.equal(right.Int)
		default:
			return byteInstruction{}, false, ""
		}
	case left.Op == opPushBool && right.Op == opPushBool && operation.Op == opEqual:
		result.Op, result.Bool = opPushBool, left.Bool == right.Bool
	case left.Op == opPushString && right.Op == opPushString && operation.Op == opEqual:
		return byteInstruction{}, false, "string pushes retain VM string-limit checks and are not folded"
	default:
		return byteInstruction{}, false, ""
	}
	result.Comment = "optimized constant fold: " + formatByteInstructions([]byteInstruction{left, right, operation})
	return result, true, ""
}

func foldUnary(value, operation byteInstruction) (byteInstruction, bool) {
	result := byteInstruction{Span: operation.Span, OperandSpan: operation.OperandSpan, OriginalIndexes: mergeInstructionOrigins(value, operation)}
	switch {
	case value.Op == opPushInt && operation.Op == opNeg:
		result.Op, result.Int = opPushInt, value.Int.complement()
	case value.Op == opPushBool && operation.Op == opNot:
		result.Op, result.Bool = opPushBool, !value.Bool
	default:
		return byteInstruction{}, false
	}
	result.Comment = "optimized constant fold: " + formatByteInstructions([]byteInstruction{value, operation})
	return result, true
}

func simplifyConstantConditionPass(exec *executable) OptimizationPassReport {
	report := OptimizationPassReport{
		Contract:           optimizationContract("constant-conditions", "literal condition and incoming-target analysis", true),
		BeforeInstructions: executableInstructionCount(exec), Changes: []OptimizationChange{}, Rejections: []OptimizationRejection{},
	}
	for _, method := range exec.MethodOrder {
		code := method.Code
		targets := instructionTargets(code)
		mapping := make([]int, len(code))
		output := make([]byteInstruction, 0, len(code))
		for index := 0; index < len(code); {
			if index+1 < len(code) && code[index].Op == opPushBool && code[index+1].Op == opJumpIfFalse {
				if targets[index+1] {
					if appendOptimizationRejection(&report, method.DisplayName, formatByteInstructions(code[index:index+2]), "the conditional instruction has an external incoming edge", code[index:index+2]) {
						report.Rejected++
					}
				} else {
					newIndex := len(output)
					mapping[index], mapping[index+1] = newIndex, newIndex
					if code[index].Bool {
						appendOptimizationChange(&report, method.DisplayName, "constant-condition", code[index:index+2], byteInstruction{Op: opcode("FALLTHROUGH")}, "constant true condition keeps only the fallthrough path")
					} else {
						replacement := cloneByteInstruction(code[index+1])
						replacement.Op = opJump
						replacement.OriginalIndexes = mergeInstructionOrigins(code[index], code[index+1])
						replacement.Comment = "optimized constant false condition"
						output = append(output, replacement)
						appendOptimizationChange(&report, method.DisplayName, "constant-condition", code[index:index+2], replacement, "constant false condition keeps only the branch target")
					}
					report.Transformations++
					index += 2
					continue
				}
			}
			mapping[index] = len(output)
			output = append(output, cloneByteInstruction(code[index]))
			index++
		}
		retargetInstructions(output, mapping)
		method.Code = output
		method.Code, report.Transformations = removeUnreachableInstructions(method, &report, report.Transformations)
	}
	report.AfterInstructions = executableInstructionCount(exec)
	return report
}

func cleanupControlFlowPass(exec *executable) OptimizationPassReport {
	report := OptimizationPassReport{
		Contract:           optimizationContract("cfg-cleanup", "reachability and jump-target fixed point", true),
		BeforeInstructions: executableInstructionCount(exec), Changes: []OptimizationChange{}, Rejections: []OptimizationRejection{},
	}
	for _, method := range exec.MethodOrder {
		for {
			beforeTransformations := report.Transformations
			code := method.Code
			for index := range code {
				thread := func(target int) int { return threadedTarget(code, target) }
				switch code[index].Op {
				case opJump, opJumpIfFalse:
					old := code[index].Target
					code[index].Target = thread(old)
					if old != code[index].Target {
						report.Transformations++
						appendOptimizationChange(&report, method.DisplayName, "jump-thread", []byteInstruction{{Op: code[index].Op, Target: old, Span: code[index].Span, OriginalIndexes: code[index].OriginalIndexes}}, code[index], "jump target threaded through unconditional jumps")
					}
				case opCaseSelect:
					before := cloneByteInstruction(code[index])
					changed := false
					for targetIndex := range code[index].Cases {
						old := code[index].Cases[targetIndex].Target
						code[index].Cases[targetIndex].Target = thread(old)
						if old != code[index].Cases[targetIndex].Target {
							report.Transformations++
							changed = true
						}
					}
					if changed {
						appendOptimizationChange(&report, method.DisplayName, "case-jump-thread", []byteInstruction{before}, code[index], "case targets threaded through unconditional jumps")
					}
				}
			}
			method.Code = code
			method.Code, report.Transformations = removeRedundantJumps(method, &report, report.Transformations)
			method.Code, report.Transformations = removeUnreachableInstructions(method, &report, report.Transformations)
			if report.Transformations == beforeTransformations {
				break
			}
		}
	}
	report.AfterInstructions = executableInstructionCount(exec)
	return report
}

func removeRedundantJumps(method *compiledMethod, report *OptimizationPassReport, transformations int) ([]byteInstruction, int) {
	code := method.Code
	mapping := make([]int, len(code))
	output := make([]byteInstruction, 0, len(code))
	for index, instruction := range code {
		mapping[index] = len(output)
		if instruction.Op == opJump && instruction.Target == index+1 {
			transformations++
			appendOptimizationChange(report, method.DisplayName, "redundant-jump", []byteInstruction{instruction}, byteInstruction{Op: opcode("FALLTHROUGH")}, "jump already targets the next instruction")
			continue
		}
		output = append(output, cloneByteInstruction(instruction))
	}
	retargetInstructions(output, mapping)
	return output, transformations
}

func removeUnreachableInstructions(method *compiledMethod, report *OptimizationPassReport, transformations int) ([]byteInstruction, int) {
	code := method.Code
	if len(code) == 0 {
		return code, transformations
	}
	reachable := make([]bool, len(code))
	queue := []int{0}
	for len(queue) > 0 {
		index := queue[0]
		queue = queue[1:]
		if index < 0 || index >= len(code) || reachable[index] {
			continue
		}
		reachable[index] = true
		instruction := code[index]
		switch instruction.Op {
		case opJump:
			queue = append(queue, instruction.Target)
		case opJumpIfFalse:
			queue = append(queue, instruction.Target, index+1)
		case opCaseSelect:
			for _, target := range instruction.Cases {
				queue = append(queue, target.Target)
			}
		case opReturn, opHalt:
		default:
			queue = append(queue, index+1)
		}
	}
	mapping := make([]int, len(code))
	output := make([]byteInstruction, 0, len(code))
	for index, instruction := range code {
		mapping[index] = len(output)
		if !reachable[index] {
			transformations++
			appendOptimizationChange(report, method.DisplayName, "unreachable", []byteInstruction{instruction}, byteInstruction{Op: opcode("REMOVED")}, "instruction is unreachable from the method entry")
			continue
		}
		output = append(output, cloneByteInstruction(instruction))
	}
	retargetInstructions(output, mapping)
	return output, transformations
}

func cloneExecutableForOptimization(source *executable) *executable {
	result := *source
	result.Methods = make(map[string]*compiledMethod, len(source.Methods))
	result.MethodOrder = make([]*compiledMethod, 0, len(source.MethodOrder))
	for _, method := range source.MethodOrder {
		clone := *method
		clone.Formals = append([]string(nil), method.Formals...)
		clone.FormalTypes = append([]string(nil), method.FormalTypes...)
		clone.Code = make([]byteInstruction, len(method.Code))
		for index, instruction := range method.Code {
			clone.Code[index] = cloneByteInstruction(instruction)
		}
		result.MethodOrder = append(result.MethodOrder, &clone)
		result.Methods[methodKey(clone.Owner, clone.Name)] = &clone
		if source.Entry == method {
			result.Entry = &clone
		}
	}
	result.IR = cloneIRInstructions(source.IR)
	result.Assembly = append([]string(nil), source.Assembly...)
	result.AssemblySpans = append([]Span(nil), source.AssemblySpans...)
	result.ControlFlow = cloneControlFlow(source.ControlFlow)
	result.Diagnostics = append([]Diagnostic(nil), source.Diagnostics...)
	result.verified = false
	return &result
}

func cloneByteInstruction(source byteInstruction) byteInstruction {
	result := source
	result.Cases = append([]caseTarget(nil), source.Cases...)
	result.OriginalIndexes = append([]int(nil), source.OriginalIndexes...)
	return result
}

func instructionTargets(code []byteInstruction) map[int]bool {
	targets := map[int]bool{}
	for _, instruction := range code {
		switch instruction.Op {
		case opJump, opJumpIfFalse:
			targets[instruction.Target] = true
		case opCaseSelect:
			for _, target := range instruction.Cases {
				targets[target.Target] = true
			}
		}
	}
	return targets
}

func retargetInstructions(code []byteInstruction, oldToNew []int) {
	retarget := func(old int) int {
		if old < 0 || old >= len(oldToNew) {
			return old
		}
		return oldToNew[old]
	}
	for index := range code {
		switch code[index].Op {
		case opJump, opJumpIfFalse:
			code[index].Target = retarget(code[index].Target)
		case opCaseSelect:
			for targetIndex := range code[index].Cases {
				code[index].Cases[targetIndex].Target = retarget(code[index].Cases[targetIndex].Target)
			}
		}
	}
}

func threadedTarget(code []byteInstruction, target int) int {
	seen := map[int]bool{}
	for target >= 0 && target < len(code) && code[target].Op == opJump && !seen[target] {
		seen[target] = true
		target = code[target].Target
	}
	return target
}

func mergeInstructionOrigins(instructions ...byteInstruction) []int {
	set := map[int]bool{}
	for _, instruction := range instructions {
		for _, index := range instruction.OriginalIndexes {
			set[index] = true
		}
	}
	result := make([]int, 0, len(set))
	for index := range set {
		result = append(result, index)
	}
	sort.Ints(result)
	return result
}

func executableInstructionCount(exec *executable) int {
	count := 0
	for _, method := range exec.MethodOrder {
		count += len(method.Code)
	}
	return count
}

func appendOptimizationChange(report *OptimizationPassReport, method, kind string, before []byteInstruction, after byteInstruction, explanation string) {
	if len(report.Changes) >= maxOptimizationEvidence {
		return
	}
	report.Changes = append(report.Changes, OptimizationChange{
		Method: method, Kind: kind, Before: formatByteInstructions(before), After: formatByteInstructions([]byteInstruction{after}),
		Explanation: explanation, OriginalIndexes: mergeInstructionOrigins(before...), Span: evidenceSpan(before, after),
	})
}

func appendOptimizationRejection(report *OptimizationPassReport, method, opportunity, reason string, instructions []byteInstruction) bool {
	origins := mergeInstructionOrigins(instructions...)
	for _, existing := range report.Rejections {
		if existing.Method == method && existing.Reason == reason && fmt.Sprint(existing.OriginalIndexes) == fmt.Sprint(origins) {
			return false
		}
	}
	if len(report.Rejections) >= maxOptimizationEvidence {
		return true
	}
	report.Rejections = append(report.Rejections, OptimizationRejection{
		Method: method, Opportunity: opportunity, Reason: reason,
		OriginalIndexes: origins, Span: evidenceSpan(instructions, byteInstruction{}),
	})
	return true
}

func evidenceSpan(before []byteInstruction, after byteInstruction) Span {
	if after.Span.StartLine > 0 {
		return after.Span
	}
	if len(before) > 0 {
		return before[len(before)-1].Span
	}
	return emptySpan()
}

func formatByteInstructions(instructions []byteInstruction) string {
	parts := make([]string, 0, len(instructions))
	for _, instruction := range instructions {
		text := string(instruction.Op)
		args := instructionArgs(instruction)
		if len(args) > 0 {
			text += " " + strings.Join(args, ", ")
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, " · ")
}

func optimizationFailureDiagnostic(pass string, diagnostics []Diagnostic) Diagnostic {
	span := emptySpan()
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == SeverityError {
			span = diagnostic.Span
			break
		}
	}
	return errorDiagnostic("optimization", "COL5200", fmt.Sprintf("optimization pass %s produced invalid bytecode", pass), "Use --opt=none and report this internal compiler defect.", span)
}

func cloneOptimizationReport(source OptimizationReport) OptimizationReport {
	result := source
	result.PassOrder = append([]string{}, source.PassOrder...)
	result.BeforeIR = cloneIRInstructions(source.BeforeIR)
	result.AfterIR = cloneIRInstructions(source.AfterIR)
	result.BeforeCFG = cloneControlFlow(source.BeforeCFG)
	result.AfterCFG = cloneControlFlow(source.AfterCFG)
	result.Passes = append([]OptimizationPassReport{}, source.Passes...)
	for index := range result.Passes {
		result.Passes[index].Contract.Preconditions = append([]string{}, source.Passes[index].Contract.Preconditions...)
		result.Passes[index].Contract.Preserves = append([]string{}, source.Passes[index].Contract.Preserves...)
		result.Passes[index].Changes = append([]OptimizationChange{}, source.Passes[index].Changes...)
		for change := range result.Passes[index].Changes {
			result.Passes[index].Changes[change].OriginalIndexes = append([]int{}, source.Passes[index].Changes[change].OriginalIndexes...)
		}
		result.Passes[index].Rejections = append([]OptimizationRejection{}, source.Passes[index].Rejections...)
		for rejection := range result.Passes[index].Rejections {
			result.Passes[index].Rejections[rejection].OriginalIndexes = append([]int{}, source.Passes[index].Rejections[rejection].OriginalIndexes...)
		}
	}
	return result
}

func cloneIRInstructions(source []IRInstruction) []IRInstruction {
	result := append([]IRInstruction{}, source...)
	for index := range result {
		result[index].Args = append([]string(nil), source[index].Args...)
		result[index].OriginalIndexes = append([]int{}, source[index].OriginalIndexes...)
	}
	return result
}
