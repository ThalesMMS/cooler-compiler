// SPDX-License-Identifier: AGPL-3.0-or-later

package conformance

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
)

const schemaVersion = "1.0.0"

var (
	ruleIDPattern    = regexp.MustCompile(`^(LEX|PAR|SEM-CLASS|SEM-EXPR|RUN|BUILTIN|TOOL)-[A-Z0-9]+(?:-[A-Z0-9]+)*$`)
	fixtureIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

var canonicalManualSections = []ManualSection{
	{Section: "1", Title: "Introduction"},
	{Section: "2", Title: "Getting Started"},
	{Section: "3", Title: "Classes"},
	{Section: "3.1", Title: "Features"},
	{Section: "3.2", Title: "Inheritance"},
	{Section: "4", Title: "Types"},
	{Section: "4.1", Title: "SELF_TYPE"},
	{Section: "4.2", Title: "Type Checking"},
	{Section: "5", Title: "Attributes"},
	{Section: "5.1", Title: "Void"},
	{Section: "6", Title: "Methods"},
	{Section: "7", Title: "Expressions"},
	{Section: "7.1", Title: "Constants"},
	{Section: "7.2", Title: "Identifiers"},
	{Section: "7.3", Title: "Assignment"},
	{Section: "7.4", Title: "Dispatch"},
	{Section: "7.5", Title: "Conditionals"},
	{Section: "7.6", Title: "Loops"},
	{Section: "7.7", Title: "Blocks"},
	{Section: "7.8", Title: "Let"},
	{Section: "7.9", Title: "Case"},
	{Section: "7.10", Title: "New"},
	{Section: "7.11", Title: "Isvoid"},
	{Section: "7.12", Title: "Arithmetic and Comparison Operations"},
	{Section: "8", Title: "Basic Classes"},
	{Section: "8.1", Title: "Object"},
	{Section: "8.2", Title: "IO"},
	{Section: "8.3", Title: "Int"},
	{Section: "8.4", Title: "String"},
	{Section: "8.5", Title: "Bool"},
	{Section: "9", Title: "Main Class"},
	{Section: "10", Title: "Lexical Structure"},
	{Section: "10.1", Title: "Integers, Identifiers, and Special Notation"},
	{Section: "10.2", Title: "Strings"},
	{Section: "10.3", Title: "Comments"},
	{Section: "10.4", Title: "Keywords"},
	{Section: "10.5", Title: "White Space"},
	{Section: "11", Title: "Cool Syntax"},
	{Section: "11.1", Title: "Precedence"},
	{Section: "12", Title: "Type Rules"},
	{Section: "12.1", Title: "Type Environments"},
	{Section: "12.2", Title: "Type Checking Rules"},
	{Section: "13", Title: "Operational Semantics"},
	{Section: "13.1", Title: "Environment and the Store"},
	{Section: "13.2", Title: "Syntax for Cool Objects"},
	{Section: "13.3", Title: "Class Definitions"},
	{Section: "13.4", Title: "Operational Rules"},
	{Section: "14", Title: "Acknowledgements"},
}

func Load(root, repoRoot string) (Matrix, []Fixture, error) {
	var matrix Matrix
	if err := decodeStrict(filepath.Join(root, "matrix.json"), &matrix); err != nil {
		return Matrix{}, nil, err
	}

	fixtures := []Fixture{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "fixture.json" {
			return nil
		}
		var fixture Fixture
		if err := decodeStrict(path, &fixture); err != nil {
			return err
		}
		fixture.metadataPath = path
		fixtures = append(fixtures, fixture)
		return nil
	})
	if err != nil {
		return Matrix{}, nil, fmt.Errorf("load fixtures: %w", err)
	}
	sort.Slice(fixtures, func(i, j int) bool { return fixtures[i].ID < fixtures[j].ID })

	if err := validate(matrix, fixtures, root, repoRoot); err != nil {
		return Matrix{}, nil, err
	}
	return matrix, fixtures, nil
}

func decodeStrict(path string, destination any) (err error) {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("multiple JSON values")
}

func validate(matrix Matrix, fixtures []Fixture, root, repoRoot string) error {
	problems := []string{}
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if matrix.SchemaVersion != schemaVersion {
		add("matrix schemaVersion %q, want %q", matrix.SchemaVersion, schemaVersion)
	}
	if strings.TrimSpace(matrix.Project) == "" {
		add("matrix project is empty")
	}

	sourceIDs := map[string]bool{}
	for index, source := range matrix.NormativeHierarchy {
		if source.Rank != index+1 {
			add("normative source %q rank %d, want %d", source.ID, source.Rank, index+1)
		}
		if source.ID == "" || sourceIDs[source.ID] {
			add("duplicate or empty normative source id %q", source.ID)
		}
		sourceIDs[source.ID] = true
		if source.Title == "" || source.Locator == "" || source.Role == "" {
			add("normative source %q is incomplete", source.ID)
		}
	}
	if len(sourceIDs) == 0 {
		add("normative hierarchy is empty")
	}

	allowedStatuses := map[string]bool{
		"supported": true, "partial": true, "intentional-divergence": true,
		"unsupported": true, "not-yet-tested": true,
	}
	for status := range allowedStatuses {
		if strings.TrimSpace(matrix.StatusDefinitions[status]) == "" {
			add("missing status definition %q", status)
		}
	}

	rules := map[string]Rule{}
	fixtureReferences := map[string]map[string]bool{}
	for _, rule := range matrix.Rules {
		if !ruleIDPattern.MatchString(rule.ID) {
			add("invalid rule id %q", rule.ID)
		}
		if _, exists := rules[rule.ID]; exists {
			add("duplicate rule id %q", rule.ID)
		}
		rules[rule.ID] = rule
		if rule.Name == "" || rule.Behavior == "" || rule.ExpectedStage == "" || rule.ReferenceComparison == "" {
			add("rule %q is missing required text", rule.ID)
		}
		if !sourceIDs[rule.Citation.Source] || rule.Citation.Section == "" {
			add("rule %q has invalid citation", rule.ID)
		}
		if !allowedStatuses[rule.Status] {
			add("rule %q has invalid status %q", rule.ID, rule.Status)
		}
		if rule.Status == "supported" && len(rule.PositiveFixtures)+len(rule.NegativeFixtures) == 0 {
			add("supported rule %q has no fixture", rule.ID)
		}
		if (rule.Status == "partial" || rule.Status == "unsupported" || rule.Status == "not-yet-tested") && rule.Issue == "" {
			add("rule %q with status %q has no issue link", rule.ID, rule.Status)
		}
		if (rule.Status == "supported" || rule.Status == "intentional-divergence") && rule.Issue != "" {
			add("completed rule %q retains issue link %q", rule.ID, rule.Issue)
		}
		if rule.Status == "intentional-divergence" {
			if rule.Divergence == nil || strings.TrimSpace(rule.Divergence.Boundary) == "" || strings.TrimSpace(rule.Divergence.Reason) == "" || strings.TrimSpace(rule.Divergence.Consequence) == "" || strings.TrimSpace(rule.Divergence.Assertion) == "" {
				add("rule %q has an incomplete divergence record", rule.ID)
			}
		} else if rule.Divergence != nil {
			add("rule %q has divergence details but status %q", rule.ID, rule.Status)
		}
		for _, id := range append(append([]string{}, rule.PositiveFixtures...), rule.NegativeFixtures...) {
			if fixtureReferences[id] == nil {
				fixtureReferences[id] = map[string]bool{}
			}
			fixtureReferences[id][rule.ID] = true
		}
	}
	if len(rules) == 0 {
		add("matrix contains no rules")
	}
	if sourceIDs["cool-manual"] {
		validateManualCoverage(matrix.ManualCoverage, rules, add)
	}

	fixturesByID := map[string]Fixture{}
	for _, fixture := range fixtures {
		if fixture.SchemaVersion != schemaVersion {
			add("fixture %q schemaVersion %q, want %q", fixture.ID, fixture.SchemaVersion, schemaVersion)
		}
		if !fixtureIDPattern.MatchString(fixture.ID) {
			add("invalid fixture id %q", fixture.ID)
		}
		if _, exists := fixturesByID[fixture.ID]; exists {
			add("duplicate fixture id %q", fixture.ID)
		}
		fixturesByID[fixture.ID] = fixture
		if fixture.Title == "" || fixture.Provenance == "" {
			add("fixture %q is missing title or provenance", fixture.ID)
		}
		if fixture.Validity != "valid" && fixture.Validity != "invalid" {
			add("fixture %q has invalid validity %q", fixture.ID, fixture.Validity)
		}
		if fixture.RunThrough != "" && !validRunThrough(fixture.RunThrough) {
			add("fixture %q has invalid runThrough %q", fixture.ID, fixture.RunThrough)
		}
		if status := fixture.ExpectedRuntimeStatus; status != "" && status != "halted" && status != "paused" {
			add("fixture %q has invalid expected runtime status %q", fixture.ID, status)
		}
		if fixture.VMPolicy != nil {
			normalized, policyErr := compiler.NormalizeVMPolicy(*fixture.VMPolicy)
			if policyErr != nil {
				add("fixture %q has invalid vmPolicy: %v", fixture.ID, policyErr)
			} else {
				for _, steps := range fixture.RunSteps {
					if steps < 1 || steps > normalized.MaxInstructionsPerRun {
						add("fixture %q runSteps must be between 1 and vmPolicy.maxInstructionsPerRun", fixture.ID)
						break
					}
				}
			}
		}
		if scenario := fixture.DebuggerScenario; scenario != nil {
			if scenario.MaxActions < 1 || scenario.MaxActions > 256 {
				add("fixture %q debugger maxActions must be between 1 and 256", fixture.ID)
			}
			if len(scenario.Actions) < 1 || len(scenario.Actions) > scenario.MaxActions {
				add("fixture %q debugger actions must be non-empty and no greater than maxActions", fixture.ID)
			}
			if scenario.MaxInstructions < 1 || scenario.MaxInstructions > compiler.MaximumVMPolicy().MaxInstructionsPerRun {
				add("fixture %q debugger maxInstructions is out of bounds", fixture.ID)
			}
			if scenario.ReverseLimits != nil {
				limits := *scenario.ReverseLimits
				if limits.CheckpointInterval < 1 || limits.MaxCheckpoints < 1 || limits.MaxCheckpointBytes < 1 || limits.MaxEvents < 1 || limits.MaxEventBytes < 1 {
					add("fixture %q debugger reverseLimits must be positive", fixture.ID)
				}
			}
			allowedActions := map[string]bool{
				"snapshot-pipeline": true, "next-phase": true, "complete": true, "promote": true,
				"set-reverse-limits": true, "set-input": true, "set-interactive": true, "set-watches": true,
				"step-instruction": true, "step-instructions": true, "step-into": true, "step-over": true,
				"step-out": true, "run": true, "run-to-cursor": true, "reset": true,
				"provide-input": true, "finish-input": true, "heap": true, "step-back": true,
				"reverse-continue": true, "scan-source-steps": true, "advance-to-location": true,
				"repeat-wait":  true,
				"public-start": true, "public-pause-run": true, "public-close": true, "public-reset": true,
			}
			for index, action := range scenario.Actions {
				if !allowedActions[action.Op] {
					add("fixture %q debugger action %d has invalid op %q", fixture.ID, index, action.Op)
				}
				if action.Steps < 0 || action.Steps > scenario.MaxInstructions || action.Count < 0 || action.Count > scenario.MaxInstructions {
					add("fixture %q debugger action %d exceeds its instruction bound", fixture.ID, index)
				}
			}
		}
		for _, steps := range fixture.RunSteps {
			if steps < 1 {
				add("fixture %q has non-positive runSteps entry", fixture.ID)
				break
			}
		}
		for _, line := range fixture.Breakpoints {
			if line < 1 {
				add("fixture %q has non-positive breakpoint line", fixture.ID)
				break
			}
		}
		if fixture.ExpectedTraceOffsetMinimum < 0 {
			add("fixture %q has negative expectedTraceOffsetMinimum", fixture.ID)
		}
		if fixture.ExpectedRetainedTraceMaximum != nil && *fixture.ExpectedRetainedTraceMaximum < 0 {
			add("fixture %q has negative expectedRetainedTraceMaximum", fixture.ID)
		}
		if !validLastStage(fixture.ExpectedLastSuccessfulStage) {
			add("fixture %q has invalid expectedLastSuccessfulStage %q", fixture.ID, fixture.ExpectedLastSuccessfulStage)
		}
		if fixture.Validity == "invalid" && len(fixture.ExpectedDiagnostics) == 0 {
			add("invalid fixture %q has no expected diagnostics", fixture.ID)
		}
		if len(fixture.RuleIDs) == 0 {
			add("orphan fixture %q has no ruleIds", fixture.ID)
		}
		if len(fixture.Sources) == 0 {
			add("fixture %q has no sources", fixture.ID)
		}
		base := filepath.Dir(fixture.metadataPath)
		for _, source := range fixture.Sources {
			if !safeRelativePath(source) || filepath.Ext(source) != ".cl" {
				add("fixture %q has unsafe or non-COOL source %q", fixture.ID, source)
				continue
			}
			if info, err := os.Stat(filepath.Join(base, filepath.FromSlash(source))); err != nil || !info.Mode().IsRegular() {
				add("fixture %q source %q is missing or not regular", fixture.ID, source)
			}
		}
		seenRule := map[string]bool{}
		for _, id := range fixture.RuleIDs {
			if seenRule[id] {
				add("fixture %q repeats rule id %q", fixture.ID, id)
			}
			seenRule[id] = true
			if _, exists := rules[id]; !exists {
				add("fixture %q references unknown rule %q", fixture.ID, id)
			}
		}
		for _, diagnostic := range fixture.ExpectedDiagnostics {
			if diagnostic.Code == "" || diagnostic.Stage == "" || (diagnostic.Severity != "error" && diagnostic.Severity != "warning" && diagnostic.Severity != "info") {
				add("fixture %q has incomplete expected diagnostic", fixture.ID)
			}
			if diagnostic.Source != "" && !containsString(fixture.Sources, diagnostic.Source) {
				add("fixture %q expected diagnostic %q names unknown source %q", fixture.ID, diagnostic.Code, diagnostic.Source)
			}
			if diagnostic.Span != nil && !validExpectedSpan(diagnostic.Span) {
				add("fixture %q expected diagnostic %q has an invalid span", fixture.ID, diagnostic.Code)
			}
			for _, related := range diagnostic.Related {
				if strings.TrimSpace(related.Label) == "" {
					add("fixture %q expected diagnostic %q has an unlabeled related span", fixture.ID, diagnostic.Code)
				}
				if related.Source != "" && !containsString(fixture.Sources, related.Source) {
					add("fixture %q expected diagnostic %q related span names unknown source %q", fixture.ID, diagnostic.Code, related.Source)
				}
				if related.Span != nil && !validExpectedSpan(related.Span) {
					add("fixture %q expected diagnostic %q has an invalid related span", fixture.ID, diagnostic.Code)
				}
			}
		}
		for index, token := range fixture.ExpectedTokens {
			if token.Type == "" {
				add("fixture %q expectedTokens[%d] has no type", fixture.ID, index)
			}
			if token.Span != nil && (token.Span.StartLine < 1 || token.Span.StartColumn < 1 || token.Span.EndLine < token.Span.StartLine || token.Span.EndColumn < 1 || (token.Span.EndLine == token.Span.StartLine && token.Span.EndColumn < token.Span.StartColumn) || token.Span.StartOffset < 0 || token.Span.EndOffset < token.Span.StartOffset) {
				add("fixture %q expectedTokens[%d] has an invalid span", fixture.ID, index)
			}
		}
		for index, node := range fixture.ExpectedAST {
			if strings.TrimSpace(node.Kind) == "" {
				add("fixture %q expectedAst[%d] has no kind", fixture.ID, index)
			}
			for _, child := range node.Path {
				if child < 0 {
					add("fixture %q expectedAst[%d] has a negative path component", fixture.ID, index)
					break
				}
			}
			if node.Span != nil && !validExpectedSpan(node.Span) {
				add("fixture %q expectedAst[%d] has an invalid span", fixture.ID, index)
			}
		}
		for _, artifact := range fixture.ExpectedArtifacts {
			if !validArtifact(artifact) {
				add("fixture %q has unknown expected artifact %q", fixture.ID, artifact)
			}
		}
		for _, source := range fixture.ExpectedSourceUnits {
			if !safeRelativePath(source) {
				add("fixture %q has unsafe expected source unit %q", fixture.ID, source)
			}
		}
	}

	for _, rule := range matrix.Rules {
		for _, id := range rule.PositiveFixtures {
			fixture, exists := fixturesByID[id]
			if !exists {
				add("rule %q references missing positive fixture %q", rule.ID, id)
			} else if fixture.Validity != "valid" {
				add("rule %q positive fixture %q is not valid", rule.ID, id)
			}
		}
		for _, id := range rule.NegativeFixtures {
			fixture, exists := fixturesByID[id]
			if !exists {
				add("rule %q references missing negative fixture %q", rule.ID, id)
			} else if fixture.Validity != "invalid" {
				add("rule %q negative fixture %q is not invalid", rule.ID, id)
			}
		}
	}
	for _, fixture := range fixtures {
		references := fixtureReferences[fixture.ID]
		if len(references) == 0 {
			add("orphan fixture %q is not referenced by a matrix rule", fixture.ID)
			continue
		}
		declared := map[string]bool{}
		for _, id := range fixture.RuleIDs {
			declared[id] = true
		}
		for id := range references {
			if !declared[id] {
				add("fixture %q omits reciprocal rule id %q", fixture.ID, id)
			}
		}
		for id := range declared {
			if !references[id] {
				add("fixture %q declares rule %q without a reciprocal matrix reference", fixture.ID, id)
			}
		}
	}
	validateRuleEvidence(matrix.Rules, fixturesByID, add)

	claimIDs := map[string]bool{}
	claimedRules := map[string]bool{}
	for _, claim := range matrix.Claims {
		if claim.ID == "" || claimIDs[claim.ID] {
			add("duplicate or empty claim id %q", claim.ID)
		}
		claimIDs[claim.ID] = true
		if !safeRelativePath(claim.Document) || claim.Contains == "" || len(claim.RuleIDs) == 0 {
			add("claim %q is incomplete", claim.ID)
			continue
		}
		content, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(claim.Document)))
		if err != nil {
			add("claim %q document: %v", claim.ID, err)
		} else if !strings.Contains(string(content), claim.Contains) {
			add("claim %q text %q is missing from %s", claim.ID, claim.Contains, claim.Document)
		}
		for _, id := range claim.RuleIDs {
			if _, exists := rules[id]; !exists {
				add("claim %q references unknown rule %q", claim.ID, id)
			}
			claimedRules[id] = true
		}
	}
	for _, rule := range matrix.Rules {
		if !claimedRules[rule.ID] {
			add("rule %q is not linked to a documented claim", rule.ID)
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("conformance validation failed:\n- %s", strings.Join(problems, "\n- "))
	}
	return nil
}

func validateRuleEvidence(rules []Rule, fixtures map[string]Fixture, add func(string, ...any)) {
	for _, rule := range rules {
		if rule.Status != "supported" {
			continue
		}
		var tokens, ast, diagnostics, runtime bool
		ids := append(append([]string{}, rule.PositiveFixtures...), rule.NegativeFixtures...)
		for _, id := range ids {
			fixture, exists := fixtures[id]
			if !exists {
				continue
			}
			tokens = tokens || len(fixture.ExpectedTokens) > 0
			ast = ast || len(fixture.ExpectedAST) > 0
			diagnostics = diagnostics || len(fixture.ExpectedDiagnostics) > 0
			runtime = runtime || fixture.ExpectedOutput != "" || fixture.ExpectedRuntimeStatus != ""
		}
		switch {
		case strings.HasPrefix(rule.ID, "LEX-") && !tokens:
			add("lexer rule %q has no token evidence", rule.ID)
		case strings.HasPrefix(rule.ID, "PAR-") && !ast:
			add("parser rule %q has no AST evidence", rule.ID)
		case (strings.HasPrefix(rule.ID, "SEM-CLASS-") || strings.HasPrefix(rule.ID, "SEM-EXPR-")) && !ast && !diagnostics:
			add("semantic rule %q has neither typed AST nor diagnostic evidence", rule.ID)
		case (strings.HasPrefix(rule.ID, "RUN-") || strings.HasPrefix(rule.ID, "BUILTIN-")) && !runtime && !diagnostics:
			add("runtime rule %q has neither output/state nor diagnostic evidence", rule.ID)
		}
	}
}

func safeRelativePath(value string) bool {
	if value == "" || path.IsAbs(value) || strings.Contains(value, "\\") || path.Clean(value) != value {
		return false
	}
	if first := strings.SplitN(value, "/", 2)[0]; strings.Contains(first, ":") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." || part == "." || part == "" {
			return false
		}
	}
	return true
}

func validateManualCoverage(coverage []ManualSection, rules map[string]Rule, add func(string, ...any)) {
	if len(coverage) != len(canonicalManualSections) {
		add("manual coverage is missing sections: got %d entries, want %d", len(coverage), len(canonicalManualSections))
	}
	coveredRules := map[string]bool{}
	seenSections := map[string]bool{}
	for index, section := range coverage {
		if seenSections[section.Section] {
			add("manual coverage repeats section %q", section.Section)
		}
		seenSections[section.Section] = true
		if index >= len(canonicalManualSections) {
			add("manual coverage has unexpected section %q", section.Section)
		} else {
			want := canonicalManualSections[index]
			if section.Section != want.Section || section.Title != want.Title {
				add("manual coverage entry %d is %q %q, want %q %q", index, section.Section, section.Title, want.Section, want.Title)
			}
		}
		if strings.TrimSpace(section.Rationale) == "" {
			add("manual coverage section %q has no rationale", section.Section)
		}
		switch section.Applicability {
		case "rules":
			if len(section.RuleIDs) == 0 {
				add("manual coverage section %q maps to no rules", section.Section)
			}
			for _, id := range section.RuleIDs {
				rule, exists := rules[id]
				if !exists {
					add("manual coverage section %q references unknown rule %q", section.Section, id)
					continue
				}
				if rule.Citation.Source == "cool-manual" {
					coveredRules[id] = true
				}
			}
		case "not-applicable":
			if len(section.RuleIDs) > 0 {
				add("manual coverage section %q is not applicable but maps rules", section.Section)
			}
		default:
			add("manual coverage section %q has invalid applicability %q", section.Section, section.Applicability)
		}
	}
	for id, rule := range rules {
		if rule.Citation.Source == "cool-manual" && !coveredRules[id] {
			add("manual rule %q is absent from manual coverage", id)
		}
	}
}

func validExpectedSpan(span *ExpectedSpan) bool {
	return span != nil && span.StartLine >= 1 && span.StartColumn >= 1 && span.EndLine >= span.StartLine && span.EndColumn >= 1 && (span.EndLine != span.StartLine || span.EndColumn >= span.StartColumn) && span.StartOffset >= 0 && span.EndOffset >= span.StartOffset
}

func validLastStage(stage string) bool {
	switch stage {
	case "none", "lexer", "parser", "semantic", "ir", "codegen", "runtime":
		return true
	default:
		return false
	}
}

func validRunThrough(stage string) bool {
	switch stage {
	case "lexer", "parser", "semantic", "ir", "codegen", "runtime":
		return true
	default:
		return false
	}
}

func validArtifact(artifact string) bool {
	switch artifact {
	case "tokens", "ast", "symbols", "ir", "assembly", "controlFlow", "runtimeTrace":
		return true
	default:
		return false
	}
}
