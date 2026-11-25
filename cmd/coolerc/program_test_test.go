// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
)

func TestProgramTestCommand(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"echo.cl":     `class Main inherits IO {main():Object {out_string(in_string())};};`,
		"semantic.cl": `class Main {main():Object {missing};};`,
		"runtime.cl":  `class Main {main():Object {1/0};};`,
		"loop.cl":     `class Main {main():Object {while true loop 0 pool};};`,
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := `{"schemaVersion":"1.0.0","cases":[
	{"name":"echo","sources":["echo.cl"],"input":"one\n","expect":{"outcome":"success","output":"one"}},
	{"name":"isolated input","sources":["echo.cl"],"expect":{"outcome":"input-required","output":""}},
	{"name":"semantic","sources":["semantic.cl"],"expect":{"outcome":"error","output":"","diagnostics":[{"code":"COL3100","stage":"semantic"}]}},
	{"name":"runtime","sources":["runtime.cl"],"expect":{"outcome":"error","output":"","diagnostics":[{"code":"COL6015","stage":"runtime"}]}},
	{"name":"budget","sources":["loop.cl"],"limits":{"maxSteps":20},"expect":{"outcome":"step-limit","output":""}}
	]}`
	path := filepath.Join(dir, "exercise.json")
	if err := os.WriteFile(path, []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"test", "--json", path}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s %s", code, &stdout, &stderr)
	}
	var report programTestReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Passed != 5 || len(report.Cases) != 5 || report.Cases[1].Name != "isolated input" || report.Cases[4].ExecutedSteps != 20 {
		t.Fatalf("report %+v", report)
	}
	manifest = strings.Replace(manifest, `"output":"one"`, `"output":"one\n"`, 1)
	if err := os.WriteFile(path, []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"test", path}, nil, &stdout, &stderr); code != 1 || !strings.Contains(stdout.String(), `expected "one\n"`) || !strings.Contains(stdout.String(), "4 passed, 1 failed") {
		t.Fatalf("mismatch exit %d: %s %s", code, &stdout, &stderr)
	}
}

func TestProgramManifestRejectsInvalidInput(t *testing.T) {
	valid := `{"schemaVersion":"1.0.0","cases":[{"name":"one","sources":["main.cl"],"expect":{"outcome":"success","output":""}}]}`
	for _, input := range []string{
		`{}`, valid + `{}`, strings.Replace(valid, `"1.0.0"`, `"2.0.0"`, 1),
		strings.Replace(valid, `"name":"one"`, `"name":"one","typo":true`, 1),
		strings.Replace(valid, `"main.cl"`, `"../main.cl"`, 1),
		strings.Replace(valid, `"success"`, `"unknown"`, 1),
		strings.Replace(valid, `"expect":`, `"limits":{"maxSteps":-1},"expect":`, 1),
		strings.Replace(valid, `"output":""`, `"output":null`, 1),
	} {
		if _, err := parseProgramManifest(strings.NewReader(input)); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}

func TestProgramExerciseAndToolFailures(t *testing.T) {
	var out, log bytes.Buffer
	if code := run([]string{"test", "../../examples/exercise-tests/cases.json"}, nil, &out, &log); code != 0 || !strings.Contains(out.String(), "6 passed, 0 failed") {
		t.Fatalf("exercise exit %d: %s %s", code, &out, &log)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.json")
	manifest := `{"schemaVersion":"1.0.0","cases":[{"name":"missing file","sources":["missing.cl"],"expect":{"outcome":"success","output":""}}]}`
	if err := os.WriteFile(path, []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	log.Reset()
	if code := run([]string{"test", path}, nil, &out, &log); code != 3 || !strings.Contains(out.String(), "tool-error") {
		t.Fatalf("I/O failure exit %d: %s", code, &out)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out.Reset()
	log.Reset()
	if code := runProgramTests(ctx, []string{"../../examples/exercise-tests/cases.json"}, &out, &log); code != 3 || !strings.Contains(log.String(), "context canceled") {
		t.Fatalf("cancellation exit %d: %s", code, &out)
	}
	out.Reset()
	log.Reset()
	if code := run([]string{"test", "--help"}, nil, &out, &log); code != 0 || !strings.Contains(out.String(), "usage:") {
		t.Fatalf("help: %d %s", code, &out)
	}
}

func TestProgramCaseResourcePolicies(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.cl"), []byte(`class Main inherits IO {main():Object {out_string("long")};};`), 0600); err != nil {
		t.Fatal(err)
	}
	manifest, err := parseProgramManifest(strings.NewReader(`{"schemaVersion":"1.0.0","cases":[{"name":"output bound","sources":["main.cl"],"limits":{"maxOutputBytes":2},"expect":{"outcome":"error","output":"","diagnostics":[{"code":"COL6052","stage":"runtime"}]}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if result := executeProgramCase(context.Background(), dir, manifest.Cases[0]); !result.Passed {
		t.Fatalf("policy %+v", result)
	}
}

func TestProgramManifestReadError(t *testing.T) {
	failure := errors.New("read failed")
	_, err := parseProgramManifest(iotest.ErrReader(failure))
	var readFailure manifestReadError
	if !errors.As(err, &readFailure) || !errors.Is(err, failure) {
		t.Fatalf("lost I/O error classification: %v", err)
	}
	var out, log bytes.Buffer
	if code := run([]string{"test", t.TempDir()}, nil, &out, &log); code != 3 || !strings.Contains(log.String(), "regular file") {
		t.Fatalf("non-regular manifest: %d %s", code, &log)
	}
}
