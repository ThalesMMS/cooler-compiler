// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	api "github.com/ThalesMMS/cooler-compiler/compiler"
	"github.com/ThalesMMS/cooler-compiler/examples"
)

const helloProgram = `class Main inherits IO { main() : Object { out_string("hello\n") }; };`

func TestPublicHistoryNavigation(t *testing.T) {
	ctx := context.Background()
	s, err := api.NewSession(ctx, api.Project{Sources: []api.SourceUnit{{ID: "main", Name: "main.cl", Content: helloProgram}}}, api.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := s.Complete(ctx); err != nil {
		t.Fatal(err)
	}
	d, err := s.Debugger(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	final, err := d.Run(ctx, 1000, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.SeekRetainedStep(ctx, 0, final.Timeline.Revision); err != nil {
		t.Fatal(err)
	}
	got, err := d.SeekRetainedStep(ctx, final.Timeline.LatestStep, final.Timeline.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if got.Timeline.StateHash != final.Timeline.StateHash || got.StopReason != api.StopHistorySeek {
		t.Fatalf("round trip: %+v", got)
	}
	if _, err := d.SeekRetainedStep(ctx, 0, final.Timeline.Revision+1); !errors.Is(err, api.ErrTimelineRevision) {
		t.Fatalf("revision identity: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := d.SeekRetainedStep(cancelled, 0, final.Timeline.Revision); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel identity: %v", err)
	}
}

func TestCompileRunAndImmutableArtifacts(t *testing.T) {
	result, err := api.Compile(context.Background(), helloProgram, api.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if result.HasErrors() {
		t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics())
	}
	if result.CompilerVersion() != "1.1.0" {
		t.Fatalf("compiler version %q", result.CompilerVersion())
	}
	if got := strings.Join(result.Runtime().Output, ""); got != "hello\n" {
		t.Fatalf("output %q", got)
	}
	if result.SchemaVersion() != api.ReportSchemaVersion || result.AST() == nil || len(result.IR()) == 0 || len(result.Assembly()) == 0 || len(result.CFG().Blocks) == 0 {
		t.Fatal("expected all stable artifacts")
	}

	first := result.Report()
	first.Diagnostics = append(first.Diagnostics, api.Diagnostic{Message: "caller mutation"})
	first.IR[0].Op = "caller mutation"
	first.Runtime.Output[0] = "caller mutation"
	second := result.Report()
	if len(second.Diagnostics) != 0 || second.IR[0].Op == "caller mutation" || second.Runtime.Output[0] == "caller mutation" {
		t.Fatal("result leaked mutable caller state")
	}
}

func TestCheckStopsAfterSemanticPhase(t *testing.T) {
	options := api.DefaultOptions()
	options.Mode = api.ModeCheck
	result, err := api.Compile(context.Background(), helloProgram, options)
	if err != nil {
		t.Fatal(err)
	}
	report := result.Report()
	if len(report.Symbols) == 0 || len(report.IR) != 0 || report.Runtime.ExecutedSteps != 0 {
		t.Fatalf("unexpected check artifacts: symbols=%d ir=%d steps=%d", len(report.Symbols), len(report.IR), report.Runtime.ExecutedSteps)
	}
}

func TestProjectAndIncrementalSession(t *testing.T) {
	project := api.Project{Sources: []api.SourceUnit{
		{ID: "helper", Name: "helper.cl", Content: `class Helper { value() : Int { 42 }; };`},
		{ID: "main", Name: "main.cl", Content: `class Main inherits IO { main() : Object { out_int((new Helper).value()) }; };`},
	}}
	session, err := api.NewSession(context.Background(), project, api.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, session)
	want := []api.CompilationPhase{api.PhaseLexer, api.PhaseParser, api.PhaseSemantic, api.PhaseIR, api.PhaseCodegen, api.PhaseRuntime}
	for _, phase := range want {
		snapshot, err := session.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.CurrentPhase != phase {
			t.Fatalf("phase %q, want %q", snapshot.CurrentPhase, phase)
		}
	}
	debugger, err := session.Debugger(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, debugger)
	snapshot, err := debugger.Run(context.Background(), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(snapshot.Runtime.Output, ""); got != "42" {
		t.Fatalf("output %q", got)
	}
}

func TestDiagnosticsAreResultsAndAPIMisuseIsError(t *testing.T) {
	result, err := api.Compile(context.Background(), `class Main { main() : Int { missing }; };`, api.DefaultOptions())
	if err != nil {
		t.Fatalf("COOL diagnostic became Go error: %v", err)
	}
	if !result.HasErrors() {
		t.Fatal("expected semantic diagnostic")
	}
	_, err = api.CompileProject(context.Background(), api.Project{}, api.DefaultOptions())
	if !errors.Is(err, api.ErrInvalidProject) {
		t.Fatalf("error %v", err)
	}
	options := api.DefaultOptions()
	options.MaxSteps = -1
	_, err = api.Compile(context.Background(), helloProgram, options)
	if !errors.Is(err, api.ErrInvalidOptions) {
		t.Fatalf("error %v", err)
	}
}

func TestCancellationAndConcurrentResultReaders(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := api.Compile(cancelled, helloProgram, api.DefaultOptions()); !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v", err)
	}
	result, err := api.Compile(context.Background(), helloProgram, api.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 25 {
				if result.SchemaVersion() != api.ReportSchemaVersion || len(result.Tokens()) == 0 {
					t.Error("inconsistent immutable result")
				}
			}
		}()
	}
	wait.Wait()
}

func TestDebuggerCancellationAndDisposal(t *testing.T) {
	project := api.Project{Sources: []api.SourceUnit{{ID: "main", Name: "main.cl", Content: `class Main { main() : Object { while true loop 0 pool }; };`}}}
	options := api.DefaultOptions()
	options.Limits = api.MaximumLimits()
	options.MaxSteps = options.Limits.MaxInstructionsPerRun
	debugger, err := api.NewDebugSession(context.Background(), project, options)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, runErr := debugger.Run(context.Background(), options.MaxSteps, nil)
		done <- runErr
	}()
	deadline := time.Now().Add(time.Second)
	for {
		err = debugger.Pause()
		if err == nil {
			break
		}
		if !errors.Is(err, api.ErrNotRunning) || time.Now().After(deadline) {
			t.Fatalf("pause: %v", err)
		}
	}
	if runErr := <-done; !errors.Is(runErr, context.Canceled) {
		t.Fatalf("run error %v", runErr)
	}
	if err := debugger.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := debugger.Reset(context.Background()); !errors.Is(err, api.ErrClosed) {
		t.Fatalf("reset after close: %v", err)
	}
}

func TestDebuggerInputSnapshotsAndBlockedPrograms(t *testing.T) {
	project := api.Project{Sources: []api.SourceUnit{{ID: "main", Name: "main.cl", Content: `
class Main inherits IO {
  main() : Object {
    let value : String <- in_string() in out_string(value)
  };
};`}}}
	debugger, err := api.NewDebugSession(context.Background(), project, api.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, debugger)
	full, err := debugger.Snapshot(context.Background(), false, 0)
	if err != nil || full.Result.Report().Source == "" || len(full.Result.Tokens()) == 0 {
		t.Fatalf("full snapshot: %+v %v", full.Result.Report(), err)
	}
	light, err := debugger.Snapshot(context.Background(), true, 0)
	if err != nil || light.Result.Report().Source != "" || len(light.Result.Tokens()) != 0 {
		t.Fatalf("light snapshot retained static artifacts: %v", err)
	}
	if err := debugger.SetInteractiveInput(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	waiting, err := debugger.Run(context.Background(), 0, nil)
	if err != nil || waiting.Runtime.WaitingInput == nil {
		t.Fatalf("waiting snapshot: %+v %v", waiting.Runtime, err)
	}
	provided, err := debugger.ProvideInputLine(context.Background(), "live", waiting.Runtime.WaitingInput.Sequence)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := debugger.Run(context.Background(), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(completed.Runtime.Output, ""); got != "live" || provided.Runtime.ExecutedSteps == 0 {
		t.Fatalf("output %q after input", got)
	}
	reset, err := debugger.Reset(context.Background())
	if err != nil || reset.Runtime.ExecutedSteps != 0 {
		t.Fatalf("reset: steps=%d err=%v", reset.Runtime.ExecutedSteps, err)
	}

	blocked, err := api.NewDebugSession(context.Background(), api.Project{Sources: []api.SourceUnit{{
		ID: "broken", Name: "broken.cl", Content: `class Main { main() : Int { missing }; };`,
	}}}, api.DefaultOptions())
	if err != nil {
		t.Fatalf("COOL error became Go error: %v", err)
	}
	defer closeTestResource(t, blocked)
	blockedSnapshot, err := blocked.Snapshot(context.Background(), false, 0)
	if err != nil || !blockedSnapshot.Result.HasErrors() || blockedSnapshot.Status != "blocked" {
		t.Fatalf("blocked snapshot: status=%q diagnostics=%+v err=%v", blockedSnapshot.Status, blockedSnapshot.Result.Diagnostics(), err)
	}
}

type testCloser interface {
	Close() error
}

func closeTestResource(t *testing.T, resource testCloser) {
	t.Helper()
	if err := resource.Close(); err != nil {
		t.Errorf("close test resource: %v", err)
	}
}

func TestEveryCatalogExampleUsesPublicFacade(t *testing.T) {
	catalog, err := examples.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, example := range catalog {
		t.Run(example.ID, func(t *testing.T) {
			options := api.DefaultOptions()
			options.Input = example.Input
			project := api.Project{}
			if len(example.Files) == 0 {
				project.Sources = []api.SourceUnit{{ID: example.ID, Name: example.Path, Content: example.Source}}
			} else {
				for _, file := range example.Files {
					project.Sources = append(project.Sources, api.SourceUnit{ID: example.ID + "-" + file.Name, Name: file.Name, Content: file.Source})
				}
			}
			result, err := api.CompileProject(context.Background(), project, options)
			if err != nil {
				t.Fatal(err)
			}
			if !example.IntentionalError && result.HasErrors() {
				t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics())
			}
		})
	}
}

func TestResultJSONRoundTrip(t *testing.T) {
	result, err := api.Compile(context.Background(), helloProgram, api.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := api.DecodeResult(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion() != api.ReportSchemaVersion || strings.Join(decoded.Runtime().Output, "") != "hello\n" {
		t.Fatal("round trip changed result")
	}
}

func TestCompileFilesAndIOErrors(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "main.cl")
	if err := os.WriteFile(path, []byte(helloProgram), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := api.CompileFiles(context.Background(), api.DefaultOptions(), path)
	if err != nil || result.HasErrors() {
		t.Fatalf("compile files: result=%+v err=%v", result.Diagnostics(), err)
	}
	_, err = api.CompileFiles(context.Background(), api.DefaultOptions(), filepath.Join(directory, "missing.cl"))
	if !errors.Is(err, api.ErrIO) {
		t.Fatalf("error %v", err)
	}
}
