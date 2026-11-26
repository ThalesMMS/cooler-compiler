// SPDX-License-Identifier: AGPL-3.0-or-later

package conformance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
	"github.com/ThalesMMS/cooler-compiler/internal/reftools"
)

// The phase differential compares Cooler's opt-in reference formats (PA2–PA4)
// with a user-supplied course `lexer`, `parser`, and `semant`. Like the
// runtime differential it never downloads or bundles a tool, and it skips
// cleanly when the tools are absent unless strict mode is requested.

const phaseDifferentialSchemaVersion = "1.0.0"

// Phase names compared by the harness.
const (
	PhaseTokens = "tokens"
	PhaseParser = "parser"
	PhaseSemant = "semant"
)

type PhaseDifferentialConfig struct {
	Root        string
	RepoRoot    string
	BatteryDirs []string
	LexerPath   string
	ParserPath  string
	SemantPath  string
	CoolRoot    string
	Timeout     time.Duration
	OutputLimit int64
	Rule        string
	Fixture     string
	Strict      bool
	// AllowlistPath names a JSON file of evidenced, expected divergences.
	AllowlistPath string
}

type PhaseDifferentialReport struct {
	SchemaVersion string                    `json:"schemaVersion"`
	GeneratedAt   string                    `json:"generatedAt"`
	Host          DifferentialHost          `json:"host"`
	Tools         PhaseDifferentialTools    `json:"tools"`
	Summary       PhaseDifferentialSummary  `json:"summary"`
	SkippedReason string                    `json:"skippedReason,omitempty"`
	Results       []PhaseDifferentialResult `json:"results"`
}

type PhaseDifferentialTools struct {
	Lexer  DifferentialTool `json:"lexer"`
	Parser DifferentialTool `json:"parser"`
	Semant DifferentialTool `json:"semant"`
}

type PhaseDifferentialSummary struct {
	Cases               int `json:"cases"`
	Comparisons         int `json:"comparisons"`
	Matched             int `json:"matched"`
	ExpectedDivergences int `json:"expectedDivergences"`
	Mismatches          int `json:"mismatches"`
	ReferenceErrors     int `json:"referenceErrors"`
	Skipped             int `json:"skipped"`
}

type PhaseDifferentialResult struct {
	Case        string   `json:"case"`
	Sources     []string `json:"sources"`
	Phase       string   `json:"phase"`
	Outcome     string   `json:"outcome"`
	AllowlistID string   `json:"allowlistId,omitempty"`
	FirstDiff   string   `json:"firstDifference,omitempty"`
	Cooler      string   `json:"cooler"`
	Reference   string   `json:"reference"`
	Error       string   `json:"error,omitempty"`
}

type PhaseAllowlist struct {
	SchemaVersion string                `json:"schemaVersion"`
	Entries       []PhaseAllowlistEntry `json:"entries"`
}

// PhaseAllowlistEntry records one evidenced divergence. Every field is
// required; an entry is removed when its removal condition is met.
type PhaseAllowlistEntry struct {
	ID               string `json:"id"`
	Case             string `json:"case"`
	Phase            string `json:"phase"`
	Reason           string `json:"reason"`
	Evidence         string `json:"evidence"`
	RemovalCondition string `json:"removalCondition"`
}

type phaseCase struct {
	id      string
	sources []string
}

// RunPhaseDifferential compares the reference token, parser, and semant
// output on the conformance corpus and the PA-style fixture battery.
func RunPhaseDifferential(config PhaseDifferentialConfig) (PhaseDifferentialReport, error) {
	if config.Timeout <= 0 {
		config.Timeout = 5 * time.Second
	}
	if config.OutputLimit <= 0 {
		config.OutputLimit = 1 << 20
	}
	report := PhaseDifferentialReport{
		SchemaVersion: phaseDifferentialSchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		Host:          DifferentialHost{OS: runtime.GOOS, Arch: runtime.GOARCH, GoVersion: runtime.Version()},
		Results:       []PhaseDifferentialResult{},
	}
	cases, err := phaseCases(config)
	if err != nil {
		return report, err
	}
	report.Summary.Cases = len(cases)
	allowlist, err := loadPhaseAllowlist(config.AllowlistPath)
	if err != nil {
		return report, err
	}

	lexer := reftools.LocateReferenceTool(config.LexerPath, "lexer", config.CoolRoot)
	parser := reftools.LocateReferenceTool(config.ParserPath, "parser", config.CoolRoot)
	semant := reftools.LocateReferenceTool(config.SemantPath, "semant", config.CoolRoot)
	limits := DifferentialConfig{Timeout: config.Timeout, OutputLimit: config.OutputLimit}
	report.Tools = PhaseDifferentialTools{
		Lexer:  inspectDifferentialTool(lexer, nil, nil, limits),
		Parser: inspectDifferentialTool(parser, nil, nil, limits),
		Semant: inspectDifferentialTool(semant, nil, nil, limits),
	}
	if lexer == "" {
		report.SkippedReason = "reference lexer was not found; set -lexer, COOLER_REFERENCE_LEXER, or -cool-root"
		report.Summary.Skipped = len(cases)
		if config.Strict {
			return report, fmt.Errorf("%w: %s", ErrReferenceUnavailable, report.SkippedReason)
		}
		return report, nil
	}
	if config.Strict && (parser == "" || semant == "") {
		report.SkippedReason = "reference parser or semant was not found"
		return report, fmt.Errorf("%w: %s", ErrReferenceUnavailable, report.SkippedReason)
	}

	for _, item := range cases {
		results, err := comparePhaseCase(config, item, lexer, parser, semant)
		if err != nil {
			return report, err
		}
		for _, result := range results {
			applyPhaseAllowlist(&result, allowlist)
			report.Summary.Comparisons++
			switch result.Outcome {
			case "match":
				report.Summary.Matched++
			case "expected-divergence":
				report.Summary.ExpectedDivergences++
			case "mismatch":
				report.Summary.Mismatches++
			case "reference-error":
				report.Summary.ReferenceErrors++
			}
			report.Results = append(report.Results, result)
		}
	}
	return report, nil
}

// HasFailures reports unexplained differences or reference failures.
func (r PhaseDifferentialReport) HasFailures() bool {
	return r.Summary.Mismatches > 0 || r.Summary.ReferenceErrors > 0
}

func phaseCases(config PhaseDifferentialConfig) ([]phaseCase, error) {
	cases := []phaseCase{}
	if config.Root != "" {
		matrix, fixtures, err := Load(config.Root, config.RepoRoot)
		if err != nil {
			return nil, err
		}
		selected, err := selectDifferentialFixtures(matrix, fixtures, config.Rule, config.Fixture)
		if err != nil {
			return nil, err
		}
		for _, fixture := range selected {
			paths := make([]string, len(fixture.Sources))
			for index, source := range fixture.Sources {
				paths[index] = resolveFixtureSource(config.RepoRoot, fixture, source)
			}
			cases = append(cases, phaseCase{id: "conformance/" + fixture.ID, sources: paths})
		}
	}
	if config.Rule == "" && config.Fixture == "" {
		for _, dir := range config.BatteryDirs {
			matches, err := filepath.Glob(filepath.Join(dir, "*.cl"))
			if err != nil {
				return nil, err
			}
			sort.Strings(matches)
			for _, match := range matches {
				cases = append(cases, phaseCase{id: "battery/" + filepath.Base(match), sources: []string{match}})
			}
		}
	}
	return cases, nil
}

func resolveFixtureSource(repoRoot string, fixture Fixture, source string) string {
	if filepath.IsAbs(source) {
		return source
	}
	if fixture.metadataPath != "" {
		candidate := filepath.Join(filepath.Dir(fixture.metadataPath), source)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return filepath.Join(repoRoot, source)
}

// comparePhaseCase copies the case's sources into a private directory under
// their base names, so the course tools and Cooler print the same file
// names, then compares each phase the course pipeline reaches.
func comparePhaseCase(config PhaseDifferentialConfig, item phaseCase, lexer, parser, semant string) ([]PhaseDifferentialResult, error) {
	dir, err := os.MkdirTemp("", "cooler-phase-differential-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	names := make([]string, len(item.sources))
	units := make([]compiler.SourceUnit, len(item.sources))
	for index, source := range item.sources {
		content, err := os.ReadFile(source)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", source, err)
		}
		name := filepath.Base(source)
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			return nil, err
		}
		names[index] = name
		units[index] = compiler.SourceUnit{ID: fmt.Sprintf("unit-%d", index), Name: name, Content: string(content)}
	}
	run := func(tool string, args []string, input string) DifferentialCommand {
		return executeDifferentialCommand(tool, args, dir, input, config.Timeout, config.OutputLimit)
	}
	result := func(phase, cooler string, command DifferentialCommand, useStderr bool) PhaseDifferentialResult {
		reference := command.Stdout
		if useStderr {
			reference = command.Stderr
		}
		reference = strings.ReplaceAll(reference, dir+string(filepath.Separator), "")
		entry := PhaseDifferentialResult{Case: item.id, Sources: names, Phase: phase, Cooler: cooler, Reference: reference}
		switch {
		case command.ExitCategory == "timeout" || command.ExitCategory == "launch-error" || command.ExitCategory == "output-limit" || command.ExitCategory == "crash":
			entry.Outcome, entry.Error = "reference-error", command.Error
		case cooler == reference:
			entry.Outcome = "match"
		default:
			entry.Outcome, entry.FirstDiff = "mismatch", firstLineDifference(cooler, reference)
		}
		return entry
	}

	results := []PhaseDifferentialResult{}
	tokens := run(lexer, names, "")
	results = append(results, result(PhaseTokens, compiler.ReferenceTokenDump(units), tokens, false))
	if parser == "" || tokens.ExitCode != 0 {
		return results, nil
	}
	parsed := run(parser, nil, tokens.Stdout)
	parserText, parserOK := compiler.ReferenceParserDump(units)
	results = append(results, result(PhaseParser, parserText, parsed, parsed.ExitCode != 0))
	if semant == "" || parsed.ExitCode != 0 || !parserOK {
		return results, nil
	}
	checked := run(semant, nil, parsed.Stdout)
	semantText, _ := compiler.ReferenceSemantDump(units)
	results = append(results, result(PhaseSemant, semantText, checked, checked.ExitCode != 0))
	return results, nil
}

func firstLineDifference(cooler, reference string) string {
	left, right := strings.Split(cooler, "\n"), strings.Split(reference, "\n")
	for index := 0; index < len(left) || index < len(right); index++ {
		var a, b string
		if index < len(left) {
			a = left[index]
		}
		if index < len(right) {
			b = right[index]
		}
		if a != b {
			return fmt.Sprintf("line %d: cooler %q, reference %q", index+1, a, b)
		}
	}
	return ""
}

func loadPhaseAllowlist(path string) (PhaseAllowlist, error) {
	if path == "" {
		return PhaseAllowlist{}, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return PhaseAllowlist{}, err
	}
	var allowlist PhaseAllowlist
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&allowlist); err != nil {
		return PhaseAllowlist{}, fmt.Errorf("invalid phase allowlist: %w", err)
	}
	for _, entry := range allowlist.Entries {
		if entry.ID == "" || entry.Case == "" || entry.Phase == "" || entry.Reason == "" || entry.Evidence == "" || entry.RemovalCondition == "" {
			return PhaseAllowlist{}, errors.New("invalid phase allowlist: every entry needs id, case, phase, reason, evidence, and removalCondition")
		}
	}
	return allowlist, nil
}

func applyPhaseAllowlist(result *PhaseDifferentialResult, allowlist PhaseAllowlist) {
	if result.Outcome != "mismatch" {
		return
	}
	for _, entry := range allowlist.Entries {
		if entry.Case == result.Case && entry.Phase == result.Phase {
			result.Outcome, result.AllowlistID = "expected-divergence", entry.ID
			return
		}
	}
}

// WritePhaseDifferentialReport writes the machine-readable report.
func WritePhaseDifferentialReport(path string, report PhaseDifferentialReport) error {
	return writeIndentedJSON(path, report)
}

func writeIndentedJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}
