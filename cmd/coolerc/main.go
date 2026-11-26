// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	publiccompiler "github.com/ThalesMMS/cooler-compiler/compiler"
	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
)

const (
	exitOK       = 0
	exitError    = 1
	exitUsage    = 2
	exitTool     = 3
	defaultSteps = 100_000
)

var (
	version   = "1.0.0"
	commit    = ""
	buildDate = ""
	maxSteps  = compiler.MaximumVMPolicy().MaxInstructionsPerRun
)

type cliOptions struct {
	command              string
	emit                 string
	files                []string
	inputFile            string
	vmPolicyFile         string
	vmPolicy             compiler.VMPolicy
	maxSteps             int
	diagnosticFormat     string
	color                string
	versionJSON          bool
	versionVerbose       bool
	runBeforeEmit        bool
	interactive          bool
	optimization         string
	warningRules         string
	emitIR               string
	explainOptimizations bool
	// Course-assignment (opt-in) modes; see docs/course-assignments.md.
	format   string
	stage    string
	frontend string
	target   string
	gc       compiler.MIPSGCOptions
}

type usageError struct{ message string }

func (e usageError) Error() string { return e.message }

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "fmt" {
		return runFormat(args[1:], stdin, stdout, stderr)
	}
	if len(args) > 0 && args[0] == "test" {
		return runProgramTests(context.Background(), args[1:], stdout, stderr)
	}
	options, usage, err := parseCLI(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			if usage != nil {
				if writeErr := usage(); writeErr != nil {
					return exitTool
				}
			}
			return exitOK
		}
		if usage != nil {
			if writeErr := usage(); writeErr != nil {
				return exitTool
			}
		}
		if message := strings.TrimSpace(err.Error()); message != "" {
			if _, writeErr := fmt.Fprintf(stderr, "coolerc: %s\n", message); writeErr != nil {
				return exitTool
			}
		}
		return exitUsage
	}
	if options.command == "version" {
		if err := writeVersion(stdout, options); err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "coolerc: could not write version: %v\n", err); writeErr != nil {
				return exitTool
			}
			return exitTool
		}
		return exitOK
	}
	if err := loadVMPolicy(&options); err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "coolerc: %v\n", err); writeErr != nil {
			return exitTool
		}
		return exitUsage
	}

	name, source, files, programInput, err := readInputs(options, stdin)
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "coolerc: %v\n", err); writeErr != nil {
			return exitTool
		}
		return exitTool
	}
	if courseModeRequested(options) {
		return runCourseMode(options, name, string(source), files, programInput, stdout, stderr)
	}

	var result compiler.Result
	if options.interactive {
		result, err = compileInteractively(options, string(source), files, stdin, stdout)
		if err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "coolerc: interactive execution failed: %v\n", err); writeErr != nil {
				return exitTool
			}
			return exitTool
		}
	} else {
		result, err = compileForCommand(options, name, string(source), files, programInput)
		if err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "coolerc: compilation API failed: %v\n", err); writeErr != nil {
				return exitTool
			}
			return exitTool
		}
	}
	if err := writeDiagnostics(stderr, name, files, result.Diagnostics, options.diagnosticFormat); err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "coolerc: could not write diagnostics: %v\n", err); writeErr != nil {
			return exitTool
		}
		return exitTool
	}
	if !options.interactive {
		if err := writeCommandOutput(stdout, options, result); err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "coolerc: could not write output: %v\n", err); writeErr != nil {
				return exitTool
			}
			return exitTool
		}
	}

	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Severity == compiler.SeverityError {
			return exitError
		}
	}
	return exitOK
}

func parseCLI(args []string, stderr io.Writer) (cliOptions, func() error, error) {
	options := cliOptions{command: "run", maxSteps: defaultSteps, diagnosticFormat: "text", color: "auto", optimization: compiler.OptimizationNone, emitIR: "after"}
	if len(args) > 0 {
		switch args[0] {
		case "run", "check":
			options.command = args[0]
			return parseSubcommand(options, args[1:], stderr)
		case "emit":
			if len(args) < 2 {
				return options, generalUsage(stderr), usageError{"emit requires an artifact name"}
			}
			options.command, options.emit = "emit", args[1]
			if !validEmit(options.emit) {
				return options, generalUsage(stderr), usageError{fmt.Sprintf("unknown artifact %q", options.emit)}
			}
			return parseSubcommand(options, args[2:], stderr)
		case "version":
			options.command = "version"
			flags := flag.NewFlagSet("coolerc version", flag.ContinueOnError)
			flags.SetOutput(io.Discard)
			flags.BoolVar(&options.versionJSON, "json", false, "write version information as JSON")
			flags.BoolVar(&options.versionVerbose, "verbose", false, "include toolchain and target information")
			usage := generalUsage(stderr)
			if err := flags.Parse(args[1:]); err != nil {
				return options, usage, err
			}
			if flags.NArg() != 0 {
				return options, usage, usageError{"version does not accept source files"}
			}
			return options, usage, nil
		}
	}
	return parseLegacy(options, args, stderr)
}

func parseSubcommand(options cliOptions, args []string, stderr io.Writer) (cliOptions, func() error, error) {
	flags := flag.NewFlagSet("coolerc "+options.command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	if options.command == "run" || options.runBeforeEmit {
		flags.StringVar(&options.inputFile, "input", "", "read program input from FILE")
		flags.IntVar(&options.maxSteps, "max-steps", defaultSteps, "maximum VM instructions")
		if options.command == "run" {
			flags.BoolVar(&options.interactive, "interactive", false, "read program input one line at a time")
		}
	}
	flags.StringVar(&options.vmPolicyFile, "vm-policy", "", "read bounded VM policy overrides from JSON FILE")
	flags.StringVar(&options.diagnosticFormat, "diagnostic-format", "text", "text, json, or reference (course semant text; check only)")
	flags.StringVar(&options.color, "color", "auto", "auto, always, or never")
	flags.StringVar(&options.optimization, "opt", compiler.OptimizationNone, "optimization mode: none or basic")
	flags.StringVar(&options.frontend, "frontend", compiler.FrontendHandwritten, "front-end: handwritten (default) or generated (spec-driven lexer + LALR parser)")
	flags.StringVar(&options.warningRules, "warnings", "", "warning rules: all, none, or comma-separated diagnostic codes")
	if options.command == "emit" && (options.emit == "tokens" || options.emit == "ast") {
		flags.StringVar(&options.format, "format", "cooler", "artifact format: cooler (JSON) or reference (course text)")
	}
	if options.command == "run" || (options.command == "emit" && options.emit == "assembly") {
		flags.StringVar(&options.target, "target", "vm", "assembly target: vm (educational pseudo-assembly) or mips (SPIM, Cool runtime)")
		flags.BoolVar(&options.gc.Generational, "g", false, "with --target=mips: generational garbage collector (coolc -g)")
		flags.BoolVar(&options.gc.Test, "t", false, "with --target=mips: collect at every allocation (coolc -t)")
		flags.BoolVar(&options.gc.Debug, "T", false, "with --target=mips: check object headers on attribute stores (coolc -T)")
	}
	if options.command == "emit" && options.emit == "ast" {
		flags.StringVar(&options.stage, "stage", "parser", "reference AST stage: parser or semant")
	}
	if options.command == "emit" && options.emit == "ir" {
		flags.StringVar(&options.emitIR, "emit-ir", "after", "IR view: before, after, or both")
		flags.BoolVar(&options.explainOptimizations, "explain-optimizations", false, "include pass decisions when emitting IR")
	}
	usage := generalUsage(stderr)
	if err := flags.Parse(args); err != nil {
		return options, usage, err
	}
	options.files = flags.Args()
	if err := validateOptions(options); err != nil {
		return options, usage, err
	}
	return options, usage, nil
}

func parseLegacy(options cliOptions, args []string, stderr io.Writer) (cliOptions, func() error, error) {
	flags := flag.NewFlagSet("coolerc", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	jsonOutput := flags.Bool("json", false, "write the complete compilation result as JSON")
	showIR := flags.Bool("ir", false, "print generated stack IR instead of program output")
	showAssembly := flags.Bool("assembly", false, "print pseudo-assembly instead of program output")
	showVersion := flags.Bool("version", false, "print version and exit")
	flags.StringVar(&options.inputFile, "input", "", "read program input from FILE")
	flags.IntVar(&options.maxSteps, "max-steps", defaultSteps, "maximum VM instructions")
	flags.StringVar(&options.vmPolicyFile, "vm-policy", "", "read bounded VM policy overrides from JSON FILE")
	flags.StringVar(&options.diagnosticFormat, "diagnostic-format", "text", "text, json, or reference (course semant text; check only)")
	flags.StringVar(&options.color, "color", "auto", "auto, always, or never")
	flags.StringVar(&options.optimization, "opt", compiler.OptimizationNone, "optimization mode: none or basic")
	flags.StringVar(&options.frontend, "frontend", compiler.FrontendHandwritten, "front-end: handwritten (default) or generated (spec-driven lexer + LALR parser)")
	flags.StringVar(&options.warningRules, "warnings", "", "warning rules: all, none, or comma-separated diagnostic codes")
	usage := legacyUsage(stderr, flags)
	if err := flags.Parse(args); err != nil {
		return options, usage, err
	}
	if enabledCount(*jsonOutput, *showIR, *showAssembly) > 1 {
		return options, usage, usageError{"-json, -ir, and -assembly are mutually exclusive"}
	}
	if *showVersion {
		if flags.NArg() != 0 || enabledCount(*jsonOutput, *showIR, *showAssembly) != 0 {
			return options, usage, usageError{"--version cannot be combined with source files or output modes"}
		}
		options.command = "version"
		return options, usage, nil
	}
	options.files = flags.Args()
	if *jsonOutput {
		options.command, options.emit, options.runBeforeEmit = "emit", "report", true
	}
	if *showIR {
		options.command, options.emit, options.runBeforeEmit = "emit", "ir", true
	}
	if *showAssembly {
		options.command, options.emit, options.runBeforeEmit = "emit", "assembly", true
	}
	if err := validateOptions(options); err != nil {
		return options, usage, err
	}
	return options, usage, nil
}

func validateOptions(options cliOptions) error {
	if options.interactive && options.command != "run" {
		return usageError{"--interactive is only available with run"}
	}
	if options.interactive && options.inputFile != "" {
		return usageError{"--interactive and --input are mutually exclusive"}
	}
	if options.interactive && len(options.files) == 0 {
		return usageError{"--interactive requires at least one source file so stdin is reserved for program input"}
	}
	if options.maxSteps < 1 || options.maxSteps > maxSteps {
		return usageError{fmt.Sprintf("--max-steps must be between 1 and %d", maxSteps)}
	}
	if options.diagnosticFormat != "text" && options.diagnosticFormat != "json" && options.diagnosticFormat != "reference" {
		return usageError{"--diagnostic-format must be text, json, or reference"}
	}
	if options.color != "auto" && options.color != "always" && options.color != "never" {
		return usageError{"--color must be auto, always, or never"}
	}
	if _, err := compiler.NormalizeOptimizationMode(options.optimization); err != nil {
		return usageError{"--opt must be none or basic"}
	}
	if _, err := compiler.NormalizeFrontend(options.frontend); err != nil {
		return usageError{"--frontend must be handwritten or generated"}
	}
	if err := compiler.ValidateWarningRules(options.warningRules); err != nil {
		return usageError{err.Error()}
	}
	if options.emitIR != "before" && options.emitIR != "after" && options.emitIR != "both" {
		return usageError{"--emit-ir must be before, after, or both"}
	}
	if err := validateCourseOptions(options); err != nil {
		return err
	}
	if (options.emitIR != "after" || options.explainOptimizations) && !(options.command == "emit" && options.emit == "ir") {
		return usageError{"--emit-ir and --explain-optimizations are only available with 'emit ir'"}
	}
	return nil
}

func loadVMPolicy(options *cliOptions) error {
	policy := compiler.VMPolicy{}
	if options.vmPolicyFile != "" {
		file, err := os.Open(options.vmPolicyFile)
		if err != nil {
			return fmt.Errorf("could not open VM policy: %w", err)
		}
		contents, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
		closeErr := file.Close()
		if err != nil {
			return fmt.Errorf("could not read VM policy: %w", errors.Join(err, closeErr))
		}
		if closeErr != nil {
			return fmt.Errorf("could not close VM policy: %w", closeErr)
		}
		if len(contents) > 64<<10 {
			return errors.New("invalid VM policy JSON: expected an object smaller than 64 KiB")
		}
		decoder := json.NewDecoder(bytes.NewReader(contents))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&policy); err != nil {
			return fmt.Errorf("invalid VM policy JSON: %w", err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return errors.New("invalid VM policy JSON: expected exactly one object smaller than 64 KiB")
		}
	}
	normalized, err := compiler.NormalizeVMPolicy(policy)
	if err != nil {
		return err
	}
	if options.maxSteps > normalized.MaxInstructionsPerRun {
		return fmt.Errorf("--max-steps must not exceed vmPolicy.maxInstructionsPerRun (%d)", normalized.MaxInstructionsPerRun)
	}
	options.vmPolicy = normalized
	return nil
}

func validEmit(value string) bool {
	for _, candidate := range []string{"tokens", "ast", "symbols", "cfg", "ir", "assembly", "report"} {
		if value == candidate {
			return true
		}
	}
	return false
}

func generalUsage(w io.Writer) func() error {
	return func() error {
		_, err := fmt.Fprintln(w, "usage: coolerc run [options] [file.cl ...]")
		if err == nil {
			_, err = fmt.Fprintln(w, "       coolerc check [options] [file.cl ...]")
		}
		if err == nil {
			_, err = fmt.Fprintln(w, "       coolerc test [--json] exercise.json")
		}
		if err == nil {
			_, err = fmt.Fprintln(w, "       coolerc fmt [-check|-w] [file.cl ...]")
		}
		if err == nil {
			_, err = fmt.Fprintln(w, "       coolerc emit tokens|ast|symbols|cfg|ir|assembly|report [options] [file.cl ...]")
		}
		if err == nil {
			_, err = fmt.Fprintln(w, "       coolerc version [--json|--verbose]")
		}
		return err
	}
}

func legacyUsage(w io.Writer, flags *flag.FlagSet) func() error {
	return func() error {
		if _, err := fmt.Fprintln(w, "usage: coolerc [options] [file.cl ...]"); err != nil {
			return err
		}
		flags.SetOutput(w)
		flags.PrintDefaults()
		flags.SetOutput(io.Discard)
		return nil
	}
}

func readInputs(options cliOptions, stdin io.Reader) (string, []byte, []sourceFile, string, error) {
	name := "<stdin>"
	var source []byte
	var files []sourceFile
	if len(options.files) > 0 {
		for index, path := range options.files {
			data, err := os.ReadFile(path)
			if err != nil {
				return name, nil, nil, "", err
			}
			files = append(files, sourceFile{id: fmt.Sprintf("source-%d", index+1), name: path, source: string(data)})
		}
		name = options.files[0]
	} else {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return name, nil, nil, "", fmt.Errorf("could not read %s: %w", name, err)
		}
		source = data
	}

	programInput := ""
	if options.command == "run" || options.runBeforeEmit {
		if options.inputFile != "" {
			data, err := os.ReadFile(options.inputFile)
			if err != nil {
				return name, nil, nil, "", fmt.Errorf("could not read program input: %w", err)
			}
			programInput = string(data)
		} else if len(options.files) > 0 && !options.interactive && !stdinIsCharDevice(stdin) {
			data, err := io.ReadAll(stdin)
			if err != nil {
				return name, nil, nil, "", fmt.Errorf("could not read program input: %w", err)
			}
			programInput = string(data)
		}
	}
	return name, source, files, programInput, nil
}

func compileForCommand(options cliOptions, _ string, source string, files []sourceFile, input string) (compiler.Result, error) {
	mode := publiccompiler.ModeRun
	if options.command == "check" {
		mode = publiccompiler.ModeCheck
	}
	publicOptions := publiccompiler.Options{
		WarningRules: options.warningRules,
		Mode:         mode, Input: input, MaxSteps: options.maxSteps, Limits: publicLimits(options.vmPolicy),
		Optimization: options.optimization, Frontend: options.frontend, CompilerVersion: version, CompilerCommit: resolvedCommit(),
	}
	project := publiccompiler.Project{}
	if len(files) == 0 {
		project.Sources = []publiccompiler.SourceUnit{{ID: "source-1", Name: "program.cl", Content: source}}
	} else {
		project.Sources = make([]publiccompiler.SourceUnit, len(files))
		for index, file := range files {
			project.Sources[index] = publiccompiler.SourceUnit{ID: file.id, Name: filepath.Base(file.name), Content: file.source}
		}
	}
	session, err := publiccompiler.NewSession(context.Background(), project, publicOptions)
	if err != nil {
		return compiler.Result{}, err
	}
	defer func() { _ = session.Close() }()
	snapshot, err := session.Complete(context.Background())
	if err != nil {
		return compiler.Result{}, err
	}
	publicResult := snapshot.Result
	if (options.command == "run" || options.runBeforeEmit) && !snapshot.Blocked {
		debugger, debugErr := session.Debugger(context.Background())
		if debugErr != nil {
			return compiler.Result{}, debugErr
		}
		defer func() { _ = debugger.Close() }()
		if err := debugger.SetInput(context.Background(), input); err != nil {
			return compiler.Result{}, err
		}
		debugSnapshot, err := debugger.Run(context.Background(), options.maxSteps, nil)
		if err != nil {
			return compiler.Result{}, err
		}
		publicResult = debugSnapshot.Result
	}
	result, err := internalResult(publicResult)
	if err != nil {
		return compiler.Result{}, err
	}
	return resultWithStepLimitDiagnostic(result, options.maxSteps), nil
}

func publicLimits(policy compiler.VMPolicy) publiccompiler.Limits {
	return publiccompiler.Limits{
		MaxInstructionsPerRun: policy.MaxInstructionsPerRun, MaxInstructionsTotal: policy.MaxInstructionsTotal,
		MaxOperandStackDepth: policy.MaxOperandStackDepth, MaxCallFrameDepth: policy.MaxCallFrameDepth,
		MaxScopeDepth: policy.MaxScopeDepth, MaxObjectAllocations: policy.MaxObjectAllocations,
		MaxTraceEntries: policy.MaxTraceEntries, MaxTraceBytes: policy.MaxTraceBytes,
		MaxStringBytes: policy.MaxStringBytes, MaxOutputBytes: policy.MaxOutputBytes,
		MaxInputBytes: policy.MaxInputBytes, MaxInputLines: policy.MaxInputLines, MaxBreakpoints: policy.MaxBreakpoints,
	}
}

func internalResult(result publiccompiler.Result) (compiler.Result, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return compiler.Result{}, err
	}
	var converted compiler.Result
	if err := json.Unmarshal(encoded, &converted); err != nil {
		return compiler.Result{}, err
	}
	return converted, nil
}

func compilationPipeline(options cliOptions, source string, files []sourceFile, inputProvided bool) *compiler.CompilationSession {
	contract := compiler.ResultContract{
		CompilerVersion: version,
		CompilerCommit:  resolvedCommit(),
		Options: compiler.CompilationOptions{
			WarningRules:  options.warningRules,
			Execute:       options.command == "run" || options.runBeforeEmit,
			VMMaxSteps:    options.maxSteps,
			InputProvided: inputProvided,
			VMPolicy:      options.vmPolicy,
			Optimization:  options.optimization,
			Frontend:      options.frontend,
		},
	}
	if len(files) > 0 {
		units := make([]compiler.SourceUnit, len(files))
		for index, file := range files {
			units[index] = compiler.SourceUnit{ID: file.id, Name: filepath.Base(file.name), Content: file.source}
		}
		return compiler.NewProjectCompilationSession(units, contract)
	}
	return compiler.NewCompilationSessionWithContract(source, contract)
}

func compileInteractively(options cliOptions, source string, files []sourceFile, stdin io.Reader, stdout io.Writer) (compiler.Result, error) {
	pipeline := compilationPipeline(options, source, files, false)
	result := pipeline.Complete().Result
	session, ok := pipeline.DebugSession()
	if !ok {
		return result, nil
	}
	session.SetInteractiveInput(true)
	reader := bufio.NewReader(stdin)
	writtenOutput := 0
	for {
		remaining := options.maxSteps - result.Runtime.ExecutedSteps
		if remaining <= 0 {
			return resultWithStepLimitDiagnostic(result, options.maxSteps), nil
		}
		snapshot := session.Run(remaining, nil)
		result = snapshot.Result
		if writtenOutput < len(result.Runtime.Output) {
			if _, err := io.WriteString(stdout, strings.Join(result.Runtime.Output[writtenOutput:], "")); err != nil {
				return result, err
			}
			writtenOutput = len(result.Runtime.Output)
		}
		if result.Runtime.Halted || result.Runtime.WaitingInput == nil {
			return resultWithStepLimitDiagnostic(result, options.maxSteps), nil
		}

		line, err := reader.ReadString('\n')
		if errors.Is(err, io.EOF) && line == "" {
			result = session.FinishInteractiveInput().Result
			continue
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return result, err
		}
		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\r")
		result, err = provideInteractiveInput(session, line, result.Runtime.WaitingInput.Sequence)
		if err != nil {
			return result, err
		}
	}
}

func provideInteractiveInput(session *compiler.DebugSession, line string, sequence int) (compiler.Result, error) {
	snapshot, err := session.ProvideInputLine(line, sequence)
	return snapshot.Result, err
}

func resultWithStepLimitDiagnostic(result compiler.Result, limit int) compiler.Result {
	if result.Runtime.Paused && !result.Runtime.Halted {
		span := compiler.Span{}
		if trace := result.Runtime.Trace; len(trace) > 0 {
			span = trace[len(trace)-1].Span
		}
		result.Diagnostics = append(result.Diagnostics, compiler.Diagnostic{
			Code: "COL6002", Stage: "runtime", Severity: compiler.SeverityError,
			Message: fmt.Sprintf("execution did not halt within %d instructions", limit),
			Hint:    "Increase --max-steps only for programs that are expected to terminate.",
			Line:    span.StartLine, Column: span.StartColumn, Span: span,
		})
	}
	return result
}

func writeCommandOutput(w io.Writer, options cliOptions, result compiler.Result) error {
	switch options.command {
	case "check":
		return nil
	case "run":
		_, err := io.WriteString(w, strings.Join(result.Runtime.Output, ""))
		return err
	case "emit":
		switch options.emit {
		case "tokens":
			return writeJSON(w, result.Tokens)
		case "ast":
			return writeJSON(w, result.AST)
		case "symbols":
			return writeJSON(w, result.Symbols)
		case "cfg":
			return writeJSON(w, result.ControlFlow)
		case "report":
			return writeJSON(w, result)
		case "ir":
			before, after := result.Optimization.BeforeIR, result.Optimization.AfterIR
			if len(before) == 0 {
				before = result.IR
			}
			if len(after) == 0 {
				after = result.IR
			}
			if options.emitIR == "before" || options.emitIR == "both" {
				if options.emitIR == "both" {
					if _, err := fmt.Fprintln(w, ";; unoptimized IR"); err != nil {
						return err
					}
				}
				if err := writeIR(w, before, options.optimization != compiler.OptimizationNone || options.emitIR != "after"); err != nil {
					return err
				}
			}
			if options.emitIR == "after" || options.emitIR == "both" {
				if options.emitIR == "both" {
					label := ";; execution IR"
					if result.Optimization.ExecutionIR == "optimized" {
						label = ";; optimized IR"
					}
					if _, err := fmt.Fprintln(w, label); err != nil {
						return err
					}
				}
				if err := writeIR(w, after, options.optimization != compiler.OptimizationNone || options.emitIR != "after"); err != nil {
					return err
				}
			}
			if options.explainOptimizations {
				if err := writeOptimizationExplanation(w, result.Optimization); err != nil {
					return err
				}
			}
		case "assembly":
			for _, line := range result.Assembly {
				if _, err := fmt.Fprintln(w, line); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func writeIR(w io.Writer, instructions []compiler.IRInstruction, showOrigins bool) error {
	for _, instruction := range instructions {
		format := "%04d %-24s %-16s %s\n"
		values := []any{instruction.Index, instruction.Method, instruction.Op, strings.Join(instruction.Args, ", ")}
		if showOrigins {
			format = "%04d %-24s %-16s %-20s ; origins %v\n"
			values = append(values, instruction.OriginalIndexes)
		}
		if _, err := fmt.Fprintf(w, format, values...); err != nil {
			return err
		}
	}
	return nil
}

func writeOptimizationExplanation(w io.Writer, report compiler.OptimizationReport) error {
	if _, err := fmt.Fprintf(w, ";; optimization mode=%s execution=%s baseline-verified=%t optimized-verified=%t\n", report.Mode, report.ExecutionIR, report.BaselineVerified, report.OptimizedVerified); err != nil {
		return err
	}
	for _, pass := range report.Passes {
		if _, err := fmt.Fprintf(w, ";; pass %s: %d -> %d instructions, %d transformations, %d rejected, verified=%t\n", pass.Contract.Name, pass.BeforeInstructions, pass.AfterInstructions, pass.Transformations, pass.Rejected, pass.Verified); err != nil {
			return err
		}
		for _, change := range pass.Changes {
			if _, err := fmt.Fprintf(w, ";;   %s %s: %s => %s (%s)\n", change.Method, change.Kind, change.Before, change.After, change.Explanation); err != nil {
				return err
			}
		}
		for _, rejection := range pass.Rejections {
			if _, err := fmt.Fprintf(w, ";;   kept %s: %s\n", rejection.Opportunity, rejection.Reason); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func writeVersion(w io.Writer, options cliOptions) error {
	info := struct {
		Version   string `json:"version"`
		Commit    string `json:"commit,omitempty"`
		BuildDate string `json:"buildDate,omitempty"`
		GoVersion string `json:"goVersion"`
		Target    string `json:"target"`
	}{version, resolvedCommit(), buildDate, runtime.Version(), runtime.GOOS + "/" + runtime.GOARCH}
	if options.versionJSON {
		return writeJSON(w, info)
	}
	if _, err := fmt.Fprintf(w, "coolerc %s", info.Version); err != nil {
		return err
	}
	if info.Commit != "" {
		if _, err := fmt.Fprintf(w, " (%s)", info.Commit); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if options.versionVerbose {
		if _, err := fmt.Fprintf(w, "go: %s\ntarget: %s\n", info.GoVersion, info.Target); err != nil {
			return err
		}
		if info.BuildDate != "" {
			_, err := fmt.Fprintf(w, "build date: %s\n", info.BuildDate)
			return err
		}
	}
	return nil
}

func resolvedCommit() string {
	if commit != "" {
		return commit
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				return setting.Value
			}
		}
	}
	return ""
}

type sourceFile struct {
	id     string
	name   string
	source string
}

func enabledCount(values ...bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}
	return count
}

func stdinIsCharDevice(stdin io.Reader) bool {
	file, ok := stdin.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

type outputRelatedSpan struct {
	File   string        `json:"file"`
	Line   int           `json:"line"`
	Column int           `json:"column"`
	Label  string        `json:"label"`
	Span   compiler.Span `json:"span"`
}

type outputDiagnostic struct {
	File     string                      `json:"file"`
	Line     int                         `json:"line"`
	Column   int                         `json:"column"`
	Code     string                      `json:"code"`
	Stage    string                      `json:"stage"`
	Severity compiler.DiagnosticSeverity `json:"severity"`
	Message  string                      `json:"message"`
	Hint     string                      `json:"hint,omitempty"`
	Span     compiler.Span               `json:"span"`
	Related  []outputRelatedSpan         `json:"related,omitempty"`
}

func mappedDiagnostics(name string, files []sourceFile, diagnostics []compiler.Diagnostic) []outputDiagnostic {
	fileByID := map[string]string{}
	for _, file := range files {
		fileByID[file.id] = file.name
	}
	result := make([]outputDiagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		fileName := fileByID[diagnostic.Span.SourceID]
		if fileName == "" {
			fileName = name
		}
		mapped := outputDiagnostic{File: fileName, Line: diagnostic.Line, Column: diagnostic.Column, Code: diagnostic.Code, Stage: diagnostic.Stage, Severity: diagnostic.Severity, Message: diagnostic.Message, Hint: diagnostic.Hint, Span: diagnostic.Span}
		for _, related := range diagnostic.Related {
			relatedFile := fileByID[related.Span.SourceID]
			if relatedFile == "" {
				relatedFile = name
			}
			mapped.Related = append(mapped.Related, outputRelatedSpan{File: relatedFile, Line: related.Line, Column: related.Column, Label: related.Label, Span: related.Span})
		}
		result = append(result, mapped)
	}
	return result
}

func writeDiagnostics(w io.Writer, name string, files []sourceFile, diagnostics []compiler.Diagnostic, format string) error {
	mapped := mappedDiagnostics(name, files, diagnostics)
	if format == "json" {
		return writeJSON(w, mapped)
	}
	for _, diagnostic := range mapped {
		if _, err := fmt.Fprintf(w, "%s:%d:%d: %s %s %s: %s\n", diagnostic.File, diagnostic.Line, diagnostic.Column, diagnostic.Severity, diagnostic.Stage, diagnostic.Code, diagnostic.Message); err != nil {
			return err
		}
		if diagnostic.Hint != "" {
			if _, err := fmt.Fprintf(w, "  hint: %s\n", diagnostic.Hint); err != nil {
				return err
			}
		}
		for _, related := range diagnostic.Related {
			if _, err := fmt.Fprintf(w, "  related: %s:%d:%d: %s\n", related.File, related.Line, related.Column, related.Label); err != nil {
				return err
			}
		}
	}
	return nil
}
