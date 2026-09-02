// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	internal "github.com/ThalesMMS/cooler-compiler/internal/compiler"
)

var (
	// ErrInvalidOptions identifies unsupported or unsafe compiler options.
	ErrInvalidOptions = errors.New("compiler: invalid options")
	// ErrInvalidProject identifies missing, duplicate, or malformed source units.
	ErrInvalidProject = errors.New("compiler: invalid project")
	// ErrClosed identifies an operation attempted after Close.
	ErrClosed = errors.New("compiler: session is closed")
	// ErrNotReady means compilation has not reached a debuggable runtime phase.
	ErrNotReady = errors.New("compiler: debugger is not ready")
	// ErrDebuggerAcquired means a Compilation already transferred its VM to a
	// debugger.
	ErrDebuggerAcquired = errors.New("compiler: debugger was already acquired")
	// ErrUnsupportedSchema identifies a report from an unsupported schema version.
	ErrUnsupportedSchema = errors.New("compiler: unsupported report schema")
	// ErrInvalidReport identifies malformed schema-versioned report JSON.
	ErrInvalidReport = errors.New("compiler: invalid report")
	// ErrIO identifies a source file read failure.
	ErrIO = errors.New("compiler: source I/O failed")
	// ErrInternal identifies a facade conversion or invariant failure.
	ErrInternal = errors.New("compiler: internal failure")
)

const defaultMaxSteps = 100_000

// DefaultOptions returns the supported baseline: execute without optimization
// under bounded desktop limits.
func DefaultOptions() Options {
	return Options{Mode: ModeRun, MaxSteps: defaultMaxSteps, Limits: fromInternalLimits(internal.DefaultVMPolicy()), Optimization: OptimizationNone}
}

// MaximumLimits returns the local ceilings accepted by NormalizeOptions.
func MaximumLimits() Limits { return fromInternalLimits(internal.MaximumVMPolicy()) }

// NormalizeOptions fills defaults and rejects unsupported modes, optimization
// names, and unsafe runtime limits.
func NormalizeOptions(options Options) (Options, error) {
	if options.Mode == "" {
		options.Mode = ModeRun
	}
	if options.Mode != ModeRun && options.Mode != ModeCheck {
		return Options{}, fmt.Errorf("%w: mode must be run or check", ErrInvalidOptions)
	}
	if options.MaxSteps == 0 {
		options.MaxSteps = defaultMaxSteps
	}
	if options.MaxSteps < 1 {
		return Options{}, fmt.Errorf("%w: max steps must be positive", ErrInvalidOptions)
	}
	policy, err := internal.NormalizeVMPolicy(toInternalLimits(options.Limits))
	if err != nil {
		return Options{}, fmt.Errorf("%w: %w", ErrInvalidOptions, err)
	}
	if options.MaxSteps > policy.MaxInstructionsPerRun {
		return Options{}, fmt.Errorf("%w: max steps exceeds limits.maxInstructionsPerRun", ErrInvalidOptions)
	}
	optimization, err := internal.NormalizeOptimizationMode(options.Optimization)
	if err != nil {
		return Options{}, fmt.Errorf("%w: optimization must be none or basic", ErrInvalidOptions)
	}
	options.Limits = fromInternalLimits(policy)
	options.Optimization = optimization
	if strings.TrimSpace(options.CompilerVersion) == "" {
		options.CompilerVersion = "1.0.0"
	}
	return options, nil
}

// Result is an immutable compilation result. Its methods decode detached
// copies, so callers cannot mutate state observed by another goroutine.
type Result struct {
	encoded []byte
}

func resultFromInternal(value internal.Result) (Result, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return Result{}, fmt.Errorf("%w: encode result: %w", ErrInternal, err)
	}
	return Result{encoded: encoded}, nil
}

// LoadProject reads ordered source files into detached units. File names are
// display metadata; generated source IDs stay stable for the supplied order.
func LoadProject(ctx context.Context, paths ...string) (Project, error) {
	if err := contextError(ctx); err != nil {
		return Project{}, err
	}
	if len(paths) == 0 {
		return Project{}, fmt.Errorf("%w: at least one path is required", ErrInvalidProject)
	}
	project := Project{Sources: make([]SourceUnit, 0, len(paths))}
	for index, path := range paths {
		if err := contextError(ctx); err != nil {
			return Project{}, err
		}
		if strings.TrimSpace(path) == "" {
			return Project{}, fmt.Errorf("%w: path %d is empty", ErrInvalidProject, index)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return Project{}, fmt.Errorf("%w: read %q: %w", ErrIO, path, err)
		}
		project.Sources = append(project.Sources, SourceUnit{
			ID: fmt.Sprintf("source-%d", index+1), Name: filepath.Base(path), Content: string(content),
		})
	}
	return project, nil
}

// CompileFiles loads and compiles ordered COOL files.
func CompileFiles(ctx context.Context, options Options, paths ...string) (Result, error) {
	project, err := LoadProject(ctx, paths...)
	if err != nil {
		return Result{}, err
	}
	return CompileProject(ctx, project, options)
}

// DecodeResult validates and imports a schema-versioned JSON report.
func DecodeResult(data []byte) (Result, error) {
	decoded, err := internal.DecodeReport(data)
	if errors.Is(err, internal.ErrUnsupportedReportSchema) {
		return Result{}, fmt.Errorf("%w: %w", ErrUnsupportedSchema, err)
	}
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrInvalidReport, err)
	}
	return resultFromInternal(decoded)
}

// MarshalJSON returns an independent copy of the schema-versioned report.
func (r Result) MarshalJSON() ([]byte, error) {
	if len(r.encoded) == 0 {
		return []byte("null"), nil
	}
	return append([]byte(nil), r.encoded...), nil
}

// UnmarshalJSON validates and replaces r with a schema-versioned report.
func (r *Result) UnmarshalJSON(data []byte) error {
	decoded, err := DecodeResult(data)
	if err != nil {
		return err
	}
	*r = decoded
	return nil
}

// Report returns a detached caller-owned view of every stable artifact.
func (r Result) Report() Report {
	var report Report
	_ = json.Unmarshal(r.encoded, &report)
	return report
}

// SchemaVersion returns the report schema version.
func (r Result) SchemaVersion() string { return r.Report().SchemaVersion }

// CompilerVersion returns the producer version recorded in the report.
func (r Result) CompilerVersion() string { return r.Report().CompilerVersion }

// Diagnostics returns a detached copy of compiler and runtime findings.
func (r Result) Diagnostics() []Diagnostic { return r.Report().Diagnostics }

// Tokens returns detached lexical tokens in source-unit order.
func (r Result) Tokens() []Token { return r.Report().Tokens }

// AST returns a detached abstract syntax tree, or nil if parsing was blocked.
func (r Result) AST() *ASTNode { return r.Report().AST }

// Symbols returns detached semantic symbol records.
func (r Result) Symbols() []Symbol { return r.Report().Symbols }

// IR returns detached source-mapped stack instructions.
func (r Result) IR() []IRInstruction { return r.Report().IR }

// CFG returns a detached control-flow graph.
func (r Result) CFG() ControlFlowGraph { return r.Report().ControlFlow }

// Assembly returns detached pseudo-assembly lines.
func (r Result) Assembly() []string { return r.Report().Assembly }

// Metrics returns deterministic compiler artifact counts.
func (r Result) Metrics() Metrics { return r.Report().Metrics }

// Runtime returns the detached bounded VM result.
func (r Result) Runtime() RuntimeResult { return r.Report().Runtime }

// HasErrors reports whether any diagnostic has error severity.
func (r Result) HasErrors() bool {
	for _, diagnostic := range r.Diagnostics() {
		if diagnostic.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Compile compiles one source unit. COOL diagnostics are returned in Result;
// error is reserved for invalid API use, cancellation, and internal failures.
func Compile(ctx context.Context, source string, options Options) (Result, error) {
	return CompileProject(ctx, Project{Sources: []SourceUnit{{ID: "source-1", Name: "program.cl", Content: source}}}, options)
}

// CompileProject compiles ordered units and, in ModeRun, executes Main.main.
func CompileProject(ctx context.Context, project Project, options Options) (result Result, err error) {
	session, err := NewSession(ctx, project, options)
	if err != nil {
		return Result{}, err
	}
	defer func() { err = errors.Join(err, session.Close()) }()
	snapshot, err := session.Complete(ctx)
	if err != nil || session.options.Mode == ModeCheck || snapshot.Blocked {
		return snapshot.Result, err
	}
	debugger, err := session.Debugger(ctx)
	if err != nil {
		return Result{}, err
	}
	defer func() { err = errors.Join(err, debugger.Close()) }()
	if err := debugger.SetInput(ctx, session.options.Input); err != nil {
		return Result{}, err
	}
	debugSnapshot, runErr := debugger.Run(ctx, session.options.MaxSteps, nil)
	return debugSnapshot.Result, runErr
}

// Compilation is an incremental compiler session. Methods are safe to call
// concurrently; operations execute in call-acquisition order.
type Compilation struct {
	mu             sync.Mutex
	pipeline       *internal.CompilationSession
	options        Options
	closed         bool
	debuggerIssued bool
}

// NewSession validates and copies a project into a stopped incremental
// compilation. The caller must Close the returned session.
func NewSession(ctx context.Context, project Project, options Options) (*Compilation, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	normalized, err := NormalizeOptions(options)
	if err != nil {
		return nil, err
	}
	units, err := validateProject(project)
	if err != nil {
		return nil, err
	}
	contract := internal.ResultContract{
		CompilerVersion: normalized.CompilerVersion,
		CompilerCommit:  normalized.CompilerCommit,
		Options: internal.CompilationOptions{
			Execute: normalized.Mode == ModeRun, VMMaxSteps: normalized.MaxSteps,
			InputProvided: normalized.Input != "", VMPolicy: toInternalLimits(normalized.Limits),
			Optimization: normalized.Optimization,
		},
	}
	return &Compilation{pipeline: internal.NewProjectCompilationSession(units, contract), options: normalized}, nil
}

// Snapshot returns the current detached pipeline state without advancing it.
func (c *Compilation) Snapshot() (PipelineSnapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return PipelineSnapshot{}, ErrClosed
	}
	return publicPipelineSnapshot(c.pipeline.Snapshot(), c.options.Mode)
}

// Next advances at most one compiler phase and honors context cancellation
// before acquiring the serialized session operation.
func (c *Compilation) Next(ctx context.Context) (PipelineSnapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return PipelineSnapshot{}, ErrClosed
	}
	if err := contextError(ctx); err != nil {
		return PipelineSnapshot{}, err
	}
	current := c.pipeline.Snapshot()
	if c.options.Mode == ModeCheck && current.CurrentPhase == internal.PhaseSemantic {
		return publicPipelineSnapshot(current, c.options.Mode)
	}
	next := c.pipeline.NextPhase()
	if err := contextError(ctx); err != nil {
		return PipelineSnapshot{}, err
	}
	return publicPipelineSnapshot(next, c.options.Mode)
}

// Complete advances until the selected mode completes or a diagnostic blocks
// the pipeline.
func (c *Compilation) Complete(ctx context.Context) (PipelineSnapshot, error) {
	for {
		snapshot, err := c.Snapshot()
		if err != nil {
			return PipelineSnapshot{}, err
		}
		if snapshot.Complete || snapshot.Blocked {
			return snapshot, nil
		}
		snapshot, err = c.Next(ctx)
		if err != nil {
			return PipelineSnapshot{}, err
		}
		if snapshot.Complete || snapshot.Blocked {
			return snapshot, nil
		}
	}
}

// Debugger transfers the stopped runtime VM to one DebugSession. It succeeds
// only after PhaseRuntime and at most once per Compilation.
func (c *Compilation) Debugger(ctx context.Context) (*DebugSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, ErrClosed
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if c.debuggerIssued {
		return nil, ErrDebuggerAcquired
	}
	raw, ok := c.pipeline.DebugSession()
	if !ok {
		return nil, ErrNotReady
	}
	c.debuggerIssued = true
	return newDebugSession(raw, c.options), nil
}

// Close releases compiler state, is idempotent, and causes later operations to
// return ErrClosed. It does not close a DebugSession already acquired from c.
func (c *Compilation) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.pipeline = nil
	return nil
}

func publicPipelineSnapshot(snapshot internal.PipelineSnapshot, mode Mode) (PipelineSnapshot, error) {
	result, err := resultFromInternal(snapshot.Result)
	if err != nil {
		return PipelineSnapshot{}, err
	}
	complete := snapshot.Complete
	next := CompilationPhase(snapshot.NextPhase)
	if mode == ModeCheck && snapshot.CurrentPhase == internal.PhaseSemantic {
		complete = true
		next = PhaseInitial
	}
	return PipelineSnapshot{CurrentPhase: CompilationPhase(snapshot.CurrentPhase), NextPhase: next, Complete: complete, Blocked: snapshot.Blocked, Result: result}, nil
}

func validateProject(project Project) ([]internal.SourceUnit, error) {
	if len(project.Sources) == 0 {
		return nil, fmt.Errorf("%w: at least one source unit is required", ErrInvalidProject)
	}
	seen := make(map[string]struct{}, len(project.Sources))
	units := make([]internal.SourceUnit, 0, len(project.Sources))
	for index, unit := range project.Sources {
		if strings.TrimSpace(unit.ID) == "" {
			return nil, fmt.Errorf("%w: source %d has an empty ID", ErrInvalidProject, index)
		}
		if strings.TrimSpace(unit.Name) == "" {
			return nil, fmt.Errorf("%w: source %q has an empty name", ErrInvalidProject, unit.ID)
		}
		if _, exists := seen[unit.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate source ID %q", ErrInvalidProject, unit.ID)
		}
		seen[unit.ID] = struct{}{}
		units = append(units, internal.SourceUnit{ID: unit.ID, Name: unit.Name, Content: unit.Content})
	}
	return units, nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("%w: nil context", ErrInvalidOptions)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func toInternalLimits(value Limits) internal.VMPolicy {
	return internal.VMPolicy{
		MaxInstructionsPerRun: value.MaxInstructionsPerRun, MaxInstructionsTotal: value.MaxInstructionsTotal,
		MaxOperandStackDepth: value.MaxOperandStackDepth, MaxCallFrameDepth: value.MaxCallFrameDepth,
		MaxScopeDepth: value.MaxScopeDepth, MaxObjectAllocations: value.MaxObjectAllocations,
		MaxTraceEntries: value.MaxTraceEntries, MaxTraceBytes: value.MaxTraceBytes,
		MaxStringBytes: value.MaxStringBytes, MaxOutputBytes: value.MaxOutputBytes,
		MaxInputBytes: value.MaxInputBytes, MaxInputLines: value.MaxInputLines, MaxBreakpoints: value.MaxBreakpoints,
	}
}

func fromInternalLimits(value internal.VMPolicy) Limits {
	return Limits{
		MaxInstructionsPerRun: value.MaxInstructionsPerRun, MaxInstructionsTotal: value.MaxInstructionsTotal,
		MaxOperandStackDepth: value.MaxOperandStackDepth, MaxCallFrameDepth: value.MaxCallFrameDepth,
		MaxScopeDepth: value.MaxScopeDepth, MaxObjectAllocations: value.MaxObjectAllocations,
		MaxTraceEntries: value.MaxTraceEntries, MaxTraceBytes: value.MaxTraceBytes,
		MaxStringBytes: value.MaxStringBytes, MaxOutputBytes: value.MaxOutputBytes,
		MaxInputBytes: value.MaxInputBytes, MaxInputLines: value.MaxInputLines, MaxBreakpoints: value.MaxBreakpoints,
	}
}
