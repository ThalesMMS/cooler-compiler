// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import "fmt"

func (vm *VM) evaluateWatches() []WatchResult {
	if vm.watchCache == nil || vm.watchCacheStep != vm.executed || vm.watchCacheDepth != len(vm.frames) {
		vm.watchCache = map[string]WatchResult{}
		vm.watchCacheStep = vm.executed
		vm.watchCacheDepth = len(vm.frames)
	}
	results := make([]WatchResult, 0, len(vm.watches))
	for _, watch := range vm.watches {
		result := WatchResult{ID: watch.ID, Expression: watch.Expression, Enabled: watchEnabled(watch)}
		if !result.Enabled {
			results = append(results, result)
			continue
		}
		if !vm.watchesMayEvaluate() {
			result.Diagnostic = "watch evaluation is available only while execution is stopped"
			results = append(results, result)
			continue
		}
		cacheKey := fmt.Sprintf("%s\x00%s\x00%d\x00%d", watch.ID, watch.Expression, vm.executed, len(vm.frames))
		if cached, ok := vm.watchCache[cacheKey]; ok {
			results = append(results, cached)
			continue
		}
		parsed, err := parseDebugExpression(watch.Expression)
		if err != nil {
			result.Diagnostic = err.Error()
		} else {
			value, evaluationErr := evaluateDebugExpression(parsed, vm.currentFrame())
			result.Dependencies = append([]string(nil), parsed.dependencies...)
			if evaluationErr != nil {
				result.Diagnostic = evaluationErr.Error()
			} else {
				public := value.public()
				result.Value = &public
				result.StaticType = public.Type
				result.DynamicType = public.Type
			}
		}
		if location := vm.currentDebugLocation(); location != nil {
			copy := *location
			result.Location = &copy
		}
		vm.watchCache[cacheKey] = result
		results = append(results, result)
	}
	return results
}

func (vm *VM) watchesMayEvaluate() bool {
	if vm == nil {
		return false
	}
	if vm.paused || vm.waitingInput != nil || vm.halted {
		return true
	}
	switch vm.stopReason {
	case StopReasonInstruction, StopReasonStepInto, StopReasonStepOver, StopReasonStepOut, StopReasonRunToCursor, StopReasonBreakpoint, StopReasonInstructionLimit:
		return true
	default:
		return false
	}
}
