// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMarkdownLinksAndAnchors(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "one.md", "# One\n\n[ok](two.md#target)\n[bad](missing.md)\n")
	writeTestFile(t, root, "two.md", "# Target\n")
	problems := checkMarkdown(root, filepath.Join(root, "one.md"))
	if len(problems) != 1 {
		t.Fatalf("problems = %v, want one missing target", problems)
	}
	writeTestFile(t, root, "one.md", "# One\n\n[bad](two.md#absent)\n")
	problems = checkMarkdown(root, filepath.Join(root, "one.md"))
	if len(problems) != 1 {
		t.Fatalf("problems = %v, want one missing anchor", problems)
	}
}

func TestMarkdownAnchorsIgnoreFencesAndDisambiguateDuplicates(t *testing.T) {
	anchors := markdownAnchors("# Same\n## Same\n```\n# Hidden\n```\n")
	for _, name := range []string{"same", "same-1"} {
		if !anchors[name] {
			t.Fatalf("missing anchor %q in %v", name, anchors)
		}
	}
	if anchors["hidden"] {
		t.Fatal("code-fence heading became an anchor")
	}
}

func TestSchemaIdentity(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "schemas/good.json", `{"$id":"https://example.test/good.json"}`)
	if problems := checkSchemas(root); len(problems) != 0 {
		t.Fatalf("valid schema problems = %v", problems)
	}
	writeTestFile(t, root, "schemas/bad.json", `{"$id":"https://example.test/wrong.json"}`)
	if problems := checkSchemas(root); len(problems) != 1 {
		t.Fatalf("invalid schema problems = %v, want one", problems)
	}
}

func TestDiagnosticInventoryDetectsStaleCatalog(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "internal/compiler/a.go", `package compiler; const code = "COL1001"`)
	writeTestFile(t, root, "docs/diagnostics.md", diagnosticStart+"\n- `COL9999`\n"+diagnosticEnd)
	if problems := checkDiagnosticInventory(root); len(problems) != 1 {
		t.Fatalf("stale inventory problems = %v, want one", problems)
	}
	writeTestFile(t, root, "docs/diagnostics.md", diagnosticStart+"\n- `COL1001`\n"+diagnosticEnd)
	if problems := checkDiagnosticInventory(root); len(problems) != 0 {
		t.Fatalf("current inventory problems = %v", problems)
	}
}

func writeTestFile(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
