// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

// algebraicIdentityPass retains the instruction preceding an identity pair.
// The entire operand evaluation stays in place, including calls and failures.
// Removed instructions map to that retained instruction; no incoming edge is
// permitted to bypass it or enter the middle of the identity pair.
func algebraicIdentityPass(exec *executable) OptimizationPassReport {
	report := OptimizationPassReport{
		Contract:           optimizationContract("algebraic-identities", "typed local identities with incoming-target exclusion", false),
		BeforeInstructions: executableInstructionCount(exec), Changes: []OptimizationChange{}, Rejections: []OptimizationRejection{},
	}
	for _, method := range exec.MethodOrder {
		for {
			before := report.Transformations
			code := method.Code
			targets := instructionTargets(code)
			mapping := make([]int, len(code))
			output := make([]byteInstruction, 0, len(code))
			for index := 0; index < len(code); {
				if index+2 < len(code) {
					first, second := code[index+1], code[index+2]
					rule := ""
					switch {
					case first.Op == opPushInt && first.Int == 0 && second.Op == opAdd:
						rule = "add-zero"
					case first.Op == opPushInt && first.Int == 0 && second.Op == opSub:
						rule = "subtract-zero"
					case first.Op == opPushInt && first.Int == 1 && second.Op == opMul:
						rule = "multiply-one"
					case first.Op == opNeg && second.Op == opNeg:
						rule = "double-integer-negation"
					case first.Op == opNot && second.Op == opNot:
						rule = "double-Boolean-negation"
					}
					rejection := ""
					if rule != "" && (targets[index+1] || targets[index+2]) {
						rejection = "incoming control flow enters the identity pattern"
					}
					if first.Op == opPushInt && first.Int == 0 && second.Op == opMul {
						rejection = "zero multiplication is not an identity; operand evaluation cannot be discarded"
					}
					if rejection != "" {
						if appendOptimizationRejection(&report, method.DisplayName, formatByteInstructions(code[index:index+3]), rejection, code[index:index+3]) {
							report.Rejected++
						}
					} else if rule != "" {
						retained := cloneByteInstruction(code[index])
						retained.OriginalIndexes = mergeInstructionOrigins(code[index : index+3]...)
						mapping[index], mapping[index+1], mapping[index+2] = len(output), len(output), len(output)
						output = append(output, retained)
						report.Transformations++
						appendOptimizationChange(&report, method.DisplayName, rule, code[index:index+3], retained, "remove typed identity operations while retaining operand evaluation and its source span")
						index += 3
						continue
					}
				}
				mapping[index] = len(output)
				output = append(output, cloneByteInstruction(code[index]))
				index++
			}
			retargetInstructions(output, mapping)
			method.Code = output
			if report.Transformations == before {
				break
			}
		}
	}
	report.AfterInstructions = executableInstructionCount(exec)
	return report
}
