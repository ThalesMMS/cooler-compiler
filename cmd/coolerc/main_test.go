// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/json"
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
	t.Cleanup(func() { file.Close() })
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

func TestConcatSourcesStartLines(t *testing.T) {
	t.Run("trailing newline files", func(t *testing.T) {
		got, files := concatSources([][]byte{
			[]byte("class A {\n};\n"),
			[]byte("class B {\n};\n"),
		}, []string{"a.cl", "b.cl"})
		want := "class A {\n};\nclass B {\n};\n"
		if got != want {
			t.Fatalf("joined %q, want %q", got, want)
		}
		if len(files) != 2 || files[0].name != "a.cl" || files[1].name != "b.cl" {
			t.Fatalf("files = %+v", files)
		}
		if files[0].startLine != 1 || files[1].startLine != 3 {
			t.Fatalf("startLine = %d, %d; want 1, 3", files[0].startLine, files[1].startLine)
		}
	})
	t.Run("missing final newline inserts separator", func(t *testing.T) {
		got, files := concatSources([][]byte{
			[]byte("class A {};"),
			[]byte("class B {};"),
		}, []string{"a.cl", "b.cl"})
		want := "class A {};\nclass B {};"
		if got != want {
			t.Fatalf("joined %q, want %q", got, want)
		}
		if len(files) != 2 || files[0].startLine != 1 || files[1].startLine != 2 {
			t.Fatalf("startLine = %d, %d; want 1, 2", files[0].startLine, files[1].startLine)
		}
	})
	t.Run("multi-line first chunk", func(t *testing.T) {
		got, files := concatSources([][]byte{
			[]byte("line1\nline2\nline3"),
			[]byte("line4"),
		}, []string{"first.cl", "second.cl"})
		want := "line1\nline2\nline3\nline4"
		if got != want {
			t.Fatalf("joined %q, want %q", got, want)
		}
		if len(files) != 2 || files[0].name != "first.cl" || files[1].name != "second.cl" {
			t.Fatalf("files = %+v", files)
		}
		if files[0].startLine != 1 || files[1].startLine != 4 {
			t.Fatalf("startLine = %d, %d; want 1, 4", files[0].startLine, files[1].startLine)
		}
	})
}
