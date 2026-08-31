// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"strings"
	"testing"
)

const helloProgram = `class Main inherits IO {
  fact(n : Int) : Int {
    if n <= 1 then 1 else n * fact(n - 1) fi
  };

  main() : Object {
    {
      out_string("Hello, COOL!\n");
      out_int(fact(5));
      out_string("\n");
    }
  };
};
`

func TestCompileAndRunFactorial(t *testing.T) {
	result := Compile(helloProgram)
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Severity == SeverityError {
			t.Fatalf("unexpected %s diagnostic %s: %s", diagnostic.Stage, diagnostic.Code, diagnostic.Message)
		}
	}
	if !result.Runtime.Halted {
		t.Fatal("expected VM to halt")
	}
	if got := outputText(result.Runtime.Output); got != "Hello, COOL!\n120\n" {
		t.Fatalf("unexpected output %q", got)
	}
	if result.Metrics.Classes != 1 || result.Metrics.Methods != 2 {
		t.Fatalf("unexpected metrics: %+v", result.Metrics)
	}
	if len(result.IR) == 0 || len(result.Assembly) == 0 {
		t.Fatal("expected generated IR and assembly")
	}
}

func TestInheritanceDispatchAndAttributes(t *testing.T) {
	source := `class Counter {
  value : Int <- 2;
  inc() : SELF_TYPE { { value <- value + 1; self; } };
  get() : Int { value };
};
class FancyCounter inherits Counter {
  inc() : SELF_TYPE { { value <- value + 2; self; } };
};
class Main inherits IO {
  main() : Object {
    let c : Counter <- new FancyCounter in {
      c.inc();
      out_int(c.get());
    }
  };
};`
	result := Compile(source)
	assertNoErrors(t, result.Diagnostics)
	if got := outputText(result.Runtime.Output); got != "4" {
		t.Fatalf("unexpected output %q", got)
	}
}

func TestCaseSelectsMostSpecificBranch(t *testing.T) {
	source := `class A {};
class B inherits A {};
class Main inherits IO {
  main() : Object {
    case new B of
      a : A => out_string("A");
      b : B => out_string("B");
      o : Object => out_string("Object");
    esac
  };
};`
	result := Compile(source)
	assertNoErrors(t, result.Diagnostics)
	if got := outputText(result.Runtime.Output); got != "B" {
		t.Fatalf("unexpected output %q", got)
	}
}

func TestSemanticDiagnosticsBlockExecution(t *testing.T) {
	source := `class Main {
  main(x : Int) : Int { "wrong" };
};`
	result := Compile(source)
	if !hasErrors(result.Diagnostics) {
		t.Fatal("expected semantic errors")
	}
	if len(result.IR) != 0 {
		t.Fatal("IR should be blocked by semantic errors")
	}
	if !result.Runtime.Halted {
		t.Fatal("blocked runtime should be halted")
	}
	messages := []string{}
	for _, diagnostic := range result.Diagnostics {
		messages = append(messages, diagnostic.Message)
	}
	joined := strings.Join(messages, "\n")
	if !strings.Contains(joined, "method Main.main must not declare formal parameters") {
		t.Fatalf("missing Main.main diagnostic: %s", joined)
	}
	if !strings.Contains(joined, "does not conform") {
		t.Fatalf("missing return conformance diagnostic: %s", joined)
	}
}

func TestNestedCommentsAndStringEscapes(t *testing.T) {
	source := `(* outer (* nested *) comment *)
class Main inherits IO {
  main() : Object { out_string("a\nb") };
};`
	result := Compile(source)
	assertNoErrors(t, result.Diagnostics)
	if got := outputText(result.Runtime.Output); got != "a\nb" {
		t.Fatalf("unexpected output %q", got)
	}
}

func TestDebuggerStepRunReset(t *testing.T) {
	session := NewDebugSession(helloProgram)
	initial := session.Snapshot(false, 0)
	if initial.Status != "ready" || initial.Runtime.ExecutedSteps != 0 {
		t.Fatalf("unexpected initial snapshot: %+v", initial)
	}
	stepped := session.Step()
	if stepped.Runtime.ExecutedSteps != 1 {
		t.Fatalf("expected one executed instruction, got %d", stepped.Runtime.ExecutedSteps)
	}
	finished := session.Run(defaultVMMaxSteps, nil)
	if !finished.Runtime.Halted {
		t.Fatal("expected debug run to halt")
	}
	reset := session.Reset()
	if reset.Runtime.ExecutedSteps != 0 || reset.Runtime.Halted {
		t.Fatalf("unexpected reset state: %+v", reset.Runtime)
	}
}

func assertNoErrors(t *testing.T, diagnostics []Diagnostic) {
	t.Helper()
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == SeverityError {
			t.Fatalf("unexpected %s diagnostic %s at %d:%d: %s", diagnostic.Stage, diagnostic.Code, diagnostic.Line, diagnostic.Column, diagnostic.Message)
		}
	}
}

func TestOperatorPrecedence(t *testing.T) {
	source := `class Main inherits IO {
  main() : Object {
    {
      out_int(1 + 2 * 3);
      if not 1 < 2 then out_string("bad") else out_string(" ok") fi;
    }
  };
};`
	result := Compile(source)
	assertNoErrors(t, result.Diagnostics)
	if got := outputText(result.Runtime.Output); got != "7 ok" {
		t.Fatalf("unexpected precedence result %q", got)
	}
}

func TestStaticDispatchAndNewSelfType(t *testing.T) {
	source := `class Parent {
  label() : String { "parent" };
  fresh() : SELF_TYPE { new SELF_TYPE };
};
class Child inherits Parent {
  label() : String { "child" };
  parent_label() : String { self@Parent.label() };
};
class Main inherits IO {
  main() : Object {
    let child : Child <- new Child in {
      out_string(child.label());
      out_string("/");
      out_string(child.parent_label());
      out_string("/");
      out_string(child.fresh().type_name());
    }
  };
};`
	result := Compile(source)
	assertNoErrors(t, result.Diagnostics)
	if got := outputText(result.Runtime.Output); got != "child/parent/Child" {
		t.Fatalf("unexpected dispatch result %q", got)
	}
}

func TestRuntimeDiagnostics(t *testing.T) {
	tests := []struct {
		name   string
		source string
		code   string
	}{
		{
			name: "division by zero",
			source: `class Main inherits IO {
  main() : Object { out_int(1 / 0) };
};`,
			code: "COL6015",
		},
		{
			name: "dispatch on void",
			source: `class Value { get() : Int { 1 }; };
class Main inherits IO {
  main() : Object { let value : Value in out_int(value.get()) };
};`,
			code: "COL6020",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := Compile(test.source)
			if !result.Runtime.Halted {
				t.Fatal("expected VM to halt after a runtime error")
			}
			found := false
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Stage == "runtime" && diagnostic.Code == test.code {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("missing runtime diagnostic %s: %+v", test.code, result.Diagnostics)
			}
		})
	}
}

func TestDebuggerBreakpointResumeAndIncrementalTrace(t *testing.T) {
	session := NewDebugSession(helloProgram)
	paused := session.Run(defaultVMMaxSteps, []int{8})
	if paused.Status != "paused" || !paused.Runtime.Paused || paused.Runtime.Halted {
		t.Fatalf("expected breakpoint pause, got status=%s runtime=%+v", paused.Status, paused.Runtime)
	}
	if len(paused.Runtime.Trace) == 0 || paused.Runtime.Trace[len(paused.Runtime.Trace)-1].Op != "BREAKPOINT" {
		t.Fatalf("expected breakpoint trace entry: %+v", paused.Runtime.Trace)
	}
	traceFrom := paused.Runtime.TraceOffset + len(paused.Runtime.Trace)
	finished := session.Run(defaultVMMaxSteps, nil)
	if !finished.Runtime.Halted {
		t.Fatal("expected resumed session to halt")
	}
	incremental := session.Snapshot(true, traceFrom)
	if incremental.Result.Source != "" || incremental.Result.AST != nil || incremental.Result.Tokens != nil {
		t.Fatal("light snapshot should omit static compilation artifacts")
	}
	if incremental.Runtime.TraceOffset < traceFrom {
		t.Fatalf("trace offset regressed: got %d, want >= %d", incremental.Runtime.TraceOffset, traceFrom)
	}
}

func TestSemanticDiagnosticOrderIsStable(t *testing.T) {
	source := `class Parent {
  z : Int;
  f(x : Int) : Int { x };
};
class Child inherits Parent {
  z : String;
  f(x : String) : String { x };
};
class Main { main() : Object { 0 }; };`
	var baseline string
	for iteration := 0; iteration < 20; iteration++ {
		result := Compile(source)
		parts := make([]string, 0, len(result.Diagnostics))
		for _, diagnostic := range result.Diagnostics {
			parts = append(parts, diagnostic.Code+":"+diagnostic.Message)
		}
		current := strings.Join(parts, "\n")
		if iteration == 0 {
			baseline = current
			continue
		}
		if current != baseline {
			t.Fatalf("diagnostic order changed on iteration %d\nfirst:\n%s\ncurrent:\n%s", iteration, baseline, current)
		}
	}
}
