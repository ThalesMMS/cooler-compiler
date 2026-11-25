// SPDX-License-Identifier: AGPL-3.0-or-later

package scripts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
	"github.com/ThalesMMS/cooler-compiler/internal/releaseartifact"
)

func TestMachineReadableSchemasValidateProducedDocuments(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js is required to produce the browser report archive manifest")
	}
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	report := compiler.CompileWithContract(`class Main { main(): Object { 0 }; };`, "", compiler.ResultContract{
		CompilerVersion: "0.1.1",
		CompilerCommit:  strings.Repeat("a", 40),
		Options:         compiler.CompilationOptions{Execute: true, VMMaxSteps: 100_000},
	})
	validateSchemaDocument(t, filepath.Join(repoRoot, "schemas", "compile-report-v1.schema.json"), mustJSON(t, report))
	validateSchemaDocument(t, filepath.Join(repoRoot, "schemas", "diagnostics-v1.schema.json"), mustJSON(t, append([]compiler.Diagnostic{}, report.Diagnostics...)))

	archiveScript := `(async () => {
  const client = require("./internal/web/static/client-utils.js");
  const files = await client.buildArchiveFiles({
    schemaVersion: "1.0.0", compilerVersion: "0.1.1",
    target: { os: "darwin", arch: "arm64", goVersion: "go1.25" },
    options: { execute: true, vmMaxSteps: 100000, inputProvided: false },
    sourceUnits: [{ name: "main.cl", order: 0, byteSize: 0, sha256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" }],
    source: "", diagnostics: [], tokens: [], symbols: [], ir: [], assembly: [], runtime: {}
  }, "main.cl");
  process.stdout.write(files.at(-1).content);
})().catch((error) => { console.error(error); process.exit(1); });`
	command := exec.CommandContext(context.Background(), "node", "-e", archiveScript)
	command.Dir = repoRoot
	archiveManifest, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("produce report archive manifest: %v\n%s", err, archiveManifest)
	}
	validateSchemaDocument(t, filepath.Join(repoRoot, "schemas", "report-archive-manifest-v1.schema.json"), archiveManifest)

	packages := make([]releaseartifact.PackageManifest, 4)
	for index, target := range []releaseartifact.Target{{OS: "linux", Arch: "amd64"}, {OS: "linux", Arch: "arm64"}, {OS: "macos", Arch: "arm64"}, {OS: "windows", Arch: "amd64"}} {
		packages[index] = releaseartifact.PackageManifest{
			Filename: fmt.Sprintf("package-%d.zip", index), ByteSize: 1, SHA256: strings.Repeat(fmt.Sprint(index), 64),
			Target: target, Modes: []string{"cli"}, ValidationStatus: "built",
		}
	}
	releaseManifest := releaseartifact.Manifest{
		SchemaVersion: releaseartifact.ManifestSchemaVersion, ManifestType: "cooler-release", Project: "cooler-compiler",
		Version: "0.1.1", SourceCommit: strings.Repeat("a", 40), SourceDateEpoch: 1,
		Toolchain:  releaseartifact.Toolchain{GoVersion: runtime.Version(), HostOS: runtime.GOOS, HostArch: runtime.GOARCH},
		BuildFlags: []string{"-trimpath"}, Packages: packages,
		Validation: releaseartifact.Validation{BuildStatus: "built", RuntimeStatus: "see-evidence-document", SkippedChecks: []string{}, EvidenceDocument: "docs/release-validation-v0.1.1.md"},
	}
	validateSchemaDocument(t, filepath.Join(repoRoot, "schemas", "release-manifest-v1.schema.json"), mustJSON(t, releaseManifest))
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func validateSchemaDocument(t *testing.T, schemaPath string, document []byte) {
	t.Helper()
	schemaData, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	var schema any
	if err := json.Unmarshal(schemaData, &schema); err != nil {
		t.Fatalf("invalid JSON Schema %s: %v", schemaPath, err)
	}
	if err := validateSupportedSchema(schema, "$schema"); err != nil {
		t.Fatalf("unsupported JSON Schema %s: %v", schemaPath, err)
	}
	var value any
	if err := json.Unmarshal(document, &value); err != nil {
		t.Fatalf("invalid produced JSON for %s: %v", schemaPath, err)
	}
	if err := validateSchemaValue(schema, schema, value, "$"); err != nil {
		t.Fatalf("%s: %v", filepath.Base(schemaPath), err)
	}
}

func validateSupportedSchema(schema any, path string) error {
	rule, ok := schema.(map[string]any)
	if !ok {
		return nil
	}
	supported := map[string]bool{
		"$schema": true, "$id": true, "$ref": true, "$defs": true,
		"title": true, "type": true, "required": true, "properties": true,
		"additionalProperties": true, "const": true, "enum": true,
		"minItems": true, "items": true, "minLength": true,
		"pattern": true, "minimum": true, "maximum": true,
		"minProperties": true, "allOf": true, "if": true, "then": true,
	}
	for keyword := range rule {
		if !supported[keyword] {
			return fmt.Errorf("%s uses unsupported keyword %q", path, keyword)
		}
	}
	if _, exists := rule["$ref"]; exists && len(rule) != 1 {
		return fmt.Errorf("%s combines $ref with unsupported sibling keywords", path)
	}
	if value, exists := rule["additionalProperties"]; exists {
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s uses unsupported schema-valued additionalProperties", path)
		}
	}
	if value, exists := rule["type"]; exists && !supportedJSONTypes(value) {
		return fmt.Errorf("%s uses unsupported type declaration %#v", path, value)
	}
	for _, containerName := range []string{"properties", "$defs"} {
		container, _ := rule[containerName].(map[string]any)
		for name, child := range container {
			if err := validateSupportedSchema(child, path+"."+containerName+"."+name); err != nil {
				return err
			}
		}
	}
	if child, exists := rule["items"]; exists {
		if _, ok := child.(map[string]any); !ok {
			return fmt.Errorf("%s uses unsupported non-schema items", path)
		}
		if err := validateSupportedSchema(child, path+".items"); err != nil {
			return err
		}
	}
	for _, childName := range []string{"if", "then"} {
		if child, exists := rule[childName]; exists {
			if _, ok := child.(map[string]any); !ok {
				return fmt.Errorf("%s uses unsupported non-schema %s", path, childName)
			}
			if err := validateSupportedSchema(child, path+"."+childName); err != nil {
				return err
			}
		}
	}
	if children, exists := rule["allOf"]; exists {
		list, ok := children.([]any)
		if !ok || len(list) == 0 {
			return fmt.Errorf("%s uses unsupported allOf declaration", path)
		}
		for index, child := range list {
			if _, ok := child.(map[string]any); !ok {
				return fmt.Errorf("%s.allOf[%d] is not a schema", path, index)
			}
			if err := validateSupportedSchema(child, fmt.Sprintf("%s.allOf[%d]", path, index)); err != nil {
				return err
			}
		}
	}
	return nil
}

func supportedJSONTypes(value any) bool {
	if alternatives, ok := value.([]any); ok {
		if len(alternatives) == 0 {
			return false
		}
		for _, alternative := range alternatives {
			if !supportedJSONTypes(alternative) {
				return false
			}
		}
		return true
	}
	name, ok := value.(string)
	if !ok {
		return false
	}
	switch name {
	case "object", "array", "string", "boolean", "integer", "number", "null":
		return true
	default:
		return false
	}
}

func TestSchemaValidatorRejectsUnsupportedKeywords(t *testing.T) {
	schema := map[string]any{"type": "string", "maxLength": float64(10)}
	if err := validateSupportedSchema(schema, "$schema"); err == nil || !strings.Contains(err.Error(), "maxLength") {
		t.Fatalf("unsupported keyword error = %v", err)
	}
}

func TestCompileReportSchemaRejectsMalformedDiagnosticChildren(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	schemaData, err := os.ReadFile(filepath.Join(repoRoot, "schemas", "compile-report-v1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema any
	if err := json.Unmarshal(schemaData, &schema); err != nil {
		t.Fatal(err)
	}
	diagnosticRule, err := resolveLocalReference(schema, "#/$defs/diagnostic")
	if err != nil {
		t.Fatal(err)
	}
	span := map[string]any{
		"startLine": float64(1), "startColumn": float64(1),
		"endLine": float64(1), "endColumn": float64(1),
		"startOffset": float64(0), "endOffset": float64(0),
	}
	diagnostic := map[string]any{
		"stage": "semantic", "severity": "error", "message": "invalid",
		"line": float64(1), "column": float64(1), "span": span,
		"related": []any{map[string]any{"label": "missing location"}},
	}
	if err := validateSchemaValue(schema, diagnosticRule, diagnostic, "$.diagnostics[0]"); err == nil {
		t.Fatal("compile report schema accepted a malformed related diagnostic entry")
	}
}

func validateSchemaValue(root, schema, value any, path string) error {
	if allowed, ok := schema.(bool); ok {
		if !allowed {
			return fmt.Errorf("%s is forbidden by this schema version", path)
		}
		return nil
	}
	rule, ok := schema.(map[string]any)
	if !ok {
		return nil
	}
	if reference, ok := rule["$ref"].(string); ok {
		resolved, err := resolveLocalReference(root, reference)
		if err != nil {
			return err
		}
		return validateSchemaValue(root, resolved, value, path)
	}
	if schemas, ok := rule["allOf"].([]any); ok {
		for _, child := range schemas {
			if err := validateSchemaValue(root, child, value, path); err != nil {
				return err
			}
		}
	}
	if condition, exists := rule["if"]; exists {
		if validateSchemaValue(root, condition, value, path) == nil {
			if consequence, exists := rule["then"]; exists {
				if err := validateSchemaValue(root, consequence, value, path); err != nil {
					return err
				}
			}
		}
	}
	if constant, exists := rule["const"]; exists && !reflect.DeepEqual(constant, value) {
		return fmt.Errorf("%s = %#v, want const %#v", path, value, constant)
	}
	if enum, ok := rule["enum"].([]any); ok {
		matched := false
		for _, candidate := range enum {
			matched = matched || reflect.DeepEqual(candidate, value)
		}
		if !matched {
			return fmt.Errorf("%s = %#v is outside enum", path, value)
		}
	}
	if expected, exists := rule["type"]; exists && !matchesJSONType(expected, value) {
		return fmt.Errorf("%s has JSON type %T, want %v", path, value, expected)
	}

	switch typed := value.(type) {
	case map[string]any:
		properties, _ := rule["properties"].(map[string]any)
		if minimum, ok := rule["minProperties"].(float64); ok && len(typed) < int(minimum) {
			return fmt.Errorf("%s has %d properties, minimum is %d", path, len(typed), int(minimum))
		}
		if required, ok := rule["required"].([]any); ok {
			for _, item := range required {
				name, _ := item.(string)
				if _, exists := typed[name]; !exists {
					return fmt.Errorf("%s is missing required property %q", path, name)
				}
			}
		}
		for name, child := range typed {
			if childRule, exists := properties[name]; exists {
				if err := validateSchemaValue(root, childRule, child, path+"."+name); err != nil {
					return err
				}
			} else if rule["additionalProperties"] == false {
				return fmt.Errorf("%s contains disallowed property %q", path, name)
			}
		}
	case []any:
		if minimum, ok := rule["minItems"].(float64); ok && len(typed) < int(minimum) {
			return fmt.Errorf("%s has %d items, minimum is %d", path, len(typed), int(minimum))
		}
		if itemRule, exists := rule["items"]; exists {
			for index, child := range typed {
				if err := validateSchemaValue(root, itemRule, child, fmt.Sprintf("%s[%d]", path, index)); err != nil {
					return err
				}
			}
		}
	case string:
		if minimum, ok := rule["minLength"].(float64); ok && len([]rune(typed)) < int(minimum) {
			return fmt.Errorf("%s is shorter than minLength %d", path, int(minimum))
		}
		if pattern, ok := rule["pattern"].(string); ok {
			matched, err := regexp.MatchString(pattern, typed)
			if err != nil || !matched {
				return fmt.Errorf("%s value %q does not match %q", path, typed, pattern)
			}
		}
	case float64:
		if minimum, ok := rule["minimum"].(float64); ok && typed < minimum {
			return fmt.Errorf("%s value %v is below minimum %v", path, typed, minimum)
		}
		if maximum, ok := rule["maximum"].(float64); ok && typed > maximum {
			return fmt.Errorf("%s value %v is above maximum %v", path, typed, maximum)
		}
	}
	return nil
}

func matchesJSONType(expected, value any) bool {
	if alternatives, ok := expected.([]any); ok {
		for _, alternative := range alternatives {
			if matchesJSONType(alternative, value) {
				return true
			}
		}
		return false
	}
	name, _ := expected.(string)
	switch name {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "integer":
		number, ok := value.(float64)
		return ok && math.Trunc(number) == number
	case "number":
		_, ok := value.(float64)
		return ok
	case "null":
		return value == nil
	default:
		return true
	}
}

func resolveLocalReference(root any, reference string) (any, error) {
	if !strings.HasPrefix(reference, "#/") {
		return nil, fmt.Errorf("unsupported non-local schema reference %q", reference)
	}
	current := root
	for _, segment := range strings.Split(strings.TrimPrefix(reference, "#/"), "/") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid schema reference %q", reference)
		}
		current, ok = object[strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")]
		if !ok {
			return nil, fmt.Errorf("unresolved schema reference %q", reference)
		}
	}
	return current, nil
}

func TestGUICompleteReportSchema(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node required for GUI export")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	for _, sources := range [][]compiler.SourceUnit{{{ID: "main", Name: "main.cl", Content: `class Main { main():Object { 0 }; };`}}, {{ID: "helper", Name: "helper.cl", Content: `class Helper {};`}, {ID: "main", Name: "main.cl", Content: `class Main inherits Helper { main():Object { 0 }; };`}}} {
		report := compiler.NewProjectCompilationSession(sources, compiler.ResultContract{Options: compiler.CompilationOptions{WarningRules: "none"}}).Complete().Result
		payload := mustJSON(t, map[string]any{"report": report, "tabs": sources})
		script := `(async()=>{const fs=require('fs');const client=require('./internal/web/static/client-utils.js');const {reportSources}=await import('./internal/web/static/report-sources.mjs');const {report,tabs}=JSON.parse(fs.readFileSync(0,'utf8'));process.stdout.write(JSON.stringify({...client.buildCompileReport(report,'main.cl','2026-09-06T00:00:00Z'),...reportSources(report,tabs.map(t=>({...t,source:t.content})))}));})().catch(e=>{console.error(e);process.exit(1)});`
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, "node", "-e", script)
		cmd.Dir = root
		cmd.Stdin = strings.NewReader(string(payload))
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		data, err := cmd.Output()
		cancel()
		if err != nil {
			t.Fatalf("GUI export: %v %s", err, stderr.String())
		}
		validateSchemaDocument(t, filepath.Join(root, "schemas", "compile-report-v1.schema.json"), data)
		var exported struct {
			Sources []compiler.SourceUnit `json:"sources"`
		}
		if err := json.Unmarshal(data, &exported); err != nil {
			t.Fatal(err)
		}
		if string(mustJSON(t, exported.Sources)) != string(mustJSON(t, sources)) {
			t.Fatalf("GUI export did not preserve sources: %s", data)
		}
	}
}

func TestReportSchemaVersionedExtensions(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "schemas", "compile-report-v1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	base := compiler.CompileWithContract(`class Main { main():Object {0}; };`, "", compiler.ResultContract{Options: compiler.CompilationOptions{WarningRules: "none"}})
	for _, name := range []string{"old-warning", "old-sources", "missing-content", "unknown-source-field", "unknown-top-level", "legacy-valid"} {
		t.Run(name, func(t *testing.T) {
			var document map[string]any
			if err := json.Unmarshal(mustJSON(t, base), &document); err != nil {
				t.Fatal(err)
			}
			options := document["options"].(map[string]any)
			switch name {
			case "old-warning":
				document["schemaVersion"] = "1.4.0"
			case "old-sources":
				document["schemaVersion"] = "1.4.0"
				delete(options, "warningRules")
				document["sources"] = []any{map[string]any{"id": "a", "name": "a.cl", "content": ""}}
			case "missing-content":
				document["sources"] = []any{map[string]any{"id": "a", "name": "a.cl"}}
			case "unknown-source-field":
				document["sources"] = []any{map[string]any{"id": "a", "name": "a.cl", "content": "", "unexpected": true}}
			case "unknown-top-level":
				document["unexpected"] = true
			case "legacy-valid":
				document["schemaVersion"] = "1.4.0"
				delete(options, "warningRules")
			}
			err := validateSchemaValue(schema, schema, document, "$")
			if name == "legacy-valid" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("invalid report accepted")
			}
		})
	}
}
