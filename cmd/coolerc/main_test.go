// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
)

const cliProgram = `class Main inherits IO {
  main() : Object { out_string("ok\n") };
};`

const echoInputProgram = `class Main inherits IO {
  main() : Object {
    {
      out_string(in_string());
      out_string("\n");
    }
  };
};`

const promptedInputProgram = `class Main inherits IO {
  main() : Object {
    {
      out_string("> ");
      out_string(in_string());
      out_string("\n");
    }
  };
};`

const interactiveIntProgram = `class Main inherits IO {
  main() : Object {
    {
      out_int(in_int());
      out_string("\n");
    }
  };
};`

const multiFileHelper = `class Util inherits IO { ping() : Object { out_string("pong\n") }; };`

const multiFileMain = `class Main inherits IO { main() : Object { (new Util).ping() }; };`

const multiFileEchoHelper = `class Util inherits IO {
  echo() : Object {
    {
      out_string(in_string());
      out_string("\n");
    }
  };
};`

const multiFileEchoMain = `class Main inherits IO { main() : Object { (new Util).echo() }; };`

func TestRunExecutesFileAndStdin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "program.cl")
	if err := os.WriteFile(path, []byte(cliProgram), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		args  []string
		stdin string
	}{
		{name: "file", args: []string{path}},
		{name: "stdin", stdin: cliProgram},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(test.args, strings.NewReader(test.stdin), &stdout, &stderr); code != exitOK {
				t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
			}
			if stdout.String() != "ok\n" || stderr.Len() != 0 {
				t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestCheckRejectsMalformedUTF8FromFileAndStdin(t *testing.T) {
	source := append([]byte("class Main { main() : Object { 0 }; };"), 0xff)
	path := filepath.Join(t.TempDir(), "malformed.cl")
	if err := os.WriteFile(path, source, 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		args  []string
		stdin string
	}{
		{name: "file", args: []string{"check", "--diagnostic-format", "json", path}},
		{name: "stdin", args: []string{"check", "--diagnostic-format", "json"}, stdin: string(source)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(test.args, strings.NewReader(test.stdin), &stdout, &stderr); code != exitError {
				t.Fatalf("exit code = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if strings.Count(stderr.String(), "COL1008") != 1 {
				t.Fatalf("malformed UTF-8 diagnostic = %q", stderr.String())
			}
		})
	}
}

func TestRunFeedsStdinAsProgramInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "echo.cl")
	if err := os.WriteFile(path, []byte(echoInputProgram), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{path}, strings.NewReader("hi\n"), &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if stdout.String() != "hi\n" || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestSourceFromStdinCannotAlsoBecomeProgramInput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, strings.NewReader(echoInputProgram), &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if stdout.String() != "\n" || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRedirectedStringInputPreservesEmbeddedNULWithoutLineDelimiter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "echo.cl")
	if err := os.WriteFile(path, []byte(echoInputProgram), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{path}, strings.NewReader("left\x00right\r\n"), &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if stdout.String() != "left\x00right\n" || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunInteractiveReadsOnlyAfterWritingPrompt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt.cl")
	if err := os.WriteFile(path, []byte(promptedInputProgram), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	stdin := &promptCheckingReader{source: strings.NewReader("hello\n"), stdout: &stdout}
	if code := run([]string{"run", "--interactive", path}, stdin, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if stdin.readBeforePrompt {
		t.Fatal("interactive stdin was read before the program prompt was written")
	}
	if stdout.String() != "> hello\n" || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunInteractiveUsesDeterministicEOFAndInvalidIntegerFallbacks(t *testing.T) {
	dir := t.TempDir()
	echoPath := filepath.Join(dir, "echo.cl")
	intPath := filepath.Join(dir, "int.cl")
	if err := os.WriteFile(echoPath, []byte(echoInputProgram), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(intPath, []byte(interactiveIntProgram), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, path, stdin, want string
	}{
		{name: "EOF becomes empty string", path: echoPath, want: "\n"},
		{name: "invalid integer becomes zero", path: intPath, stdin: "not-an-int\n", want: "0\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run([]string{"run", "--interactive", test.path}, strings.NewReader(test.stdin), &stdout, &stderr); code != exitOK {
				t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
			}
			if stdout.String() != test.want || stderr.Len() != 0 {
				t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunInteractiveRequiresFileAndRejectsInputFile(t *testing.T) {
	for _, args := range [][]string{
		{"run", "--interactive"},
		{"run", "--interactive", "--input", "input.txt", "program.cl"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, strings.NewReader(""), &stdout, &stderr); code != exitUsage {
			t.Fatalf("run(%v) exit code = %d, stderr = %q", args, code, stderr.String())
		}
		if !strings.Contains(stderr.String(), "--interactive") {
			t.Fatalf("run(%v) missing interactive diagnostic: %q", args, stderr.String())
		}
	}
}

type promptCheckingReader struct {
	source           io.Reader
	stdout           *bytes.Buffer
	readBeforePrompt bool
}

func (r *promptCheckingReader) Read(p []byte) (int, error) {
	if !strings.Contains(r.stdout.String(), "> ") {
		r.readBeforePrompt = true
	}
	return r.source.Read(p)
}

func TestRunCompilesMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	helper := filepath.Join(dir, "helper.cl")
	mainPath := filepath.Join(dir, "main.cl")
	if err := os.WriteFile(helper, []byte(multiFileHelper), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainPath, []byte(multiFileMain), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "helper then main", args: []string{helper, mainPath}},
		{name: "main then helper", args: []string{mainPath, helper}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(test.args, strings.NewReader(""), &stdout, &stderr); code != exitOK {
				t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
			}
			if stdout.String() != "pong\n" || stderr.Len() != 0 {
				t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunMultipleFilesFeedStdinAsProgramInput(t *testing.T) {
	dir := t.TempDir()
	helper := filepath.Join(dir, "helper.cl")
	mainPath := filepath.Join(dir, "main.cl")
	if err := os.WriteFile(helper, []byte(multiFileEchoHelper), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainPath, []byte(multiFileEchoMain), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{helper, mainPath}, strings.NewReader("hi\n"), &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if stdout.String() != "hi\n" || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestStdinIsCharDeviceFalseForPipedReaders(t *testing.T) {
	if stdinIsCharDevice(strings.NewReader("")) {
		t.Fatal("strings.NewReader should not be a char device")
	}
	if stdinIsCharDevice(&bytes.Buffer{}) {
		t.Fatal("bytes.Buffer should not be a char device")
	}
	path := filepath.Join(t.TempDir(), "stdin.txt")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := file.Close(); err != nil {
			t.Errorf("close input fixture: %v", err)
		}
	})
	if stdinIsCharDevice(file) {
		t.Fatal("regular file should not be a char device")
	}
}

func TestRunOutputModes(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := run([]string{"-json"}, strings.NewReader(cliProgram), &stdout, &stderr); code != exitOK {
			t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
		}
		var result compiler.Result
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
		}
		if result.Source != cliProgram || strings.Join(result.Runtime.Output, "") != "ok\n" {
			t.Fatalf("incomplete result: %+v", result)
		}
	})

	t.Run("ir", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := run([]string{"-ir"}, strings.NewReader(cliProgram), &stdout, &stderr); code != exitOK {
			t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
		}
		if !strings.Contains(stdout.String(), "Main.main") || !strings.Contains(stdout.String(), "CALL") {
			t.Fatalf("unexpected IR:\n%s", stdout.String())
		}
	})

	t.Run("assembly", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := run([]string{"-assembly"}, strings.NewReader(cliProgram), &stdout, &stderr); code != exitOK {
			t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
		}
		if !strings.Contains(stdout.String(), "Main.main:") || !strings.Contains(stdout.String(), "CALL") {
			t.Fatalf("unexpected assembly:\n%s", stdout.String())
		}
	})
}

func TestRunReportsDiagnosticsAndFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, strings.NewReader("class Main { main(): Object { missing }; };"), &stdout, &stderr)
	if code != exitError {
		t.Fatalf("exit code = %d, want %d", code, exitError)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "COL3100") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunRejectsInvalidArgumentsWithUsage(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "conflicting modes", args: []string{"-json", "-ir"}},
		{name: "unknown option", args: []string{"-unknown"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(test.args, strings.NewReader(""), &stdout, &stderr); code != exitUsage {
				t.Fatalf("exit code = %d, want %d", code, exitUsage)
			}
			if !strings.Contains(stderr.String(), "usage: coolerc [options] [file.cl ...]") || stdout.Len() != 0 {
				t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunHelpPrintsUsageAndSucceeds(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-h"}, strings.NewReader(""), &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code = %d, want %d", code, exitOK)
	}
	if !strings.Contains(stderr.String(), "usage: coolerc [options] [file.cl ...]") {
		t.Fatalf("usage missing: %q", stderr.String())
	}
}

func TestVersionCommandsDoNotReadSource(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"version"}, {"version", "--verbose"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, strings.NewReader("not COOL"), &stdout, &stderr); code != exitOK {
			t.Fatalf("%v exit=%d stderr=%q", args, code, stderr.String())
		}
		if !strings.Contains(stdout.String(), "coolerc ") || stderr.Len() != 0 {
			t.Fatalf("%v stdout=%q stderr=%q", args, stdout.String(), stderr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"version", "--json"}, strings.NewReader(""), &stdout, &stderr); code != exitOK {
		t.Fatal(code)
	}
	var info map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"version", "goVersion", "target"} {
		if info[field] == nil {
			t.Fatalf("missing version field %s: %s", field, stdout.String())
		}
	}
}

func TestCheckDoesNotExecuteVM(t *testing.T) {
	program := `class Main { main(): Object { 1 / 0 }; };`
	var checkedOut, checkedErr bytes.Buffer
	if code := run([]string{"check"}, strings.NewReader(program), &checkedOut, &checkedErr); code != exitOK {
		t.Fatalf("check exit=%d stderr=%q", code, checkedErr.String())
	}
	if checkedOut.Len() != 0 || !strings.Contains(checkedErr.String(), "COL3161") || strings.Contains(checkedErr.String(), "COL6003") {
		t.Fatalf("check output: %q %q", checkedOut.String(), checkedErr.String())
	}
	var runOut, runErr bytes.Buffer
	if code := run([]string{"run"}, strings.NewReader(program), &runOut, &runErr); code != exitError || !strings.Contains(runErr.String(), "division by zero") {
		t.Fatalf("run exit=%d stderr=%q", code, runErr.String())
	}
}

func TestEmitEveryArtifact(t *testing.T) {
	for _, artifact := range []string{"tokens", "ast", "symbols", "cfg", "report"} {
		t.Run(artifact, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run([]string{"emit", artifact}, strings.NewReader(cliProgram), &stdout, &stderr); code != exitOK {
				t.Fatalf("exit=%d stderr=%q", code, stderr.String())
			}
			var value any
			if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
				t.Fatalf("invalid %s JSON: %v", artifact, err)
			}
		})
	}
	for _, artifact := range []string{"ir", "assembly"} {
		var stdout, stderr bytes.Buffer
		if code := run([]string{"emit", artifact}, strings.NewReader(cliProgram), &stdout, &stderr); code != exitOK || !strings.Contains(stdout.String(), "Main.main") {
			t.Fatalf("%s exit=%d stdout=%q stderr=%q", artifact, code, stdout.String(), stderr.String())
		}
	}
}

func TestOptimizationCLISelectsAndExplainsIR(t *testing.T) {
	program := `class Main inherits IO { main() : Object { out_int((1 + 2) * 3) }; };`
	var stdout, stderr bytes.Buffer
	args := []string{"emit", "ir", "--opt=basic", "--emit-ir=both", "--explain-optimizations"}
	if code := run(args, strings.NewReader(program), &stdout, &stderr); code != exitOK {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	for _, text := range []string{";; unoptimized IR", ";; optimized IR", "origins", ";; pass constant-fold", "pure literal operation evaluated at compile time"} {
		if !strings.Contains(stdout.String(), text) {
			t.Fatalf("optimization output omitted %q:\n%s", text, stdout.String())
		}
	}
	if strings.Count(stdout.String(), "Main.main") < 2 {
		t.Fatalf("before/after IR was not emitted: %s", stdout.String())
	}

	for _, args := range [][]string{
		{"emit", "ir", "--opt=unsafe"},
		{"run", "--emit-ir=after"},
		{"run", "--emit-ir=both"},
		{"emit", "ir", "--emit-ir=sideways"},
	} {
		stdout.Reset()
		stderr.Reset()
		if code := run(args, strings.NewReader(program), &stdout, &stderr); code != exitUsage {
			t.Fatalf("invalid args %v exit=%d stderr=%q", args, code, stderr.String())
		}
	}
}

func TestReportExposesVersionedContractAndOrderedSourceUnits(t *testing.T) {
	directory := t.TempDir()
	first := filepath.Join(directory, "first.cl")
	second := filepath.Join(directory, "second.cl")
	if err := os.WriteFile(first, []byte("class Helper {};\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte(cliProgram), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"emit", "report", first, second}, strings.NewReader(""), &stdout, &stderr); code != exitOK {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	var result compiler.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != compiler.ReportSchemaVersion || result.CompilerVersion != version {
		t.Fatalf("missing report identity: %+v", result)
	}
	if len(result.SourceUnits) != 2 || result.SourceUnits[0].Name != "first.cl" || result.SourceUnits[1].Name != "second.cl" {
		t.Fatalf("unexpected source units: %+v", result.SourceUnits)
	}
	if result.Source != "" || result.SourceUnits[0].ID != "source-1" || result.SourceUnits[1].ID != "source-2" || result.SourceUnits[0].Order != 0 || result.SourceUnits[1].Order != 1 || len(result.SourceUnits[0].SHA256) != 64 {
		t.Fatalf("invalid source metadata: %+v", result.SourceUnits)
	}
}

func TestJSONDiagnosticsKeepCrossFileOrigins(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.cl")
	second := filepath.Join(dir, "second.cl")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("class Duplicate {};\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"check", "--diagnostic-format", "json", first, second}, strings.NewReader(""), &stdout, &stderr); code != exitError {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	var diagnostics []outputDiagnostic
	if err := json.Unmarshal(stderr.Bytes(), &diagnostics); err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Code != "COL3002" {
			continue
		}
		if diagnostic.File != second || diagnostic.Line != 1 || diagnostic.Span.SourceID != "source-2" {
			t.Fatalf("primary location = %+v", diagnostic)
		}
		if len(diagnostic.Related) != 1 || diagnostic.Related[0].File != first || diagnostic.Related[0].Line != 1 || diagnostic.Related[0].Span.SourceID != "source-1" {
			t.Fatalf("related location = %+v", diagnostic.Related)
		}
		return
	}
	t.Fatalf("missing duplicate diagnostic: %+v", diagnostics)
}

func TestExplicitInputFileWorksWithMultipleSources(t *testing.T) {
	dir := t.TempDir()
	helper, mainPath, inputPath := filepath.Join(dir, "helper.cl"), filepath.Join(dir, "main.cl"), filepath.Join(dir, "input.txt")
	for path, contents := range map[string]string{helper: multiFileEchoHelper, mainPath: multiFileEchoMain, inputPath: "from-file\n"} {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"run", "--input", inputPath, helper, mainPath}, strings.NewReader("ignored\n"), &stdout, &stderr); code != exitOK || stdout.String() != "from-file\n" {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestExplicitInputFileSeparatesProgramInputFromSourceStdin(t *testing.T) {
	inputPath := filepath.Join(t.TempDir(), "input.txt")
	if err := os.WriteFile(inputPath, []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"run", "--input", inputPath}, strings.NewReader(echoInputProgram), &stdout, &stderr); code != exitOK {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if stdout.String() != "from-file\n" || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestLegacyExecutionModesReadExplicitProgramInput(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "echo.cl")
	inputPath := filepath.Join(directory, "input.txt")
	if err := os.WriteFile(sourcePath, []byte(echoInputProgram), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inputPath, []byte("legacy-input\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-json", "--input", inputPath, sourcePath}, strings.NewReader("ignored\n"), &stdout, &stderr); code != exitOK {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	var result compiler.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(result.Runtime.Output, ""); got != "legacy-input\n" {
		t.Fatalf("runtime output = %q", got)
	}
}

func TestMaxStepsCanBeRaisedAboveDefault(t *testing.T) {
	options, _, err := parseCLI([]string{"run", "--max-steps", "100001"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if options.maxSteps != 100_001 {
		t.Fatalf("max steps = %d", options.maxSteps)
	}
}

func TestJSONDiagnosticsHaveStableMappedShape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.cl")
	if err := os.WriteFile(path, []byte(`class Main { main(): Object { missing }; };`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"check", "--diagnostic-format", "json", path}, strings.NewReader(""), &stdout, &stderr); code != exitError {
		t.Fatalf("exit=%d", code)
	}
	var diagnostics []map[string]any
	if err := json.Unmarshal(stderr.Bytes(), &diagnostics); err != nil {
		t.Fatalf("invalid diagnostics: %v\n%s", err, stderr.String())
	}
	for _, field := range []string{"file", "line", "column", "code", "stage", "severity", "message", "span"} {
		if diagnostics[0][field] == nil {
			t.Fatalf("missing %s: %s", field, stderr.String())
		}
	}
}

func TestNewCLIUsageAndToolExitCodes(t *testing.T) {
	for _, args := range [][]string{{"emit"}, {"emit", "unknown"}, {"run", "--max-steps", "0"}, {"check", "--input", "x"}, {"check", "--diagnostic-format", "xml"}, {"run", "--color", "sometimes"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, strings.NewReader(""), &stdout, &stderr); code != exitUsage {
			t.Fatalf("%v exit=%d stderr=%q", args, code, stderr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"run", filepath.Join(t.TempDir(), "missing.cl")}, strings.NewReader(""), &stdout, &stderr); code != exitTool {
		t.Fatalf("I/O exit=%d stderr=%q", code, stderr.String())
	}
}

func TestReadInputsPreservesNativeSourceUnits(t *testing.T) {
	directory := t.TempDir()
	first := filepath.Join(directory, "first.cl")
	second := filepath.Join(directory, "second.cl")
	firstSource := "class A {};"
	secondSource := "class B {};\r\n"
	if err := os.WriteFile(first, []byte(firstSource), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte(secondSource), 0o600); err != nil {
		t.Fatal(err)
	}
	name, joined, files, input, err := readInputs(cliOptions{command: "check", files: []string{first, second}}, strings.NewReader("ignored"))
	if err != nil {
		t.Fatal(err)
	}
	if name != first || len(joined) != 0 || input != "" {
		t.Fatalf("legacy aggregate leaked: name=%q source=%q input=%q", name, joined, input)
	}
	if len(files) != 2 || files[0].id != "source-1" || files[1].id != "source-2" {
		t.Fatalf("source identities = %+v", files)
	}
	if files[0].source != firstSource || files[1].source != secondSource {
		t.Fatalf("source contents changed: %+v", files)
	}
}

func TestLoadVMPolicyFileValidatesAndNormalizesOverrides(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "policy.json")
	if err := os.WriteFile(path, []byte(`{"maxInstructionsPerRun":2,"maxInstructionsTotal":3,"maxOutputBytes":16}`), 0o600); err != nil {
		t.Fatal(err)
	}
	options := cliOptions{maxSteps: 2, vmPolicyFile: path}
	if err := loadVMPolicy(&options); err != nil {
		t.Fatal(err)
	}
	if options.vmPolicy.MaxInstructionsPerRun != 2 || options.vmPolicy.MaxInstructionsTotal != 3 || options.vmPolicy.MaxOutputBytes != 16 || options.vmPolicy.MaxInputBytes != compiler.DefaultVMPolicy().MaxInputBytes {
		t.Fatalf("CLI policy was not normalized: %+v", options.vmPolicy)
	}

	if err := os.WriteFile(path, []byte(`{"unknownLimit":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := loadVMPolicy(&options); err == nil {
		t.Fatal("CLI accepted unknown policy field")
	}
	options = cliOptions{maxSteps: 3, vmPolicyFile: ""}
	policy := compiler.DefaultVMPolicy()
	policy.MaxInstructionsPerRun = 2
	data, _ := json.Marshal(policy)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	options.vmPolicyFile = path
	if err := loadVMPolicy(&options); err == nil || !strings.Contains(err.Error(), "--max-steps") {
		t.Fatalf("CLI accepted run budget above policy: %v", err)
	}
}
