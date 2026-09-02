// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ThalesMMS/cooler-compiler/compiler"
)

func ExampleCompile() {
	result, err := compiler.Compile(context.Background(), `
class Main inherits IO {
  main() : Object { out_string("public API\n") };
};`, compiler.DefaultOptions())
	if err != nil {
		panic(err)
	}
	fmt.Print(strings.Join(result.Runtime().Output, ""))
	// Output: public API
}

func ExampleCompileProject() {
	project := compiler.Project{Sources: []compiler.SourceUnit{
		{ID: "answer", Name: "answer.cl", Content: `class Answer { value() : Int { 42 }; };`},
		{ID: "main", Name: "main.cl", Content: `class Main inherits IO { main() : Object { out_int((new Answer).value()) }; };`},
	}}
	result, err := compiler.CompileProject(context.Background(), project, compiler.DefaultOptions())
	if err != nil {
		panic(err)
	}
	fmt.Print(strings.Join(result.Runtime().Output, ""))
	// Output: 42
}

func ExampleCompile_checkOnly() {
	options := compiler.DefaultOptions()
	options.Mode = compiler.ModeCheck
	result, err := compiler.Compile(context.Background(), `class Main { main() : Int { 1 + 2 }; };`, options)
	if err != nil {
		panic(err)
	}
	fmt.Println(result.HasErrors(), result.Runtime().ExecutedSteps)
	// Output: false 0
}

func ExampleNewSession() {
	project := compiler.Project{Sources: []compiler.SourceUnit{{
		ID: "main", Name: "main.cl", Content: `class Main { main() : Int { 1 + 2 }; };`,
	}}}
	session, err := compiler.NewSession(context.Background(), project, compiler.DefaultOptions())
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := session.Close(); err != nil {
			panic(err)
		}
	}()
	lexer, err := session.Next(context.Background())
	if err != nil {
		panic(err)
	}
	completed, err := session.Complete(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Println(lexer.CurrentPhase, completed.CurrentPhase, completed.Complete)
	// Output: lexer runtime true
}

func ExampleNewDebugSession() {
	project := compiler.Project{Sources: []compiler.SourceUnit{{
		ID: "main", Name: "main.cl", Content: `class Main { main() : Int { 1 + 2 }; };`,
	}}}
	debugger, err := compiler.NewDebugSession(context.Background(), project, compiler.DefaultOptions())
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := debugger.Close(); err != nil {
			panic(err)
		}
	}()
	snapshot, err := debugger.StepInstruction(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Println(snapshot.Runtime.ExecutedSteps)
	// Output: 1
}

func ExampleCompile_diagnostics() {
	result, err := compiler.Compile(context.Background(), `class Main { main() : Int { missing }; };`, compiler.DefaultOptions())
	if err != nil {
		panic(err)
	}
	fmt.Println(result.Diagnostics()[0].Code)
	// Output: COL3100
}

func ExampleResult_jsonAndIR() {
	result, err := compiler.Compile(context.Background(), `class Main { main() : Int { 1 + 2 }; };`, compiler.DefaultOptions())
	if err != nil {
		panic(err)
	}
	reportJSON, err := json.Marshal(result)
	if err != nil {
		panic(err)
	}
	fmt.Println(len(result.Tokens()) > 0, len(result.IR()) > 0, len(result.CFG().Blocks) > 0, len(result.Assembly()) > 0, json.Valid(reportJSON))
	// Output: true true true true true
}

func ExampleCompile_input() {
	options := compiler.DefaultOptions()
	options.Input = "supplied line\n"
	result, err := compiler.Compile(context.Background(), `class Main inherits IO { main() : Object { out_string(in_string()) }; };`, options)
	if err != nil {
		panic(err)
	}
	fmt.Println(strings.Join(result.Runtime().Output, ""))
	// Output: supplied line
}

func ExampleCompile_cancellation() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := compiler.Compile(ctx, `class Main { main() : Int { 0 }; };`, compiler.DefaultOptions())
	fmt.Println(errors.Is(err, context.Canceled))
	// Output: true
}

func ExampleDebugSession_Close() {
	project := compiler.Project{Sources: []compiler.SourceUnit{{
		ID: "main", Name: "main.cl", Content: `class Main { main() : Int { 0 }; };`,
	}}}
	debugger, err := compiler.NewDebugSession(context.Background(), project, compiler.DefaultOptions())
	if err != nil {
		panic(err)
	}
	if err := debugger.Close(); err != nil {
		panic(err)
	}
	_, err = debugger.Reset(context.Background())
	fmt.Println(errors.Is(err, compiler.ErrClosed))
	// Output: true
}

func ExampleDebugSession_heapWatchesAndReverse() {
	project := compiler.Project{Sources: []compiler.SourceUnit{{
		ID: "main", Name: "main.cl", Content: `class Main { main() : Int { 1 + 2 }; };`,
	}}}
	debugger, err := compiler.NewDebugSession(context.Background(), project, compiler.DefaultOptions())
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := debugger.Close(); err != nil {
			panic(err)
		}
	}()
	if err := debugger.SetWatches(context.Background(), []compiler.WatchExpression{{ID: "sum", Expression: "1 + 2"}}); err != nil {
		panic(err)
	}
	forward, err := debugger.StepInstruction(context.Background())
	if err != nil {
		panic(err)
	}
	heap, err := debugger.Heap(context.Background(), compiler.HeapQuery{Mode: "summary"})
	if err != nil {
		panic(err)
	}
	reversed, err := debugger.StepBack(context.Background(), compiler.ReverseInstruction)
	if err != nil {
		panic(err)
	}
	fmt.Println(len(forward.Watches), heap.Mode, reversed.StopReason)
	// Output: 1 summary reverse-step
}

func ExampleNormalizeOptions_limits() {
	options := compiler.DefaultOptions()
	options.MaxSteps = -1
	_, err := compiler.NormalizeOptions(options)
	fmt.Println(errors.Is(err, compiler.ErrInvalidOptions))
	// Output: true
}
