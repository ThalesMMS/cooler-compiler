// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var ErrUnsupportedReportSchema = errors.New("unsupported Cooler report schema")

// DecodeReport reads the current v1 contract and the unversioned v0 format
// emitted before v0.1.1. Compatible additive v1 minor versions are accepted;
// a different major version produces ErrUnsupportedReportSchema.
func DecodeReport(data []byte) (Result, error) {
	var header struct {
		SchemaVersion string `json:"schemaVersion"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return Result{}, fmt.Errorf("decode Cooler report header: %w", err)
	}

	var result Result
	if err := json.Unmarshal(data, &result); err != nil {
		return Result{}, fmt.Errorf("decode Cooler report: %w", err)
	}
	if header.SchemaVersion == "" {
		contract := normalizeResultContract(result.Source, ResultContract{CompilerVersion: "legacy-unversioned"})
		metadata := newResult(result.Source, contract)
		result.SchemaVersion = metadata.SchemaVersion
		result.CompilerVersion = metadata.CompilerVersion
		result.Target = metadata.Target
		result.Options = metadata.Options
		result.SourceUnits = metadata.SourceUnits
		result.Optimization = metadata.Optimization
		return result, nil
	}
	if !compatibleReportSchema(header.SchemaVersion) {
		return Result{}, fmt.Errorf("%w: %q (supported major: 1)", ErrUnsupportedReportSchema, header.SchemaVersion)
	}
	if err := validateReportContract(result); err != nil {
		return Result{}, err
	}
	if !reportSchemaAtLeast(result.SchemaVersion, 1, 2) {
		result.Options.VMPolicy = DefaultVMPolicy()
	}
	if !reportSchemaAtLeast(result.SchemaVersion, 1, 4) {
		result.Options.Optimization = OptimizationNone
		result.Optimization = OptimizationReport{Mode: OptimizationNone, ExecutionIR: "unoptimized"}
	}
	if !reportSchemaAtLeast(result.SchemaVersion, 1, 5) {
		result.Options.WarningRules = ""
	}
	if !reportSchemaAtLeast(result.SchemaVersion, 1, 6) {
		result.Options.Frontend = ""
	}
	return result, nil
}

func compatibleReportSchema(version string) bool {
	majorText, _, _ := strings.Cut(version, ".")
	major, err := strconv.Atoi(majorText)
	return err == nil && major == 1
}

func validateReportContract(result Result) error {
	if reportSchemaAtLeast(result.SchemaVersion, 1, 6) {
		frontend := result.Options.Frontend
		if frontend != "" && frontend != FrontendHandwritten && frontend != FrontendGenerated {
			return errors.New("invalid Cooler report: options.frontend must be handwritten or generated")
		}
	}
	if reportSchemaAtLeast(result.SchemaVersion, 1, 5) {
		if err := ValidateWarningRules(result.Options.WarningRules); err != nil {
			return fmt.Errorf("invalid Cooler report: options.warningRules: %w", err)
		}
	}
	if strings.TrimSpace(result.CompilerVersion) == "" {
		return errors.New("invalid Cooler report: compilerVersion is required")
	}
	if result.Target.OS == "" || result.Target.Arch == "" || result.Target.GoVersion == "" {
		return errors.New("invalid Cooler report: target os, arch, and goVersion are required")
	}
	if result.Options.VMMaxSteps <= 0 {
		return errors.New("invalid Cooler report: options.vmMaxSteps must be positive")
	}
	if reportSchemaAtLeast(result.SchemaVersion, 1, 2) {
		policy, err := NormalizeVMPolicy(result.Options.VMPolicy)
		if err != nil || policy != result.Options.VMPolicy {
			return errors.New("invalid Cooler report: options.vmPolicy must contain every validated limit")
		}
	}
	if reportSchemaAtLeast(result.SchemaVersion, 1, 4) {
		mode, err := NormalizeOptimizationMode(result.Options.Optimization)
		if err != nil || result.Options.Optimization == "" || mode != result.Options.Optimization {
			return errors.New("invalid Cooler report: options.optimization must be none or basic")
		}
		if result.Optimization.Mode != mode || result.Optimization.Enabled != (mode == OptimizationBasic) {
			return errors.New("invalid Cooler report: optimization mode and enabled state must match options.optimization")
		}
		if result.Optimization.ExecutionIR != "unoptimized" && result.Optimization.ExecutionIR != "optimized" {
			return errors.New("invalid Cooler report: optimization.executionIR must be unoptimized or optimized")
		}
		if result.Optimization.ExecutionIR == "optimized" && (!result.Optimization.Enabled || !result.Optimization.OptimizedVerified) {
			return errors.New("invalid Cooler report: optimized execution IR must be enabled and verified")
		}
		if len(result.Optimization.PassOrder) != len(result.Optimization.Passes) {
			return errors.New("invalid Cooler report: optimization passOrder must match passes")
		}
		for index, pass := range result.Optimization.Passes {
			if strings.TrimSpace(pass.Contract.Name) == "" || result.Optimization.PassOrder[index] != pass.Contract.Name {
				return fmt.Errorf("invalid Cooler report: optimization passOrder[%d] must match its pass contract", index)
			}
		}
	}
	if len(result.SourceUnits) == 0 {
		return errors.New("invalid Cooler report: at least one sourceUnit is required")
	}
	seenIdentity := map[string]struct{}{}
	requireIDs := reportSchemaAtLeast(result.SchemaVersion, 1, 1)
	for index, unit := range result.SourceUnits {
		if unit.Order != index {
			return fmt.Errorf("invalid Cooler report: sourceUnits[%d].order must be %d", index, index)
		}
		digest, digestErr := hex.DecodeString(unit.SHA256)
		if strings.TrimSpace(unit.Name) == "" || unit.ByteSize < 0 || digestErr != nil || len(digest) != sha256.Size {
			return fmt.Errorf("invalid Cooler report: sourceUnits[%d] metadata is incomplete", index)
		}
		identity := unit.Name
		identityLabel := "name"
		if requireIDs {
			identity = unit.ID
			identityLabel = "ID"
			if strings.TrimSpace(identity) == "" {
				return fmt.Errorf("invalid Cooler report: sourceUnits[%d].id is required", index)
			}
		}
		if _, exists := seenIdentity[identity]; exists {
			return fmt.Errorf("invalid Cooler report: duplicate source unit %s %q", identityLabel, identity)
		}
		seenIdentity[identity] = struct{}{}
	}
	return nil
}

func reportSchemaAtLeast(version string, wantMajor, wantMinor int) bool {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return false
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	return majorErr == nil && minorErr == nil && (major > wantMajor || major == wantMajor && minor >= wantMinor)
}
