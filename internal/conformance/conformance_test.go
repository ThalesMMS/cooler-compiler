// SPDX-License-Identifier: AGPL-3.0-or-later

package conformance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryCorpus(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	root := filepath.Join(repoRoot, "internal", "compiler", "testdata", "conformance")
	matrix, fixtures, err := Load(root, repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Run(root, repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Rules != len(matrix.Rules) || report.Summary.Fixtures != len(fixtures) || report.Summary.Failed != 0 {
		t.Fatalf("unexpected corpus summary: %+v", report.Summary)
	}
}

func TestValidateRejectsDuplicateAndOrphanFixtures(t *testing.T) {
	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "README.md"), "documented claim")
	fixturePath := filepath.Join(repoRoot, "corpus", "fixture", "fixture.json")
	writeTestFile(t, filepath.Join(filepath.Dir(fixturePath), "main.cl"), "class Main { main() : Object { 0 }; };\n")

	rule := Rule{
		ID: "LEX-ONE", Name: "one", Citation: Citation{Source: "manual", Section: "1"},
		ExpectedStage: "lexer", Behavior: "one", Status: "supported",
		PositiveFixtures: []string{"fixture-one"}, ReferenceComparison: "same",
	}
	matrix := Matrix{
		SchemaVersion: schemaVersion, Project: "test",
		NormativeHierarchy: []NormativeSource{{Rank: 1, ID: "manual", Title: "Manual", Locator: "manual", Role: "normative"}},
		StatusDefinitions: map[string]string{
			"supported": "yes", "partial": "some", "intentional-divergence": "different",
			"unsupported": "no", "not-yet-tested": "unknown",
		},
		Claims: []DocumentClaim{{ID: "claim", Document: "README.md", Contains: "documented claim", RuleIDs: []string{"LEX-ONE"}}},
		Rules:  []Rule{rule},
	}
	fixture := Fixture{
		SchemaVersion: schemaVersion, ID: "fixture-one", Title: "one", RuleIDs: []string{"LEX-ONE"},
		Validity: "valid", ExpectedLastSuccessfulStage: "runtime", Sources: []string{"main.cl"},
		ExpectedTokens: []ExpectedToken{{Type: "OBJECTID", Lexeme: "x"}},
		Provenance:     "original", metadataPath: fixturePath,
	}
	duplicate := fixture
	duplicate.metadataPath = filepath.Join(repoRoot, "corpus", "duplicate", "fixture.json")
	orphan := fixture
	orphan.ID = "fixture-orphan"
	orphan.RuleIDs = nil
	orphan.metadataPath = filepath.Join(repoRoot, "corpus", "orphan", "fixture.json")

	err := validate(matrix, []Fixture{fixture, duplicate, orphan}, filepath.Join(repoRoot, "corpus"), repoRoot)
	if err == nil {
		t.Fatal("validation unexpectedly accepted duplicate and orphan fixtures")
	}
	for _, want := range []string{"duplicate fixture id", "orphan fixture"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("validation error %q does not contain %q", err, want)
		}
	}
}

func TestDecodeStrictRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.json")
	writeTestFile(t, path, `{"schemaVersion":"1.0.0","unexpected":true}`)
	var fixture Fixture
	if err := decodeStrict(path, &fixture); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("decodeStrict error = %v, want unknown field", err)
	}
}

func TestRepositoryManualCoverageIsComplete(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	root := filepath.Join(repoRoot, "internal", "compiler", "testdata", "conformance")
	matrix, fixtures, err := Load(root, repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(matrix.ManualCoverage) != len(canonicalManualSections) {
		t.Fatalf("manual coverage sections = %d, want %d", len(matrix.ManualCoverage), len(canonicalManualSections))
	}
	for _, fixture := range fixtures {
		if fixture.Provenance == "" {
			t.Fatalf("fixture %s has no provenance", fixture.ID)
		}
	}
}

func TestValidateRejectsIncompleteManualCoverageAndDivergence(t *testing.T) {
	matrix := Matrix{
		SchemaVersion: schemaVersion, Project: "test",
		NormativeHierarchy: []NormativeSource{{Rank: 1, ID: "cool-manual", Title: "Manual", Locator: "manual", Role: "normative"}},
		StatusDefinitions: map[string]string{
			"supported": "yes", "partial": "some", "intentional-divergence": "different",
			"unsupported": "no", "not-yet-tested": "unknown",
		},
		Rules: []Rule{{
			ID: "TOOL-MODULES", Name: "modules", Citation: Citation{Source: "cool-manual", Section: "2"},
			ExpectedStage: "parser", Behavior: "none", Status: "intentional-divergence", ReferenceComparison: "none",
		}},
	}
	err := validate(matrix, nil, t.TempDir(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "manual coverage is missing") || !strings.Contains(err.Error(), "incomplete divergence") {
		t.Fatalf("validation error = %v", err)
	}
}

func TestSummaryContainsEveryManualSectionAndRule(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	root := filepath.Join(repoRoot, "internal", "compiler", "testdata", "conformance")
	matrix, fixtures, err := Load(root, repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	summary := RenderSummary(matrix, fixtures)
	for _, section := range matrix.ManualCoverage {
		if !strings.Contains(summary, "| "+section.Section+" | "+section.Title+" |") {
			t.Fatalf("summary omits manual section %s", section.Section)
		}
	}
	for _, rule := range matrix.Rules {
		if !strings.Contains(summary, "| "+rule.ID+" |") {
			t.Fatalf("summary omits rule %s", rule.ID)
		}
	}
}

func TestDebuggerScenarioValidationRejectsUnboundedAndUnknownActions(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	root := filepath.Join(repoRoot, "internal", "compiler", "testdata", "conformance")
	matrix, fixtures, err := Load(root, repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for index := range fixtures {
		if fixtures[index].ID != "debugger-session-valid" {
			continue
		}
		found = true
		fixtures[index].DebuggerScenario.MaxActions = 0
		fixtures[index].DebuggerScenario.Actions[0].Op = "unbounded-mystery"
	}
	if !found {
		t.Fatal("debugger validation fixture is missing")
	}
	err = validate(matrix, fixtures, root, repoRoot)
	if err == nil || !strings.Contains(err.Error(), "maxActions") || !strings.Contains(err.Error(), "invalid op") {
		t.Fatalf("debugger validation error = %v", err)
	}
}

func TestFailingDebuggerScenarioEmitsReplayEvidence(t *testing.T) {
	root := t.TempDir()
	fixturePath := filepath.Join(root, "fixture.json")
	writeTestFile(t, filepath.Join(root, "main.cl"), "class Main { main() : Object { 0 }; };\n")
	fixture := Fixture{
		SchemaVersion: schemaVersion, ID: "debugger-replay-valid", Title: "replay", Provenance: "test",
		RuleIDs: []string{"TOOL-DEBUGGER"}, Validity: "valid", ExpectedLastSuccessfulStage: "runtime",
		Sources: []string{"main.cl"}, metadataPath: fixturePath,
		DebuggerScenario: &DebuggerScenario{MaxActions: 1, MaxInstructions: 100, Actions: []DebuggerAction{{
			Op: "complete", Expect: DebuggerExpectation{StopReason: "halt"},
		}}},
	}
	result := runFixture(fixture)
	if result.Passed || result.Replay == nil || result.Replay.ActionIndex != 1 || len(result.Replay.Actions) != 1 {
		t.Fatalf("replay evidence = %+v", result)
	}
	if result.Replay.Actual.StopReason != "ready" || result.Replay.Actual.PipelinePhase != "runtime" {
		t.Fatalf("replay actual = %+v", result.Replay.Actual)
	}
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
