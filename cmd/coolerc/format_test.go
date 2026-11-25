// SPDX-License-Identifier: AGPL-3.0-or-later
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ThalesMMS/cooler-compiler/internal/formatter"
)

func TestFormatCLIRejectsOversizedInput(t *testing.T) {
	source := "class Main{main():Object{0};};" + strings.Repeat(" ", formatter.MaxBytes)
	var stdout, stderr bytes.Buffer
	if code := runFormat(nil, strings.NewReader(source), &stdout, &stderr); code != exitError || stdout.Len() != 0 {
		t.Fatalf("oversized stdin: exit=%d output=%d error=%s", code, stdout.Len(), stderr.String())
	}
	path := filepath.Join(t.TempDir(), "large.cl")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if code := runFormat([]string{"-w", path}, strings.NewReader(""), &stdout, &stderr); code != exitError {
		t.Fatalf("oversized file: exit=%d", code)
	}
	preserved, err := os.ReadFile(path)
	if err != nil || string(preserved) != source {
		t.Fatal("oversized file modified", err)
	}
}

func TestFormatCLI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.cl")
	source := `class Main{main():Object{0};};`
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"fmt", "-check", path}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Fatal(code, stderr.String())
	}
	if code := run([]string{"fmt", "-w", path}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatal(code, stderr.String())
	}
	formatted, err := os.ReadFile(path)
	if err != nil || string(formatted) == source {
		t.Fatal("not written", err)
	}
	if code := run([]string{"fmt", "-check", path, path}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatal(code, stderr.String())
	}
	if err := os.WriteFile(path, []byte("class Main {"), 0600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"fmt", "-w", path}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Fatal(code)
	}
	preserved, _ := os.ReadFile(path)
	if string(preserved) != "class Main {" {
		t.Fatal("invalid source changed")
	}
	if code := run([]string{"fmt", "-w", path, path}, strings.NewReader(""), &stdout, &stderr); code != 2 {
		t.Fatal("batch write accepted")
	}
}
