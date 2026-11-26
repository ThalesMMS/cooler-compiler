// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCourseFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runCourseCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, strings.NewReader(""), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestEmitTokensReferenceFormat(t *testing.T) {
	path := writeCourseFile(t, "hello.cl", "class Main {\n x : String <- \"a\nb\";\n};\n")
	code, stdout, stderr := runCourseCLI(t, "emit", "tokens", "--format=reference", path)
	if code != exitOK || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	want := "#name \"" + path + "\"\n#1 CLASS\n#1 TYPEID Main\n#1 '{'\n#2 OBJECTID x\n#2 ':'\n#2 TYPEID String\n#2 ASSIGN\n#3 ERROR \"Unterminated string constant\"\n#3 OBJECTID b\n#4 ERROR \"Unterminated string constant\"\n#4 '}'\n#4 ';'\n"
	if stdout != want {
		t.Fatalf("stdout:\n%s\nwant:\n%s", stdout, want)
	}
}

func TestEmitTokensDefaultStaysJSON(t *testing.T) {
	path := writeCourseFile(t, "a.cl", "class A {};")
	code, stdout, _ := runCourseCLI(t, "emit", "tokens", path)
	if code == exitUsage || !strings.HasPrefix(stdout, "[") {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
}

func TestCourseFlagValidation(t *testing.T) {
	path := writeCourseFile(t, "a.cl", "class A {};")
	for _, args := range [][]string{
		{"emit", "tokens", "--format=bogus", path},
		{"emit", "symbols", "--format=reference", path},
	} {
		if code, _, _ := runCourseCLI(t, args...); code != exitUsage {
			t.Fatalf("%v: code=%d, want usage error", args, code)
		}
	}
}

func TestEmitTypedASTAndReferenceDiagnostics(t *testing.T) {
	valid := writeCourseFile(t, "ok.cl", "class Main {\n main() : Int { 1 + 2 };\n};\n")
	code, stdout, stderr := runCourseCLI(t, "emit", "ast", "--format=reference", "--stage=semant", valid)
	if code != exitOK || stderr != "" || !strings.Contains(stdout, "_plus") || !strings.Contains(stdout, ": Int") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if code, stdout, stderr = runCourseCLI(t, "check", "--diagnostic-format=reference", valid); code != exitOK || stdout != "" || stderr != "" {
		t.Fatalf("check valid: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	invalid := writeCourseFile(t, "bad.cl", "class Main {\n main() : Int { x };\n};\n")
	// The erroneous expression gets type Object (the course recovery rule),
	// so the method body then fails to conform to Int.
	want := invalid + ":2: Undeclared identifier x.\n" + invalid + ":2: Inferred return type Object of method main does not conform to declared return type Int.\nCompilation halted due to static semantic errors.\n"
	for _, args := range [][]string{
		{"check", "--diagnostic-format=reference", invalid},
		{"emit", "ast", "--format=reference", "--stage=semant", invalid},
	} {
		code, stdout, stderr := runCourseCLI(t, args...)
		if code != exitError || stdout != "" || stderr != want {
			t.Fatalf("%v: code=%d stdout=%q stderr=%q", args, code, stdout, stderr)
		}
	}
	if code, _, _ := runCourseCLI(t, "run", "--diagnostic-format=reference", valid); code != exitUsage {
		t.Fatalf("run with reference diagnostics: code=%d", code)
	}
	if code, _, _ := runCourseCLI(t, "emit", "ast", "--stage=semant", valid); code != exitUsage {
		t.Fatalf("--stage without --format: code=%d", code)
	}
}

func TestFrontendFlag(t *testing.T) {
	path := writeCourseFile(t, "m.cl", "class Main inherits IO { main() : Object { out_string(\"ok\\n\") }; };")
	for _, frontend := range []string{"handwritten", "generated"} {
		if code, stdout, stderr := runCourseCLI(t, "run", "--frontend="+frontend, path); code != exitOK || stdout != "ok\n" {
			t.Fatalf("%s: code=%d stdout=%q stderr=%q", frontend, code, stdout, stderr)
		}
	}
	if code, _, _ := runCourseCLI(t, "run", "--frontend=flex", path); code != exitUsage {
		t.Fatalf("invalid frontend code=%d", code)
	}
	bad := writeCourseFile(t, "bad.cl", "class Main { main() : Int { 1 + }; };")
	code, _, stderr := runCourseCLI(t, "check", "--frontend=generated", bad)
	if code != exitError || !strings.Contains(stderr, "COL2200") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestEmitAssemblyTargetMIPS(t *testing.T) {
	path := writeCourseFile(t, "m.cl", "class Main inherits IO { main() : Object { out_string(\"hi\") }; };")
	code, stdout, stderr := runCourseCLI(t, "emit", "assembly", "--target=mips", path)
	if code != exitOK || stderr != "" || !strings.Contains(stdout, "Main.main:") || !strings.Contains(stdout, "_MemMgr_INITIALIZER") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	code, stdout, _ = runCourseCLI(t, "emit", "assembly", path)
	if code != exitOK || strings.Contains(stdout, "Main.main:\n\taddiu") {
		t.Fatalf("default assembly changed: code=%d", code)
	}
	bad := writeCourseFile(t, "bad.cl", "class Main { main() : Int { x }; };")
	if code, stdout, stderr := runCourseCLI(t, "emit", "assembly", "--target=mips", bad); code != exitError || stdout != "" || !strings.Contains(stderr, "COL3100") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if code, _, _ := runCourseCLI(t, "emit", "assembly", "--target=x86", path); code != exitUsage {
		t.Fatalf("bad target code=%d", code)
	}
}

func TestRunTargetMIPSUsesSimulator(t *testing.T) {
	path := writeCourseFile(t, "m.cl", "class Main inherits IO { main() : Object { out_int(6 * 7) }; };")
	code, stdout, stderr := runCourseCLI(t, "run", "--target=mips", path)
	if code != exitOK || stdout != "42COOL program successfully executed\n" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	void := writeCourseFile(t, "v.cl", "class Main { a : Main; main() : Object { a.main() }; };")
	code, stdout, stderr = runCourseCLI(t, "run", "--target=mips", void)
	if code != exitError || !strings.HasSuffix(stdout, "v.cl:1: Dispatch to void.\n") || !strings.Contains(stderr, "runtime-error") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestCollectorFlags(t *testing.T) {
	path := writeCourseFile(t, "m.cl", "class Main inherits IO { x : Int; main() : Object { { x <- 1; out_int(x); } }; };")
	code, stdout, _ := runCourseCLI(t, "emit", "assembly", "--target=mips", "-g", "-t", "-T", path)
	if code != exitOK || !strings.Contains(stdout, "_GenGC_Init") || !strings.Contains(stdout, "jal\t_GenGC_Assign") || !strings.Contains(stdout, "jal\t_gc_check") {
		t.Fatalf("code=%d", code)
	}
	if code, stdout, _ := runCourseCLI(t, "run", "--target=mips", "-g", "-t", path); code != exitOK || stdout != "1COOL program successfully executed\n" {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
	if code, _, _ := runCourseCLI(t, "run", "-g", path); code != exitUsage {
		t.Fatalf("-g without --target=mips: code=%d", code)
	}
}
