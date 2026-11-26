// SPDX-License-Identifier: AGPL-3.0-or-later

// Command cooler-lexgen compiles a Flex-style lexer specification into Go
// (see internal/lexgen). It is a development-time tool run by
// `make generate-frontend`; builds and apps use the committed output.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ThalesMMS/cooler-compiler/internal/lexgen"
)

func main() {
	input := flag.String("i", "internal/compiler/cool.lex", "lexer specification")
	output := flag.String("o", "internal/compiler/lexer_generated.go", "generated Go file")
	flag.Parse()
	source, err := os.ReadFile(*input)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	generated, err := lexgen.Generate(source, filepath.Base(*input))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", *input, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*output, generated, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
