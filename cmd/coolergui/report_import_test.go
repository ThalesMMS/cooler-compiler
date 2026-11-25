// SPDX-License-Identifier: AGPL-3.0-or-later
package main

import (
	"context"
	"encoding/json"
	publiccompiler "github.com/ThalesMMS/cooler-compiler/compiler"
	"strings"
	"testing"
)

func TestReportImportValidation(t *testing.T) {
	result, err := publiccompiler.Compile(context.Background(), `class Main{main():Object{0};};`, publiccompiler.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	report, sources, err := decodeInspectionReport(data)
	if err != nil || len(sources) != 1 || len(report.IR) == 0 {
		t.Fatalf("roundtrip %v %+v", err, sources)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document["id"] = "never-reuse-session"
	document["token"] = "never-reuse-token"
	injected, _ := json.Marshal(document)
	decoded, _, err := decodeInspectionReport(injected)
	encoded, _ := json.Marshal(decoded)
	if err != nil || strings.Contains(string(encoded), "never-reuse") {
		t.Fatal("session fields survived")
	}
	document["schemaVersion"] = "9.0.0"
	invalid, _ := json.Marshal(document)
	for _, data := range [][]byte{invalid, []byte(`{}`), []byte(`{"schemaVersion":`), []byte(strings.Repeat(" ", maxRequestBodyBytes+1))} {
		if _, _, err := decodeInspectionReport(data); err == nil {
			t.Fatal("invalid report accepted")
		}
	}
}

func TestReportImportSourceHashes(t *testing.T) {
	sources := []publiccompiler.SourceUnit{{ID: "a", Name: "base.cl", Content: "class Base {};"}, {ID: "b", Name: "main.cl", Content: "class Main {main():Object {new Base};};"}}
	result, err := publiccompiler.CompileProject(context.Background(), publiccompiler.Project{Sources: sources}, publiccompiler.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(result)
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	_, missing, err := decodeInspectionReport(data)
	if err != nil || len(missing) != 0 {
		t.Fatal("legacy multi-source metadata must not invent content", err)
	}
	value["sources"] = sources
	data, _ = json.Marshal(value)
	_, imported, err := decodeInspectionReport(data)
	if err != nil || len(imported) != 2 {
		t.Fatal("source roundtrip", err)
	}
	for _, version := range []string{"1.0.0", "1.1.0", "1.2.0", "1.3.0", "1.4.0"} {
		value["schemaVersion"] = version
		data, _ = json.Marshal(value)
		if _, _, err := decodeInspectionReport(data); err == nil {
			t.Fatalf("sources accepted for schema %s", version)
		}
		delete(value, "sources")
		data, _ = json.Marshal(value)
		if _, _, err := decodeInspectionReport(data); err != nil {
			t.Fatalf("legacy report %s rejected: %v", version, err)
		}
		value["sources"] = sources
	}
	value["schemaVersion"] = "1.5.0"
	sources[1].Content = "changed"
	value["sources"] = sources
	data, _ = json.Marshal(value)
	if _, _, err := decodeInspectionReport(data); err == nil {
		t.Fatal("hash mismatch accepted")
	}
	if _, _, err := decodeInspectionReport([]byte(strings.Repeat("[", 70) + "0" + strings.Repeat("]", 70))); err == nil {
		t.Fatal("deep report accepted")
	}
}
