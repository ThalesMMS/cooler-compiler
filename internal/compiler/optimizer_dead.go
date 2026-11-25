// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

// This table deliberately proves removability for a small typed local subset.
// Unknown operations remain observable, potentially failing or nonterminating.
type localComputationEffect struct {
	pops, pushes                                        int
	writes, mayFail, allocates, mayDiverge, controlFlow bool
}

func computationEffect(op opcode) localComputationEffect {
	switch op {
	case opPushInt, opPushBool, opPushVoid, opLoadSelf:
		return localComputationEffect{pushes: 1}
	case opNeg, opNot, opIsVoid:
		return localComputationEffect{pops: 1, pushes: 1}
	case opAdd, opSub, opMul, opLess, opLessEqual, opEqual:
		return localComputationEffect{pops: 2, pushes: 1}
	case opDup:
		return localComputationEffect{pops: 1, pushes: 2}
	case opPop:
		return localComputationEffect{pops: 1}
	case opPushString, opLoad, opDiv:
		return localComputationEffect{mayFail: true}
	case opCall:
		return localComputationEffect{writes: true, mayFail: true, allocates: true, mayDiverge: true}
	case opNew:
		return localComputationEffect{writes: true, mayFail: true, allocates: true, mayDiverge: true}
	case opStore, opBind, opEnterScope, opExitScopeKeep:
		return localComputationEffect{writes: true, mayFail: true}
	default:
		return localComputationEffect{controlFlow: true, mayFail: true, mayDiverge: true}
	}
}

func (effect localComputationEffect) refusal() string {
	switch {
	case effect.allocates:
		return "allocation or constructor/call evaluation must remain observable"
	case effect.writes:
		return "writes or scope effects must remain observable; this is not dead-store elimination"
	case effect.controlFlow || effect.mayDiverge:
		return "control flow or potentially divergent evaluation cannot be removed"
	case effect.mayFail:
		return "possible failure or resource-policy check must remain observable"
	default:
		return ""
	}
}

func discardedPureSequence(code []byteInstruction, start int, targets map[int]bool) (int, string) {
	depth := 0
	for index := start; index < len(code) && index-start < 128; index++ {
		if index > start && targets[index] {
			return 0, "incoming control flow enters the candidate computation"
		}
		effect := computationEffect(code[index].Op)
		if reason := effect.refusal(); reason != "" {
			return 0, reason
		}
		if depth < effect.pops {
			return 0, "candidate depends on a value produced outside the local computation"
		}
		depth += effect.pushes - effect.pops
		if depth == 0 {
			if code[index].Op == opPop && index+1 < len(code) {
				return index + 1, ""
			}
			return 0, "candidate does not end in a discarded result"
		}
	}
	return 0, "candidate exceeds the 128-instruction local analysis bound or has no discarded result"
}

func deadComputationPass(exec *executable) OptimizationPassReport {
	report := OptimizationPassReport{Contract: optimizationContract("dead-computations", "bounded local stack, effect, failure, allocation and termination analysis", false), BeforeInstructions: executableInstructionCount(exec), Changes: []OptimizationChange{}, Rejections: []OptimizationRejection{}}
	for _, method := range exec.MethodOrder {
		code := method.Code
		targets := instructionTargets(code)
		mapping := make([]int, len(code))
		output := make([]byteInstruction, 0, len(code))
		for index := 0; index < len(code); {
			effect := computationEffect(code[index].Op)
			if effect.refusal() == "" && effect.pops == 0 && effect.pushes > 0 {
				end, reason := discardedPureSequence(code, index, targets)
				if end > index {
					for removed := index; removed < end; removed++ {
						mapping[removed] = len(output)
					}
					code[end] = cloneByteInstruction(code[end])
					code[end].OriginalIndexes = mergeInstructionOrigins(code[index : end+1]...)
					report.Transformations++
					appendOptimizationChange(&report, method.DisplayName, "discarded-pure-computation", code[index:end], byteInstruction{Op: opcode("REMOVED")}, "local typed computation has no observable effects/failures/allocations, terminates, and leaves no stack value")
					index = end
					continue
				}
				if index+1 < len(code) && code[index+1].Op == opPop && reason != "" {
					if appendOptimizationRejection(&report, method.DisplayName, formatByteInstructions(code[index:index+2]), reason, code[index:index+2]) {
						report.Rejected++
					}
				}
			} else if index+1 < len(code) && code[index+1].Op == opPop && effect.refusal() != "" {
				if appendOptimizationRejection(&report, method.DisplayName, formatByteInstructions(code[index:index+2]), effect.refusal(), code[index:index+2]) {
					report.Rejected++
				}
			}
			mapping[index] = len(output)
			output = append(output, cloneByteInstruction(code[index]))
			index++
		}
		retargetInstructions(output, mapping)
		method.Code = output
	}
	report.AfterInstructions = executableInstructionCount(exec)
	return report
}
