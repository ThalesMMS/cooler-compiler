// SPDX-License-Identifier: AGPL-3.0-or-later

// Command cooler-bench records and compares local performance baselines.
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
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/ThalesMMS/cooler-compiler/internal/benchwork"
)

const schemaVersion = "1.0.0"

type baseline struct {
	SchemaVersion    string            `json:"schemaVersion"`
	Name             string            `json:"name"`
	Commit           string            `json:"commit"`
	WorkingTreeDirty bool              `json:"workingTreeDirty,omitempty"`
	Toolchain        string            `json:"toolchain"`
	Platform         platform          `json:"platform"`
	Options          runOptions        `json:"options"`
	Workloads        []workloadSummary `json:"workloads"`
	Samples          []sample          `json:"samples"`
}

type platform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
	CPU  string `json:"cpu,omitempty"`
}

type runOptions struct {
	Count         int    `json:"count"`
	Benchtime     string `json:"benchtime"`
	Benchmark     string `json:"benchmark"`
	WebIterations int    `json:"webIterations"`
}

type workloadSummary struct {
	Name        string `json:"name"`
	SourceBytes int    `json:"sourceBytes"`
	SourceUnits int    `json:"sourceUnits"`
}

type sample struct {
	Package   string             `json:"package"`
	Benchmark string             `json:"benchmark"`
	Iteration int                `json:"iteration"`
	Metrics   map[string]float64 `json:"metrics"`
}

type webResult struct {
	Benchmark string             `json:"benchmark"`
	Metrics   map[string]float64 `json:"metrics"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runContext(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "cooler-bench:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	return runContext(context.Background(), args)
}

func runContext(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("use run or compare")
	}
	switch args[0] {
	case "run":
		return runBenchmarks(ctx, args[1:])
	case "compare":
		return compareBaselines(args[1:])
	default:
		return fmt.Errorf("unknown command %q (use run or compare)", args[0])
	}
}

func runBenchmarks(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	count := flags.Int("count", 3, "number of Go benchmark samples")
	benchtime := flags.String("benchtime", "100ms", "Go benchmark duration")
	benchmark := flags.String("bench", "^Benchmark", "Go benchmark regexp")
	webIterations := flags.Int("web-iterations", 100, "iterations per web benchmark")
	output := flags.String("output", "", "optional baseline JSON path")
	saveDir := flags.String("save-dir", "", "optional directory for <name>.json")
	name := flags.String("name", "local", "baseline name")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *count < 1 || *count > 20 {
		return errors.New("count must be between 1 and 20")
	}
	if *webIterations < 1 || *webIterations > 100_000 {
		return errors.New("web-iterations must be between 1 and 100000")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`).MatchString(*name) {
		return errors.New("name must contain only letters, numbers, dot, underscore, or hyphen")
	}
	if *output != "" && *saveDir != "" {
		return errors.New("use only one of output or save-dir")
	}
	if *saveDir != "" {
		*output = filepath.Join(*saveDir, *name+".json")
	}
	if *output != "" && filepath.Ext(*output) != ".json" {
		return errors.New("output must use the .json extension")
	}

	result := baseline{
		SchemaVersion:    schemaVersion,
		Name:             *name,
		Commit:           commandText(ctx, "git", "rev-parse", "HEAD"),
		WorkingTreeDirty: workingTreeDirty(ctx),
		Toolchain:        runtime.Version(),
		Platform:         platform{OS: runtime.GOOS, Arch: runtime.GOARCH},
		Options:          runOptions{Count: *count, Benchtime: *benchtime, Benchmark: *benchmark, WebIterations: *webIterations},
		Workloads:        workloadSummaries(),
	}

	for _, pkg := range []string{"./internal/compiler", "./cmd/coolergui", "./internal/lsp"} {
		fmt.Fprintf(os.Stderr, "benchmarking %s\n", pkg)
		command := exec.CommandContext(ctx, "go", "test", pkg, "-run", "^$", "-bench", *benchmark, "-benchmem", "-count", strconv.Itoa(*count), "-benchtime", *benchtime)
		outputBytes, err := command.CombinedOutput()
		parsed, cpu, parseErr := parseGoBenchmarkOutput(outputBytes)
		if result.Platform.CPU == "" {
			result.Platform.CPU = cpu
		}
		if err != nil {
			return fmt.Errorf("benchmark %s: %w\n%s", pkg, err, outputBytes)
		}
		if parseErr != nil {
			return fmt.Errorf("parse benchmark %s: %w", pkg, parseErr)
		}
		result.Samples = append(result.Samples, parsed...)
	}

	fmt.Fprintln(os.Stderr, "benchmarking web rendering helpers")
	for iteration := 1; iteration <= *count; iteration++ {
		webCommand := exec.CommandContext(ctx, "node", "scripts/benchmark-web.mjs", "--iterations", strconv.Itoa(*webIterations))
		webOutput, err := webCommand.CombinedOutput()
		if err != nil {
			return fmt.Errorf("web benchmarks: %w\n%s", err, webOutput)
		}
		var web []webResult
		if err := json.Unmarshal(webOutput, &web); err != nil {
			return fmt.Errorf("parse web benchmarks: %w", err)
		}
		for _, item := range web {
			result.Samples = append(result.Samples, sample{Package: "web/client-utils", Benchmark: item.Benchmark, Iteration: iteration, Metrics: item.Metrics})
		}
	}
	sortSamples(result.Samples)

	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if *output != "" {
		if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(*output, encoded, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "saved %d samples to %s\n", len(result.Samples), *output)
		return nil
	}
	_, err = os.Stdout.Write(encoded)
	return err
}

func compareBaselines(args []string) error {
	flags := flag.NewFlagSet("compare", flag.ContinueOnError)
	basePath := flags.String("base", "", "baseline JSON")
	newPath := flags.String("new", "", "candidate JSON")
	threshold := flags.Float64("threshold", 10, "advisory regression percentage")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *basePath == "" || *newPath == "" {
		return errors.New("compare requires -base and -new")
	}
	base, err := readBaseline(*basePath)
	if err != nil {
		return fmt.Errorf("read base: %w", err)
	}
	candidate, err := readBaseline(*newPath)
	if err != nil {
		return fmt.Errorf("read new: %w", err)
	}
	return printComparison(os.Stdout, base, candidate, *threshold)
}

func parseGoBenchmarkOutput(output []byte) ([]sample, string, error) {
	var samples []sample
	pkg, cpu := "", ""
	iterations := map[string]int{}
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "pkg: ") {
			pkg = strings.TrimSpace(strings.TrimPrefix(line, "pkg: "))
			continue
		}
		if strings.HasPrefix(line, "cpu: ") {
			cpu = strings.TrimSpace(strings.TrimPrefix(line, "cpu: "))
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 || !strings.HasPrefix(fields[0], "Benchmark") {
			continue
		}
		name := stripCPUSuffix(fields[0])
		metrics := map[string]float64{}
		for index := 2; index+1 < len(fields); index += 2 {
			value, err := strconv.ParseFloat(fields[index], 64)
			if err != nil {
				continue
			}
			metrics[fields[index+1]] = value
		}
		if len(metrics) == 0 {
			continue
		}
		key := pkg + "\x00" + name
		iterations[key]++
		samples = append(samples, sample{Package: pkg, Benchmark: name, Iteration: iterations[key], Metrics: metrics})
	}
	if err := scanner.Err(); err != nil {
		return nil, cpu, err
	}
	return samples, cpu, nil
}

func stripCPUSuffix(name string) string {
	index := strings.LastIndexByte(name, '-')
	if index < 0 {
		return name
	}
	if _, err := strconv.Atoi(name[index+1:]); err == nil {
		return name[:index]
	}
	return name
}

func readBaseline(path string) (baseline, error) {
	var result baseline
	data, err := os.ReadFile(path)
	if err != nil {
		return result, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, err
	}
	if result.SchemaVersion != schemaVersion {
		return result, fmt.Errorf("unsupported schemaVersion %q", result.SchemaVersion)
	}
	return result, nil
}

func printComparison(writer io.Writer, base, candidate baseline, threshold float64) error {
	type aggregate struct {
		Package, Benchmark, Metric string
		Value                      float64
	}
	aggregateSamples := func(samples []sample) map[string]aggregate {
		values := map[string][]float64{}
		for _, item := range samples {
			for metric, value := range item.Metrics {
				key := item.Package + "\x00" + item.Benchmark + "\x00" + metric
				values[key] = append(values[key], value)
			}
		}
		result := map[string]aggregate{}
		for key, list := range values {
			parts := strings.Split(key, "\x00")
			result[key] = aggregate{parts[0], parts[1], parts[2], median(list)}
		}
		return result
	}
	oldValues, newValues := aggregateSamples(base.Samples), aggregateSamples(candidate.Samples)
	keys := make([]string, 0, len(oldValues)+len(newValues))
	seen := map[string]bool{}
	for key := range oldValues {
		keys = append(keys, key)
		seen[key] = true
	}
	for key := range newValues {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if _, err := fmt.Fprintf(writer, "Comparing %s (%s) -> %s (%s)\n", base.Name, shortCommit(base.Commit), candidate.Name, shortCommit(candidate.Commit)); err != nil {
		return err
	}
	for _, key := range keys {
		old, oldOK := oldValues[key]
		newValue, newOK := newValues[key]
		label := strings.TrimPrefix(newValue.Benchmark, "Benchmark") + " " + newValue.Metric
		if !newOK {
			label = strings.TrimPrefix(old.Benchmark, "Benchmark") + " " + old.Metric
			if _, err := fmt.Fprintf(writer, "REMOVED %-52s\n", label); err != nil {
				return err
			}
			continue
		}
		if !oldOK {
			if _, err := fmt.Fprintf(writer, "ADDED   %-52s %.3g\n", label, newValue.Value); err != nil {
				return err
			}
			continue
		}
		change := 0.0
		if old.Value != 0 {
			change = (newValue.Value - old.Value) / old.Value * 100
		}
		status := "ok"
		if change > threshold && lowerIsBetter(newValue.Metric) {
			status = "REGRESSION?"
		}
		if _, err := fmt.Fprintf(writer, "%-11s %-52s %+.2f%% (%.3g -> %.3g)\n", status, label, change, old.Value, newValue.Value); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(writer, "Advisory only: investigate flagged changes; this command never fails on timing variance.")
	return err
}

func median(values []float64) float64 {
	values = append([]float64(nil), values...)
	sort.Float64s(values)
	middle := len(values) / 2
	if len(values)%2 == 1 {
		return values[middle]
	}
	return math.FMA(values[middle-1], 0.5, values[middle]*0.5)
}

func lowerIsBetter(metric string) bool {
	return metric == "ns/op" || metric == "B/op" || metric == "allocs/op" || strings.HasSuffix(metric, "bytes/op")
}

func workloadSummaries() []workloadSummary {
	workloads := append(benchwork.StandardProjects(), benchwork.DeepInheritance(400), benchwork.NestedExpression(400), benchwork.LongText(500), benchwork.ArithmeticLoop(1_000), benchwork.RecursiveCalls(100), benchwork.Dispatch(500, false), benchwork.Dispatch(500, true), benchwork.AllocationLoop(500), benchwork.DeepCase(200), benchwork.StringOperations(100), benchwork.IOOperations(100))
	result := make([]workloadSummary, 0, len(workloads))
	for _, item := range workloads {
		result = append(result, workloadSummary{Name: item.Name, SourceBytes: len(item.Source), SourceUnits: len(item.Units)})
	}
	return result
}

func sortSamples(samples []sample) {
	sort.Slice(samples, func(left, right int) bool {
		if samples[left].Package != samples[right].Package {
			return samples[left].Package < samples[right].Package
		}
		if samples[left].Benchmark != samples[right].Benchmark {
			return samples[left].Benchmark < samples[right].Benchmark
		}
		return samples[left].Iteration < samples[right].Iteration
	})
}

func workingTreeDirty(ctx context.Context) bool {
	output, err := exec.CommandContext(ctx, "git", "status", "--porcelain", "--untracked-files=no").Output()
	return err == nil && strings.TrimSpace(string(output)) != ""
}

func commandText(ctx context.Context, name string, args ...string) string {
	output, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}

func shortCommit(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}
