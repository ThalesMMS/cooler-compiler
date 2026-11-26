// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"regexp"
	"strings"
	"testing"
)

const mipsLayoutProgram = `class A {
  a1 : Int;
  a2 : String <- "init";
  f() : Int { 1 };
  g() : Object { self };
};
class B inherits A {
  b1 : Bool;
  b2 : A;
  g() : Object { a1 };
  h(x : Int, y : Int) : Int { x + y };
};
class C inherits IO {};
class Main inherits IO {
  main() : Object { (new B).h(1, 2) };
};
`

func generateMIPSOK(t *testing.T, source string, options MIPSOptions) MIPSResult {
	t.Helper()
	result := GenerateMIPS([]SourceUnit{{ID: "u", Name: "t.cl", Content: source}}, options)
	if !result.OK {
		t.Fatalf("generation failed: %+v", result.Diagnostics)
	}
	return result
}

// labelBlock returns the lines after label up to the next label.
func labelBlock(assembly, label string) []string {
	lines := strings.Split(assembly, "\n")
	for index, line := range lines {
		if line == label+":" {
			var block []string
			for _, next := range lines[index+1:] {
				if strings.HasSuffix(next, ":") && !strings.HasPrefix(next, "\t") && !strings.HasPrefix(next, "label") {
					break
				}
				if strings.TrimSpace(next) == ".word\t-1" {
					break // the next object's eyecatcher
				}
				block = append(block, strings.TrimSpace(next))
			}
			return block
		}
	}
	return nil
}

func TestMIPSObjectLayoutAndPrototypes(t *testing.T) {
	result := generateMIPSOK(t, mipsLayoutProgram, MIPSOptions{})
	layouts := map[string]MIPSClassLayout{}
	for _, layout := range result.Classes {
		layouts[layout.Name] = layout
	}
	// Depth-first preorder tags: Object, IO, its subclasses, then the rest.
	order := []string{}
	for _, layout := range result.Classes {
		order = append(order, layout.Name)
	}
	if strings.Join(order, ",") != "Object,IO,C,Main,Int,Bool,String,A,B" {
		t.Fatalf("tag order %v", order)
	}
	if io := layouts["IO"]; io.Tag != 1 || io.MaxTag != 3 {
		t.Fatalf("IO tag range %+v", io)
	}
	b := layouts["B"]
	if b.SizeWords != 7 {
		t.Fatalf("B size %d", b.SizeWords)
	}
	wantOffsets := map[string]int{"a1": 12, "a2": 16, "b1": 20, "b2": 24}
	for _, attr := range b.Attributes {
		if wantOffsets[attr.Name] != attr.Offset {
			t.Fatalf("B attribute %s at %d", attr.Name, attr.Offset)
		}
	}
	assembly := result.Assembly
	for _, class := range []string{"Object", "IO", "Int", "Bool", "String", "A", "B", "C", "Main"} {
		if !strings.Contains(assembly, "\t.word\t-1\n"+class+"_protObj:\n") {
			t.Fatalf("%s prototype lacks the -1 eyecatcher", class)
		}
	}
	proto := labelBlock(assembly, "B_protObj")
	want := []string{".word\t8", ".word\t7", ".word\tB_dispTab", ".word\tint_const0", ".word\tstr_const", ".word\tbool_const0", ".word\t0"}
	for index, prefix := range want {
		if !strings.HasPrefix(proto[index], prefix) {
			t.Fatalf("B_protObj word %d = %q, want prefix %q\n%v", index, proto[index], prefix, proto)
		}
	}
	if s := labelBlock(assembly, "String_protObj"); s[0] != ".word\t6" || s[1] != ".word\t5" || s[2] != ".word\tString_dispTab" || s[3] != ".word\tint_const0" {
		t.Fatalf("String_protObj %v", s)
	}
	if v := labelBlock(assembly, "_string_tag"); v[0] != ".word\t6" {
		t.Fatalf("_string_tag %v", v)
	}
}

func TestMIPSTablesAndConstants(t *testing.T) {
	assembly := generateMIPSOK(t, mipsLayoutProgram, MIPSOptions{}).Assembly
	names := labelBlock(assembly, "class_nameTab")
	if len(names) != 10 { // 9 classes + .globl line of the next table
		t.Fatalf("class_nameTab %v", names)
	}
	objTab := labelBlock(assembly, "class_objTab")
	if len(objTab) < 18 || objTab[0] != ".word\tObject_protObj" || objTab[1] != ".word\tObject_init" || objTab[16] != ".word\tB_protObj" || objTab[17] != ".word\tB_init" {
		t.Fatalf("class_objTab %v", objTab)
	}
	a := labelBlock(assembly, "A_dispTab")
	b := labelBlock(assembly, "B_dispTab")
	wantA := []string{".word\tObject.abort", ".word\tObject.type_name", ".word\tObject.copy", ".word\tA.f", ".word\tA.g"}
	wantB := []string{".word\tObject.abort", ".word\tObject.type_name", ".word\tObject.copy", ".word\tA.f", ".word\tB.g", ".word\tB.h"}
	if strings.Join(a, "|") != strings.Join(wantA, "|") || strings.Join(b, "|") != strings.Join(wantB, "|") {
		t.Fatalf("dispatch tables\nA=%v\nB=%v", a, b)
	}
	if io := labelBlock(assembly, "IO_dispTab"); len(io) != 7 || io[3] != ".word\tIO.out_string" || io[6] != ".word\tIO.in_int" {
		t.Fatalf("IO_dispTab %v", io)
	}
	if s := labelBlock(assembly, "String_dispTab"); len(s) != 6 || s[5] != ".word\tString.substr" {
		t.Fatalf("String_dispTab %v", s)
	}
	// String constants carry their length Int and padded bytes.
	initString := regexp.MustCompile(`(?m)^(str_const\d+):\n\t\.word\t6\n\t\.word\t6\n\t\.word\tString_dispTab\n\t\.word\t(int_const\d+)\n\t\.ascii\t"init"\n\t\.byte\t0\n\t\.align\t2`)
	match := initString.FindStringSubmatch(assembly)
	if match == nil {
		t.Fatal("missing the \"init\" string constant")
	}
	if four := labelBlock(assembly, match[2]); four[3] != ".word\t4" {
		t.Fatalf("length constant %v", four)
	}
	for _, label := range []string{"bool_const0", "bool_const1", "heap_start", "_MemMgr_INITIALIZER", "_MemMgr_COLLECTOR", "_MemMgr_TEST"} {
		if labelBlock(assembly, label) == nil {
			t.Fatalf("missing %s", label)
		}
	}
	if v := labelBlock(assembly, "_MemMgr_INITIALIZER"); v[0] != ".word\t_NoGC_Init" {
		t.Fatalf("default collector %v", v)
	}
}

func TestMIPSInitMethodsAndCallingConvention(t *testing.T) {
	assembly := generateMIPSOK(t, mipsLayoutProgram, MIPSOptions{}).Assembly
	prologue := []string{"addiu\t$sp $sp -12", "sw\t$fp 12($sp)", "sw\t$s0 8($sp)", "sw\t$ra 4($sp)", "addiu\t$fp $sp 4", "move\t$s0 $a0"}
	initB := labelBlock(assembly, "B_init")
	if strings.Join(initB[:6], "|") != strings.Join(prologue, "|") || initB[6] != "jal\tA_init" {
		t.Fatalf("B_init must save the frame and run A_init first: %v", initB)
	}
	initA := strings.Join(labelBlock(assembly, "A_init"), "\n")
	if !strings.Contains(initA, "jal\tObject_init") || !regexp.MustCompile(`la\t\$a0 str_const\d+\n\s*sw\t\$a0 16\(\$s0\)`).MatchString(initA) {
		t.Fatalf("A_init must store the a2 initializer at offset 16:\n%s", initA)
	}
	if strings.Contains(initA, "12($s0)") {
		t.Fatal("A_init must not store a1, which has no initializer")
	}
	// h(x, y): x is at fp+16 and y at fp+12; the callee pops both arguments.
	h := strings.Join(labelBlock(assembly, "B.h"), "\n")
	for _, want := range []string{"lw\t$a0 16($fp)", "lw\t$a0 12($fp)", "addiu\t$sp $sp 20\n\tjr\t$ra"} {
		if !strings.Contains(h, strings.ReplaceAll(want, "\n\t", "\n")) {
			t.Fatalf("B.h lacks %q:\n%s", want, h)
		}
	}
	main := strings.Join(labelBlock(assembly, "Main.main"), "\n")
	for _, want := range []string{"la\t$a0 B_protObj", "jal\tObject.copy", "jal\tB_init", "lw\t$t1 8($a0)", "lw\t$t1 20($t1)", "jalr\t$t1"} {
		if !strings.Contains(main, want) {
			t.Fatalf("Main.main lacks %q:\n%s", want, main)
		}
	}
}

func TestMIPSExpressionsAndAborts(t *testing.T) {
	source := `class Main inherits IO {
  x : Int;
  main() : Object { {
    x <- 1 + 2 - 3 * 4 / 5;
    x <- ~x;
    if x < 2 then x <= 3 else not (x = 4) fi;
    while isvoid self loop 0 pool;
    let a : Int, s : String <- "s", b : Main in a;
    case self of m : Main => m; o : Object => o; esac;
    new SELF_TYPE;
    self@IO.out_int(x);
    out_string(type_name().concat("!").substr(0, 1));
    true = false;
  } };
};
`
	assembly := generateMIPSOK(t, source, MIPSOptions{})
	text := assembly.Assembly
	for _, want := range []string{
		"add\t$t1 $t1 $t2", "sub\t$t1 $t1 $t2", "mul\t$t1 $t1 $t2", "div\t$t1 $t1 $t2", "neg\t$t1 $t1",
		"blt\t$t1 $t2", "ble\t$t1 $t2", "jal\tequality_test", "la\t$a1 bool_const0",
		"jal\t_dispatch_abort", "jal\t_case_abort2", "jal\t_case_abort",
		"la\t$t1 class_objTab", "sll\t$t2 $t2 3", "la\t$t1 IO_dispTab",
		"sw\t$a0 12($s0)", "beqz\t$t1",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("assembly lacks %q", want)
		}
	}
	// The abort helpers receive the file name and line.
	if !regexp.MustCompile(`la\t\$a0 str_const\d+\n\tli\t\$t1 11\n\tjal\t_dispatch_abort`).MatchString(text) {
		t.Fatal("dispatch on line 11 must pass the file name and line to _dispatch_abort")
	}
	if !regexp.MustCompile(`li\t\$t1 9\n\tjal\t_case_abort2`).MatchString(text) {
		t.Fatal("case on line 9 must pass its line to _case_abort2")
	}
	// The most specific case branch (Main) is tested before Object.
	mainTest := strings.Index(text, "blt\t$t1 2 ")
	objectTest := strings.Index(text, "blt\t$t1 0 ")
	if mainTest < 0 || objectTest < 0 || mainTest > objectTest {
		t.Fatalf("case branch order: Main at %d, Object at %d", mainTest, objectTest)
	}
	// Every method label used by a dispatch table is defined.
	defined := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasSuffix(line, ":") {
			defined[strings.TrimSuffix(line, ":")] = true
		}
	}
	runtime := map[string]bool{"Object.abort": true, "Object.type_name": true, "Object.copy": true, "IO.out_string": true, "IO.out_int": true, "IO.in_string": true, "IO.in_int": true, "String.length": true, "String.concat": true, "String.substr": true}
	for _, match := range regexp.MustCompile(`\.word\t([A-Za-z_]\w*\.\w+)`).FindAllStringSubmatch(text, -1) {
		if !defined[match[1]] && !runtime[match[1]] {
			t.Fatalf("dispatch table entry %s has no definition", match[1])
		}
	}
}

func TestMIPSGCHooksAndDiagnostics(t *testing.T) {
	source := "class Main { x : Int <- 1; main() : Object { x <- 2 }; };"
	plain := generateMIPSOK(t, source, MIPSOptions{}).Assembly
	if strings.Contains(plain, "_GenGC_Assign") || strings.Contains(plain, "_gc_check") {
		t.Fatal("default output must not call collector hooks")
	}
	gc := generateMIPSOK(t, source, MIPSOptions{GC: MIPSGCOptions{Generational: true, Test: true, Debug: true}}).Assembly
	if strings.Count(gc, "jal\t_GenGC_Assign") != 2 || strings.Count(gc, "jal\t_gc_check") != 2 {
		t.Fatalf("expected GenGC_Assign and _gc_check for both attribute stores:\n%s", gc)
	}
	if v := labelBlock(gc, "_MemMgr_INITIALIZER"); v[0] != ".word\t_GenGC_Init" {
		t.Fatalf("collector %v", v)
	}
	if v := labelBlock(gc, "_MemMgr_TEST"); v[0] != ".word\t1" {
		t.Fatalf("test flag %v", v)
	}
	bad := GenerateMIPS([]SourceUnit{{ID: "u", Name: "bad.cl", Content: "class Main { main() : Int { x }; };"}}, MIPSOptions{})
	if bad.OK || bad.Assembly != "" || !hasErrors(bad.Diagnostics) {
		t.Fatalf("invalid programs must produce diagnostics only: %+v", bad)
	}
}
