// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import "fmt"

const defaultVMMaxSteps = 100_000

// VMPolicy centralizes deterministic execution and retained-state bounds. Zero
// fields select the corresponding desktop default; negative values and values
// above the local safety ceiling are rejected.
type VMPolicy struct {
	MaxInstructionsPerRun int `json:"maxInstructionsPerRun"`
	MaxInstructionsTotal  int `json:"maxInstructionsTotal"`
	MaxOperandStackDepth  int `json:"maxOperandStackDepth"`
	MaxCallFrameDepth     int `json:"maxCallFrameDepth"`
	MaxScopeDepth         int `json:"maxScopeDepth"`
	MaxObjectAllocations  int `json:"maxObjectAllocations"`
	MaxTraceEntries       int `json:"maxTraceEntries"`
	MaxTraceBytes         int `json:"maxTraceBytes"`
	MaxStringBytes        int `json:"maxStringBytes"`
	MaxOutputBytes        int `json:"maxOutputBytes"`
	MaxInputBytes         int `json:"maxInputBytes"`
	MaxInputLines         int `json:"maxInputLines"`
	MaxBreakpoints        int `json:"maxBreakpoints"`
}

var defaultVMPolicy = VMPolicy{
	MaxInstructionsPerRun: 100_000,
	MaxInstructionsTotal:  100_000,
	MaxOperandStackDepth:  4_096,
	MaxCallFrameDepth:     256,
	MaxScopeDepth:         1_024,
	MaxObjectAllocations:  10_000,
	MaxTraceEntries:       5_000,
	MaxTraceBytes:         16 << 20,
	MaxStringBytes:        1 << 20,
	MaxOutputBytes:        4 << 20,
	MaxInputBytes:         1 << 20,
	MaxInputLines:         10_000,
	MaxBreakpoints:        1_024,
}

var maximumVMPolicy = VMPolicy{
	MaxInstructionsPerRun: 1_000_000,
	MaxInstructionsTotal:  1_000_000,
	MaxOperandStackDepth:  65_536,
	MaxCallFrameDepth:     4_096,
	MaxScopeDepth:         16_384,
	MaxObjectAllocations:  1_000_000,
	MaxTraceEntries:       50_000,
	MaxTraceBytes:         64 << 20,
	MaxStringBytes:        16 << 20,
	MaxOutputBytes:        64 << 20,
	MaxInputBytes:         16 << 20,
	MaxInputLines:         100_000,
	MaxBreakpoints:        10_000,
}

// DefaultVMPolicy returns a copy of the safe desktop policy.
func DefaultVMPolicy() VMPolicy { return defaultVMPolicy }

// MaximumVMPolicy returns the accepted local override ceilings.
func MaximumVMPolicy() VMPolicy { return maximumVMPolicy }

// NormalizeVMPolicy fills zero values and validates every configured limit.
func NormalizeVMPolicy(policy VMPolicy) (VMPolicy, error) {
	perRunConfigured := policy.MaxInstructionsPerRun != 0
	totalConfigured := policy.MaxInstructionsTotal != 0
	fields := []struct {
		name     string
		value    *int
		fallback int
		maximum  int
	}{
		{"maxInstructionsPerRun", &policy.MaxInstructionsPerRun, defaultVMPolicy.MaxInstructionsPerRun, maximumVMPolicy.MaxInstructionsPerRun},
		{"maxInstructionsTotal", &policy.MaxInstructionsTotal, defaultVMPolicy.MaxInstructionsTotal, maximumVMPolicy.MaxInstructionsTotal},
		{"maxOperandStackDepth", &policy.MaxOperandStackDepth, defaultVMPolicy.MaxOperandStackDepth, maximumVMPolicy.MaxOperandStackDepth},
		{"maxCallFrameDepth", &policy.MaxCallFrameDepth, defaultVMPolicy.MaxCallFrameDepth, maximumVMPolicy.MaxCallFrameDepth},
		{"maxScopeDepth", &policy.MaxScopeDepth, defaultVMPolicy.MaxScopeDepth, maximumVMPolicy.MaxScopeDepth},
		{"maxObjectAllocations", &policy.MaxObjectAllocations, defaultVMPolicy.MaxObjectAllocations, maximumVMPolicy.MaxObjectAllocations},
		{"maxTraceEntries", &policy.MaxTraceEntries, defaultVMPolicy.MaxTraceEntries, maximumVMPolicy.MaxTraceEntries},
		{"maxTraceBytes", &policy.MaxTraceBytes, defaultVMPolicy.MaxTraceBytes, maximumVMPolicy.MaxTraceBytes},
		{"maxStringBytes", &policy.MaxStringBytes, defaultVMPolicy.MaxStringBytes, maximumVMPolicy.MaxStringBytes},
		{"maxOutputBytes", &policy.MaxOutputBytes, defaultVMPolicy.MaxOutputBytes, maximumVMPolicy.MaxOutputBytes},
		{"maxInputBytes", &policy.MaxInputBytes, defaultVMPolicy.MaxInputBytes, maximumVMPolicy.MaxInputBytes},
		{"maxInputLines", &policy.MaxInputLines, defaultVMPolicy.MaxInputLines, maximumVMPolicy.MaxInputLines},
		{"maxBreakpoints", &policy.MaxBreakpoints, defaultVMPolicy.MaxBreakpoints, maximumVMPolicy.MaxBreakpoints},
	}
	for _, field := range fields {
		if *field.value == 0 {
			*field.value = field.fallback
		}
		if *field.value < 1 || *field.value > field.maximum {
			return defaultVMPolicy, fmt.Errorf("vmPolicy.%s must be between 1 and %d", field.name, field.maximum)
		}
	}
	if !perRunConfigured && totalConfigured && policy.MaxInstructionsPerRun > policy.MaxInstructionsTotal {
		policy.MaxInstructionsPerRun = policy.MaxInstructionsTotal
	}
	if policy.MaxInstructionsPerRun > policy.MaxInstructionsTotal {
		return defaultVMPolicy, fmt.Errorf("vmPolicy.maxInstructionsPerRun must not exceed maxInstructionsTotal")
	}
	return policy, nil
}
