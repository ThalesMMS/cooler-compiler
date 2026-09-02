// SPDX-License-Identifier: AGPL-3.0-or-later

// Package v1 pins the first stable public surface at compile time. Removing or
// incompatibly changing any use below requires a new API major version.
package v1_test

import (
	"context"
	"testing"

	"github.com/ThalesMMS/cooler-compiler/compiler"
)

func TestFirstStableSurfaceCompiles(t *testing.T) {
	ctx := context.Background()
	options := compiler.DefaultOptions()
	options.Mode = compiler.ModeCheck
	project := compiler.Project{Sources: []compiler.SourceUnit{{ID: "main", Name: "main.cl", Content: `class Main { main() : Int { 0 }; };`}}}

	result, err := compiler.CompileProject(ctx, project, options)
	if err != nil {
		t.Fatal(err)
	}
	var _ []compiler.Diagnostic = result.Diagnostics()
	var _ []compiler.Token = result.Tokens()
	var _ *compiler.ASTNode = result.AST()
	var _ []compiler.Symbol = result.Symbols()
	var _ []compiler.IRInstruction = result.IR()
	var _ compiler.ControlFlowGraph = result.CFG()
	var _ compiler.Metrics = result.Metrics()
	var _ compiler.RuntimeResult = result.Runtime()

	session, err := compiler.NewSession(ctx, project, compiler.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := session.Close(); err != nil {
			t.Errorf("close compilation: %v", err)
		}
	}()
	if _, err := session.Complete(ctx); err != nil {
		t.Fatal(err)
	}
	debugger, err := session.Debugger(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := debugger.Close(); err != nil {
			t.Errorf("close debugger: %v", err)
		}
	}()
	var _ compiler.Debugger = debugger
	var _ compiler.HeapInspector = debugger
	var _ compiler.WatchController = debugger
	var _ compiler.ReverseDebugger = debugger
}
