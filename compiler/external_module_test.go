// SPDX-License-Identifier: AGPL-3.0-or-later

package compiler_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This is intentionally a separate temporary module: Go's internal-package
// rule would hide an accidental facade dependency that same-module tests miss.
func TestExternalModuleCanImportPublicAPI(t *testing.T) {
	packageDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Dir(packageDir)
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Skip("Go toolchain is unavailable")
	}
	rootModule, err := os.ReadFile(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	goDirective := ""
	for _, line := range strings.Split(string(rootModule), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "go" {
			goDirective = fields[1]
			break
		}
	}
	if goDirective == "" {
		t.Fatal("repository go.mod has no go directive")
	}
	// Keep the consumer below a path with whitespace to catch quoting and module
	// replacement assumptions that same-directory package tests cannot expose.
	temporary := filepath.Join(t.TempDir(), "external consumer")
	if err := os.Mkdir(temporary, 0o700); err != nil {
		t.Fatal(err)
	}
	goMod := "module public-api-consumer\n\ngo " + goDirective + "\n\nrequire github.com/ThalesMMS/cooler-compiler v0.0.0\n\nreplace github.com/ThalesMMS/cooler-compiler => " + strconv.Quote(repoRoot) + "\n"
	program := `package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/ThalesMMS/cooler-compiler/compiler"
)

func main() {
	result, err := compiler.Compile(context.Background(), ` + "`class Main inherits IO { main() : Object { out_string(\"external\\n\") }; };`" + `, compiler.DefaultOptions())
	if err != nil { panic(err) }
	fmt.Print(strings.Join(result.Runtime().Output, ""))
}
`
	if err := os.WriteFile(filepath.Join(temporary, "go.mod"), []byte(goMod), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(temporary, "main.go"), []byte(program), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, goBinary, "run", "-mod=mod", ".")
	command.Dir = temporary
	command.Env = append(os.Environ(), "GOWORK=off")
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("external module timed out: %v", ctx.Err())
	}
	if err != nil {
		t.Fatalf("external module failed: %v\n%s", err, output)
	}
	if strings.TrimSpace(string(output)) != "external" {
		t.Fatalf("output %q", output)
	}
}
