// SPDX-License-Identifier: AGPL-3.0-or-later

package conformance

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const differentialSchemaVersion = "1.0.0"

type DifferentialConfig struct {
	Root          string
	RepoRoot      string
	CoolcPath     string
	RunnerPath    string
	CoolcArgs     []string
	RunnerArgs    []string
	VersionArgs   []string
	Timeout       time.Duration
	OutputLimit   int64
	Rule          string
	Fixture       string
	Strict        bool
	EvidenceDir   string
	AllowlistPath string
	KeepTemporary bool
}

type DifferentialReport struct {
	SchemaVersion string                      `json:"schemaVersion"`
	GeneratedAt   string                      `json:"generatedAt"`
	Host          DifferentialHost            `json:"host"`
	Tools         DifferentialTools           `json:"tools"`
	Filters       DifferentialFilters         `json:"filters"`
	Limits        DifferentialLimits          `json:"limits"`
	Summary       DifferentialSummary         `json:"summary"`
	SkippedReason string                      `json:"skippedReason,omitempty"`
	Results       []DifferentialFixtureResult `json:"results"`
}

type DifferentialHost struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	GoVersion string `json:"goVersion"`
}

type DifferentialTools struct {
	Compiler DifferentialTool `json:"compiler"`
	Runner   DifferentialTool `json:"runner"`
}

type DifferentialTool struct {
	Available    bool     `json:"available"`
	Path         string   `json:"path,omitempty"`
	ResolvedPath string   `json:"resolvedPath,omitempty"`
	SHA256       string   `json:"sha256,omitempty"`
	Size         int64    `json:"size,omitempty"`
	ModifiedAt   string   `json:"modifiedAt,omitempty"`
	Version      string   `json:"version,omitempty"`
	DefaultArgs  []string `json:"defaultArgs,omitempty"`
}

type DifferentialFilters struct {
	Rule    string `json:"rule,omitempty"`
	Fixture string `json:"fixture,omitempty"`
}

type DifferentialLimits struct {
	TimeoutMillis int64 `json:"timeoutMillis"`
	OutputBytes   int64 `json:"outputBytes"`
}

type DifferentialSummary struct {
	Selected             int `json:"selected"`
	Matched              int `json:"matched"`
	ExpectedDivergences  int `json:"expectedDivergences"`
	UnexpectedMismatches int `json:"unexpectedMismatches"`
	UnexpectedPasses     int `json:"unexpectedPasses"`
	ReferenceErrors      int `json:"referenceErrors"`
	RuntimeNotCompared   int `json:"runtimeNotCompared"`
	Skipped              int `json:"skipped"`
}

type DifferentialFixtureResult struct {
	FixtureID       string                  `json:"fixtureId"`
	RuleIDs         []string                `json:"ruleIds"`
	Sources         []string                `json:"sources"`
	Outcome         string                  `json:"outcome"`
	Differences     []string                `json:"differences,omitempty"`
	AllowlistID     string                  `json:"allowlistId,omitempty"`
	RuntimeCompared bool                    `json:"runtimeCompared"`
	Cooler          DifferentialObservation `json:"cooler"`
	Reference       DifferentialObservation `json:"reference"`
	EvidenceBundle  string                  `json:"evidenceBundle,omitempty"`
}

type DifferentialObservation struct {
	CompileAttempted    bool                 `json:"compileAttempted"`
	CompileAccepted     bool                 `json:"compileAccepted"`
	FirstFailingStage   string               `json:"firstFailingStage,omitempty"`
	DiagnosticCategory  string               `json:"diagnosticCategory,omitempty"`
	ArtifactGenerated   bool                 `json:"artifactGenerated"`
	RuntimeAttempted    bool                 `json:"runtimeAttempted"`
	RuntimeExit         string               `json:"runtimeExit,omitempty"`
	RuntimeError        string               `json:"runtimeError,omitempty"`
	Stdout              string               `json:"stdout"`
	Stderr              string               `json:"stderr"`
	CompileCommand      *DifferentialCommand `json:"compileCommand,omitempty"`
	RuntimeCommand      *DifferentialCommand `json:"runtimeCommand,omitempty"`
	InfrastructureError string               `json:"infrastructureError,omitempty"`
	TemporaryDirectory  string               `json:"temporaryDirectory,omitempty"`
}

type DifferentialCommand struct {
	Args           []string `json:"args"`
	ExitCategory   string   `json:"exitCategory"`
	ExitCode       int      `json:"exitCode"`
	Stdout         string   `json:"stdout"`
	Stderr         string   `json:"stderr"`
	DurationMillis int64    `json:"durationMillis"`
	Error          string   `json:"error,omitempty"`
}

type DifferentialAllowlist struct {
	SchemaVersion string                       `json:"schemaVersion"`
	Entries       []DifferentialAllowlistEntry `json:"entries"`
}

type DifferentialAllowlistEntry struct {
	ID                string `json:"id"`
	RuleID            string `json:"ruleId"`
	FixtureID         string `json:"fixtureId"`
	Reason            string `json:"reason"`
	Evidence          string `json:"evidence"`
	Issue             string `json:"issue"`
	ExpectedCooler    string `json:"expectedCooler"`
	ExpectedReference string `json:"expectedReference"`
	RemovalCondition  string `json:"removalCondition"`
}

var ErrReferenceUnavailable = errors.New("reference COOL toolchain unavailable")

func RunDifferential(config DifferentialConfig) (DifferentialReport, error) {
	config = defaultDifferentialConfig(config)
	report := DifferentialReport{
		SchemaVersion: differentialSchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		Host:          DifferentialHost{OS: runtime.GOOS, Arch: runtime.GOARCH, GoVersion: runtime.Version()},
		Filters:       DifferentialFilters{Rule: config.Rule, Fixture: config.Fixture},
		Limits:        DifferentialLimits{TimeoutMillis: config.Timeout.Milliseconds(), OutputBytes: config.OutputLimit},
		Results:       []DifferentialFixtureResult{},
	}

	matrix, fixtures, err := Load(config.Root, config.RepoRoot)
	if err != nil {
		return report, err
	}
	selected, err := selectDifferentialFixtures(matrix, fixtures, config.Rule, config.Fixture)
	if err != nil {
		return report, err
	}
	report.Summary.Selected = len(selected)

	allowlist, err := loadDifferentialAllowlist(config.AllowlistPath, matrix, fixtures)
	if err != nil {
		return report, err
	}

	coolcPath, _ := resolveTool(config.CoolcPath, "coolc")
	runnerPath, _ := resolveTool(config.RunnerPath, "spim")
	report.Tools.Compiler = inspectDifferentialTool(coolcPath, config.CoolcArgs, config.VersionArgs, config)
	report.Tools.Runner = inspectDifferentialTool(runnerPath, config.RunnerArgs, config.VersionArgs, config)
	if !report.Tools.Compiler.Available {
		report.SkippedReason = "reference coolc was not found; set -coolc or COOLER_REFERENCE_COOLC"
		report.Summary.Skipped = len(selected)
		if config.Strict {
			return report, fmt.Errorf("%w: %s", ErrReferenceUnavailable, report.SkippedReason)
		}
		return report, nil
	}
	if config.Strict && !report.Tools.Runner.Available {
		report.SkippedReason = "reference runtime runner was not found; set -runner or COOLER_REFERENCE_RUNNER"
		report.Summary.Skipped = len(selected)
		return report, fmt.Errorf("%w: %s", ErrReferenceUnavailable, report.SkippedReason)
	}

	for _, fixture := range selected {
		result := runDifferentialFixture(config, fixture, coolcPath, runnerPath)
		applyDifferentialOutcome(&result, allowlist)
		if result.Outcome == "unexpected-mismatch" || result.Outcome == "reference-error" {
			bundle, bundleErr := writeDifferentialEvidence(config, fixture, result, report.Host, report.Tools)
			if bundleErr != nil {
				return report, bundleErr
			}
			result.EvidenceBundle = bundle
		}
		report.Results = append(report.Results, result)
		accumulateDifferentialSummary(&report.Summary, result)
	}
	return report, nil
}

func defaultDifferentialConfig(config DifferentialConfig) DifferentialConfig {
	if config.Root == "" {
		config.Root = filepath.Join("internal", "compiler", "testdata", "conformance")
	}
	if config.RepoRoot == "" {
		config.RepoRoot = "."
	}
	if config.Timeout <= 0 {
		config.Timeout = 5 * time.Second
	}
	if config.OutputLimit <= 0 {
		config.OutputLimit = 1 << 20
	}
	if config.EvidenceDir == "" {
		config.EvidenceDir = filepath.Join("dist", "differential-evidence")
	}
	if config.AllowlistPath == "" {
		config.AllowlistPath = filepath.Join(config.Root, "differential-allowlist.json")
	}
	if config.RunnerArgs == nil {
		config.RunnerArgs = []string{"-file"}
	}
	if config.VersionArgs == nil {
		config.VersionArgs = []string{"-version"}
	}
	return config
}

func resolveTool(explicit, fallback string) (string, error) {
	if explicit != "" {
		path, err := exec.LookPath(explicit)
		if err != nil {
			return "", err
		}
		return path, nil
	}
	return exec.LookPath(fallback)
}

func inspectDifferentialTool(path string, defaultArgs, versionArgs []string, config DifferentialConfig) DifferentialTool {
	tool := DifferentialTool{Available: path != "", Path: path, DefaultArgs: append([]string(nil), defaultArgs...)}
	if path == "" {
		return tool
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		resolved = path
	}
	resolved, _ = filepath.Abs(resolved)
	tool.ResolvedPath = resolved
	if info, statErr := os.Stat(resolved); statErr == nil {
		tool.Size = info.Size()
		tool.ModifiedAt = info.ModTime().UTC().Format(time.RFC3339Nano)
	}
	if file, openErr := os.Open(resolved); openErr == nil {
		hash := sha256.New()
		if _, copyErr := io.Copy(hash, file); copyErr == nil {
			tool.SHA256 = hex.EncodeToString(hash.Sum(nil))
		}
		_ = file.Close()
	}
	if len(versionArgs) > 0 {
		workDir, mkErr := os.MkdirTemp("", "cooler-reference-version-*")
		if mkErr == nil {
			defer func() { _ = os.RemoveAll(workDir) }()
			version := executeDifferentialCommand(path, versionArgs, workDir, "", config.Timeout, minInt64(config.OutputLimit, 16<<10))
			text := strings.TrimSpace(version.Stdout)
			if text == "" {
				text = strings.TrimSpace(version.Stderr)
			}
			if text != "" {
				if newline := strings.IndexByte(text, '\n'); newline >= 0 {
					text = text[:newline]
				}
				tool.Version = text
			}
		}
	}
	if tool.Version == "" {
		tool.Version = "unreported; identify by SHA-256"
	}
	return tool
}

func selectDifferentialFixtures(matrix Matrix, fixtures []Fixture, ruleID, fixtureID string) ([]Fixture, error) {
	if ruleID != "" {
		found := false
		for _, rule := range matrix.Rules {
			if rule.ID == ruleID {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown conformance rule %q", ruleID)
		}
	}
	selected := make([]Fixture, 0, len(fixtures))
	for _, fixture := range fixtures {
		if fixtureID != "" && fixture.ID != fixtureID {
			continue
		}
		if ruleID != "" && !containsString(fixture.RuleIDs, ruleID) {
			continue
		}
		selected = append(selected, fixture)
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no fixtures match rule=%q fixture=%q", ruleID, fixtureID)
	}
	return selected, nil
}

func loadDifferentialAllowlist(path string, matrix Matrix, fixtures []Fixture) (DifferentialAllowlist, error) {
	var allowlist DifferentialAllowlist
	if err := decodeStrict(path, &allowlist); err != nil {
		return allowlist, err
	}
	if allowlist.SchemaVersion != differentialSchemaVersion {
		return allowlist, fmt.Errorf("differential allowlist schemaVersion %q, want %q", allowlist.SchemaVersion, differentialSchemaVersion)
	}
	rules := map[string]bool{}
	for _, rule := range matrix.Rules {
		rules[rule.ID] = true
	}
	fixtureRules := map[string][]string{}
	for _, fixture := range fixtures {
		fixtureRules[fixture.ID] = fixture.RuleIDs
	}
	seen := map[string]bool{}
	seenTargets := map[string]bool{}
	for _, entry := range allowlist.Entries {
		if entry.ID == "" || seen[entry.ID] {
			return allowlist, fmt.Errorf("duplicate or empty differential allowlist id %q", entry.ID)
		}
		seen[entry.ID] = true
		if !rules[entry.RuleID] {
			return allowlist, fmt.Errorf("allowlist %q references unknown rule %q", entry.ID, entry.RuleID)
		}
		fixtureRuleIDs, exists := fixtureRules[entry.FixtureID]
		if !exists || !containsString(fixtureRuleIDs, entry.RuleID) {
			return allowlist, fmt.Errorf("allowlist %q does not match fixture %q and rule %q", entry.ID, entry.FixtureID, entry.RuleID)
		}
		if entry.Reason == "" || entry.Evidence == "" || entry.Issue == "" || entry.ExpectedCooler == "" || entry.ExpectedReference == "" || entry.RemovalCondition == "" {
			return allowlist, fmt.Errorf("allowlist %q is incomplete", entry.ID)
		}
		target := entry.FixtureID + "\x00" + entry.RuleID
		if seenTargets[target] {
			return allowlist, fmt.Errorf("multiple allowlist entries target fixture %q and rule %q", entry.FixtureID, entry.RuleID)
		}
		seenTargets[target] = true
		issueURL, parseErr := url.ParseRequestURI(entry.Issue)
		if parseErr != nil || (issueURL.Scheme != "http" && issueURL.Scheme != "https") || issueURL.Host == "" {
			return allowlist, fmt.Errorf("allowlist %q has invalid issue link %q", entry.ID, entry.Issue)
		}
	}
	return allowlist, nil
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
