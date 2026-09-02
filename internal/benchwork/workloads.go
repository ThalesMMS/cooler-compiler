// SPDX-License-Identifier: AGPL-3.0-or-later

// Package benchwork generates deterministic, bounded COOL performance inputs.
// It deliberately has no dependency on compiler internals so compiler, server,
// browser, and local benchmark tooling can share exactly the same workloads.
package benchwork

import (
	"fmt"
	"strings"
)

type Unit struct {
	ID      string
	Name    string
	Content string
}

type Workload struct {
	Name   string
	Source string
	Units  []Unit
	Input  string
}

type ProjectSize struct {
	Classes         int
	MethodsPerClass int
	ExpressionTerms int
	ClassesPerUnit  int
}

var standardProjects = []struct {
	name string
	size ProjectSize
}{
	{"small", ProjectSize{Classes: 2, MethodsPerClass: 2, ExpressionTerms: 4, ClassesPerUnit: 2}},
	{"medium", ProjectSize{Classes: 32, MethodsPerClass: 4, ExpressionTerms: 12, ClassesPerUnit: 8}},
	{"large", ProjectSize{Classes: 200, MethodsPerClass: 6, ExpressionTerms: 24, ClassesPerUnit: 20}},
}

func StandardProjects() []Workload {
	result := make([]Workload, 0, len(standardProjects))
	for _, definition := range standardProjects {
		result = append(result, Project(definition.name, definition.size))
	}
	return result
}

func Project(name string, size ProjectSize) Workload {
	if size.Classes < 1 {
		size.Classes = 1
	}
	if size.Classes > 500 {
		size.Classes = 500
	}
	if size.MethodsPerClass < 1 {
		size.MethodsPerClass = 1
	}
	if size.MethodsPerClass > 12 {
		size.MethodsPerClass = 12
	}
	if size.ExpressionTerms < 1 {
		size.ExpressionTerms = 1
	}
	if size.ExpressionTerms > 64 {
		size.ExpressionTerms = 64
	}
	if size.ClassesPerUnit < 1 {
		size.ClassesPerUnit = size.Classes
	}

	classes := make([]string, 0, size.Classes+1)
	for classIndex := 0; classIndex < size.Classes; classIndex++ {
		var source strings.Builder
		fmt.Fprintf(&source, "class BenchClass%03d {\n  value : Int <- %d;\n", classIndex, classIndex)
		for methodIndex := 0; methodIndex < size.MethodsPerClass; methodIndex++ {
			fmt.Fprintf(&source, "  method%02d(x : Int) : Int { x", methodIndex)
			for term := 0; term < size.ExpressionTerms; term++ {
				fmt.Fprintf(&source, " + %d", (classIndex+methodIndex+term)%17)
			}
			source.WriteString(" };\n")
		}
		source.WriteString("};\n")
		classes = append(classes, source.String())
	}
	classes = append(classes, fmt.Sprintf(`class Main inherits IO {
  main() : Object {
    { out_int((new BenchClass%03d).method00(1)); 0; }
  };
};
`, size.Classes-1))

	units := []Unit{}
	for start, order := 0, 0; start < len(classes); start, order = start+size.ClassesPerUnit, order+1 {
		end := start + size.ClassesPerUnit
		if end > len(classes) {
			end = len(classes)
		}
		content := strings.Join(classes[start:end], "\n")
		units = append(units, Unit{
			ID: fmt.Sprintf("bench-%s-%03d", name, order), Name: fmt.Sprintf("%s-%03d.cl", name, order), Content: content,
		})
	}
	var combined strings.Builder
	for _, unit := range units {
		combined.WriteString(unit.Content)
		combined.WriteByte('\n')
	}
	return Workload{Name: name, Source: combined.String(), Units: units}
}

func DeepInheritance(depth int) Workload {
	if depth < 1 {
		depth = 1
	}
	if depth > 400 {
		depth = 400
	}
	var source strings.Builder
	parent := "IO"
	for index := 0; index < depth; index++ {
		name := fmt.Sprintf("DeepClass%03d", index)
		fmt.Fprintf(&source, "class %s inherits %s { depth%03d() : Int { %d }; };\n", name, parent, index, index)
		parent = name
	}
	source.WriteString(deepInheritanceMain(parent))
	return Workload{Name: fmt.Sprintf("deep-%d", depth), Source: source.String()}
}

func deepInheritanceMain(parent string) string {
	return fmt.Sprintf("class Main inherits %s { main() : Object { self.type_name() }; };\n", parent)
}

func NestedExpression(depth int) Workload {
	if depth < 1 {
		depth = 1
	}
	if depth > 400 {
		depth = 400
	}
	expression := "1"
	for index := 0; index < depth; index++ {
		expression = "(" + expression + ")"
	}
	source := "class Main { main() : Object { " + expression + " }; };\n"
	return Workload{Name: fmt.Sprintf("nested-%d", depth), Source: source}
}

func LongText(blocks int) Workload {
	if blocks < 1 {
		blocks = 1
	}
	if blocks > 2_000 {
		blocks = 2_000
	}
	comment := "(*" + strings.Repeat(" benchmark-comment", 40) + "*)\n"
	var source strings.Builder
	for range blocks {
		source.WriteString(comment)
	}
	source.WriteString("class Main { main() : Object { \"")
	source.WriteString(strings.Repeat("x", 1_000))
	source.WriteString("\" }; };\n")
	return Workload{Name: fmt.Sprintf("long-text-%d", blocks), Source: source.String()}
}

func ArithmeticLoop(iterations int) Workload {
	if iterations < 1 {
		iterations = 1
	}
	if iterations > 100_000 {
		iterations = 100_000
	}
	source := fmt.Sprintf(`class Main {
  main() : Object {
    let i : Int <- 0, total : Int <- 0 in {
      while i < %d loop { total <- total + i; i <- i + 1; } pool;
      total;
    }
  };
};
`, iterations)
	return Workload{Name: fmt.Sprintf("arithmetic-%d", iterations), Source: source}
}

func RecursiveCalls(depth int) Workload {
	if depth < 1 {
		depth = 1
	}
	if depth > 200 {
		depth = 200
	}
	source := fmt.Sprintf(`class Main {
  recurse(n : Int) : Int { if n = 0 then 0 else recurse(n - 1) + 1 fi };
  main() : Object { recurse(%d) };
};
`, depth)
	return Workload{Name: fmt.Sprintf("recursion-%d", depth), Source: source}
}

func Dispatch(iterations int, static bool) Workload {
	if iterations < 1 {
		iterations = 1
	}
	if iterations > 50_000 {
		iterations = 50_000
	}
	dispatch := "worker.work(i)"
	name := "dynamic"
	if static {
		dispatch = "worker@DispatchBase.work(i)"
		name = "static"
	}
	source := fmt.Sprintf(`class DispatchBase { work(x : Int) : Int { x + 1 }; };
class DispatchLeaf inherits DispatchBase { };
class Main {
  main() : Object {
    let worker : DispatchBase <- new DispatchLeaf, i : Int <- 0, total : Int <- 0 in {
      while i < %d loop { total <- total + %s; i <- i + 1; } pool;
      total;
    }
  };
};
`, iterations, dispatch)
	return Workload{Name: fmt.Sprintf("dispatch-%s-%d", name, iterations), Source: source}
}

func AllocationLoop(iterations int) Workload {
	if iterations < 1 {
		iterations = 1
	}
	if iterations > 50_000 {
		iterations = 50_000
	}
	source := fmt.Sprintf(`class Allocated { value : Int <- 1; };
class Main {
  main() : Object {
    let i : Int <- 0 in {
      while i < %d loop { new Allocated; i <- i + 1; } pool;
      i;
    }
  };
};
`, iterations)
	return Workload{Name: fmt.Sprintf("allocation-%d", iterations), Source: source}
}

func DeepCase(depth int) Workload {
	if depth < 1 {
		depth = 1
	}
	if depth > 400 {
		depth = 400
	}
	workload := DeepInheritance(depth)
	leaf := fmt.Sprintf("DeepClass%03d", depth-1)
	workload.Name = fmt.Sprintf("case-%d", depth)
	mainClass := deepInheritanceMain(leaf)
	trimmed := strings.TrimSuffix(workload.Source, mainClass)
	if trimmed == workload.Source {
		panic("benchwork: DeepInheritance Main template drifted")
	}
	workload.Source = trimmed
	workload.Source += fmt.Sprintf(`class Main {
  main() : Object {
    case new %s of
      root : Object => 0;
      leaf : %s => 1;
    esac
  };
};
`, leaf, leaf)
	return workload
}

func StringOperations(repetitions int) Workload {
	if repetitions < 1 {
		repetitions = 1
	}
	if repetitions > 500 {
		repetitions = 500
	}
	var body strings.Builder
	body.WriteString("let value : String <- \"benchmark\" in {\n")
	for range repetitions {
		body.WriteString("value.length(); value.substr(0, 4); value.concat(\"!\");\n")
	}
	body.WriteString("value; }")
	return Workload{Name: fmt.Sprintf("strings-%d", repetitions), Source: "class Main { main() : Object { " + body.String() + " }; };\n"}
}

func IOOperations(lines int) Workload {
	if lines < 1 {
		lines = 1
	}
	if lines > 2_000 {
		lines = 2_000
	}
	var input strings.Builder
	for index := 0; index < lines; index++ {
		fmt.Fprintf(&input, "%d\n", index)
	}
	source := fmt.Sprintf(`class Main inherits IO {
  main() : Object {
    let i : Int <- 0 in {
      while i < %d loop { out_int(in_int()); i <- i + 1; } pool;
      i;
    }
  };
};
`, lines)
	return Workload{Name: fmt.Sprintf("io-%d", lines), Source: source, Input: input.String()}
}
