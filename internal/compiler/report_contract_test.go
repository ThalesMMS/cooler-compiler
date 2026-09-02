// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompiledReportCarriesV1Contract(t *testing.T) {
	result := CompileWithContract(pipelineProgram, "", ResultContract{
		CompilerVersion: "0.1.1",
		CompilerCommit:  "abc123",
		Options:         CompilationOptions{Execute: true, VMMaxSteps: 321},
		SourceUnits: []SourceUnitMetadata{
			NewSourceUnitMetadata("main.cl", pipelineProgram, 0),
		},
	})
	if result.SchemaVersion != ReportSchemaVersion || result.CompilerVersion != "0.1.1" || result.CompilerCommit != "abc123" {
		t.Fatalf("unexpected report identity: %+v", result)
	}
	if result.Target.OS == "" || result.Target.Arch == "" || result.Target.GoVersion == "" {
		t.Fatalf("missing target metadata: %+v", result.Target)
	}
	if result.Options.VMMaxSteps != 321 || !result.Options.Execute {
		t.Fatalf("missing compilation options: %+v", result.Options)
	}
	if err := validateReportContract(result); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	assertRequiredSchemaFields(t, filepath.Join("..", "..", "schemas", "compile-report-v1.schema.json"), data)
}

func TestCompileWithContractAppliesReportedVMLimit(t *testing.T) {
	result := CompileWithContract(`class Main { main(): Object { while true loop 0 pool }; };`, "", ResultContract{
		CompilerVersion: "0.1.1",
		Options:         CompilationOptions{Execute: true, VMMaxSteps: 1},
	})
	if result.Options.VMMaxSteps != 1 || result.Runtime.ExecutedSteps != 1 || !result.Runtime.Paused || result.Runtime.Halted {
		t.Fatalf("reported and applied VM limits diverged: options=%+v runtime=%+v", result.Options, result.Runtime)
	}
}

func TestCompileWithContractNormalizesAndHonorsExecutionOptions(t *testing.T) {
	program := `class Main inherits IO { main(): Object { out_string("ran") }; };`

	notExecuted := CompileWithContract(program, "", ResultContract{
		Options: CompilationOptions{Execute: false},
	})
	if notExecuted.Options.VMMaxSteps != defaultVMMaxSteps {
		t.Fatalf("normalized VM limit = %d, want %d", notExecuted.Options.VMMaxSteps, defaultVMMaxSteps)
	}
	if notExecuted.CompilerVersion != "1.0.0" {
		t.Fatalf("normalized compiler version = %q", notExecuted.CompilerVersion)
	}
	if len(notExecuted.Runtime.Output) != 0 || notExecuted.Runtime.ExecutedSteps != 0 {
		t.Fatalf("Execute=false ran the VM: %+v", notExecuted.Runtime)
	}

	executed := CompileWithContract(program, "", ResultContract{
		Options: CompilationOptions{Execute: true},
	})
	if executed.Options.VMMaxSteps != defaultVMMaxSteps || strings.Join(executed.Runtime.Output, "") != "ran" {
		t.Fatalf("Execute=true did not use normalized options: options=%+v runtime=%+v", executed.Options, executed.Runtime)
	}
}

func assertRequiredSchemaFields(t *testing.T, schemaPath string, document []byte) {
	t.Helper()
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("invalid JSON Schema %s: %v", schemaPath, err)
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(document, &value); err != nil {
		t.Fatal(err)
	}
	for _, field := range schema.Required {
		if _, exists := value[field]; !exists {
			t.Errorf("document does not satisfy required schema field %q", field)
		}
	}
}

func TestDecodeReportAcceptsLegacyUnversionedFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "reports", "v0-unversioned.json"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := DecodeReport(data)
	if err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != ReportSchemaVersion || result.CompilerVersion != "legacy-unversioned" {
		t.Fatalf("legacy report was not upgraded: %+v", result)
	}
	if result.Source != "class Main {};" || len(result.SourceUnits) != 1 {
		t.Fatalf("legacy payload was not preserved: %+v", result)
	}
}

func TestV1ReportGoldenFixture(t *testing.T) {
	result := newResult("", ResultContract{
		CompilerVersion: "0.1.1",
		CompilerCommit:  "abc123",
		Options:         CompilationOptions{VMMaxSteps: 100_000},
		SourceUnits:     []SourceUnitMetadata{NewSourceUnitMetadata("main.cl", "", 0)},
	})
	result.Target = ReportTarget{OS: "test", Arch: "test", GoVersion: "go-test"}
	result.Runtime.Halted = true
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	want, err := os.ReadFile(filepath.Join("testdata", "reports", "v1-report.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(want) {
		t.Fatalf("v1 report changed without updating its golden fixture\ngot:\n%s\nwant:\n%s", data, want)
	}
}

func TestDecodeReportRejectsUnsupportedOrIncompleteContracts(t *testing.T) {
	result := Compile(pipelineProgram)
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}

	document["schemaVersion"] = "2.0.0"
	data, _ = json.Marshal(document)
	if _, err := DecodeReport(data); !errors.Is(err, ErrUnsupportedReportSchema) {
		t.Fatalf("unsupported schema error = %v", err)
	}

	document["schemaVersion"] = ReportSchemaVersion
	delete(document, "compilerVersion")
	data, _ = json.Marshal(document)
	if _, err := DecodeReport(data); err == nil {
		t.Fatal("incomplete v1 report was accepted")
	}
}

func TestReportOptimizationContractAndLegacyUpgrade(t *testing.T) {
	result := CompileWithContract(pipelineProgram, "", ResultContract{
		CompilerVersion: "test",
		Options:         CompilationOptions{Execute: true, VMMaxSteps: 100_000, Optimization: OptimizationBasic},
	})
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeReport(data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Options.Optimization != OptimizationBasic || decoded.Optimization.Mode != OptimizationBasic || !decoded.Optimization.Enabled {
		t.Fatalf("optimization contract was not preserved: %+v", decoded.Optimization)
	}

	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	options := document["options"].(map[string]any)
	options["optimization"] = "unsafe"
	invalid, _ := json.Marshal(document)
	if _, err := DecodeReport(invalid); err == nil {
		t.Fatal("invalid optimization option was accepted")
	}

	document["schemaVersion"] = "1.3.0"
	delete(options, "optimization")
	delete(document, "optimization")
	legacy, _ := json.Marshal(document)
	upgraded, err := DecodeReport(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if upgraded.Options.Optimization != OptimizationNone || upgraded.Optimization.Mode != OptimizationNone || upgraded.Optimization.ExecutionIR != "unoptimized" {
		t.Fatalf("legacy optimization defaults = %+v / %+v", upgraded.Options, upgraded.Optimization)
	}
}
