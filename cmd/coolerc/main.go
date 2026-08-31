// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ThalesMMS/cooler-compiler/internal/compiler"
)

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("coolerc", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "write the complete compilation result as JSON")
	showIR := flags.Bool("ir", false, "print generated stack IR instead of program output")
	showAssembly := flags.Bool("assembly", false, "print pseudo-assembly instead of program output")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "usage: coolerc [options] [file.cl ...]")
		flags.PrintDefaults()
	}

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		flags.Usage()
		return exitUsage
	}
	if enabledCount(*jsonOutput, *showIR, *showAssembly) > 1 {
		flags.Usage()
		fmt.Fprintln(stderr, "coolerc: -json, -ir, and -assembly are mutually exclusive")
		return exitUsage
	}

	name := "<stdin>"
	var source []byte
	var programInput string
	var files []sourceFile
	var err error
	if flags.NArg() >= 1 {
		chunks := make([][]byte, flags.NArg())
		names := make([]string, flags.NArg())
		for i, path := range flags.Args() {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				fmt.Fprintf(stderr, "coolerc: %v\n", readErr)
				return exitError
			}
			chunks[i] = data
			names[i] = path
		}
		var joined string
		joined, files = concatSources(chunks, names)
		source = []byte(joined)
		if stdinIsCharDevice(stdin) {
			programInput = ""
		} else {
			inputBytes, readErr := io.ReadAll(stdin)
			if readErr != nil {
				fmt.Fprintf(stderr, "coolerc: could not read program input: %v\n", readErr)
				return exitError
			}
			programInput = string(inputBytes)
		}
	} else {
		source, err = io.ReadAll(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "coolerc: could not read %s: %v\n", name, err)
			return exitError
		}
	}

	var result compiler.Result
	if flags.NArg() >= 1 {
		result = compiler.CompileWithInput(string(source), programInput)
	} else {
		result = compiler.Compile(string(source))
	}
	writeDiagnostics(stderr, name, files, result.Diagnostics)

	switch {
	case *jsonOutput:
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			fmt.Fprintf(stderr, "coolerc: could not write JSON: %v\n", err)
			return exitError
		}
	case *showIR:
		for _, instruction := range result.IR {
			fmt.Fprintf(stdout, "%04d %-24s %-16s %s\n", instruction.Index, instruction.Method, instruction.Op, strings.Join(instruction.Args, ", "))
		}
	case *showAssembly:
		for _, line := range result.Assembly {
			fmt.Fprintln(stdout, line)
		}
	default:
		fmt.Fprint(stdout, strings.Join(result.Runtime.Output, ""))
	}

	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Severity == compiler.SeverityError {
			return exitError
		}
	}
	return exitOK
}

type sourceFile struct {
	name      string
	startLine int
}

func concatSources(chunks [][]byte, names []string) (string, []sourceFile) {
	var b strings.Builder
	files := make([]sourceFile, 0, len(chunks))
	lines := 0
	endsWithNewline := false
	for i, chunk := range chunks {
		if b.Len() > 0 && !endsWithNewline {
			b.WriteByte('\n')
			lines++
			endsWithNewline = true
		}
		startLine := lines + 1
		files = append(files, sourceFile{name: names[i], startLine: startLine})
		b.Write(chunk)
		if len(chunk) > 0 {
			lines += strings.Count(string(chunk), "\n")
			endsWithNewline = chunk[len(chunk)-1] == '\n'
		}
	}
	return b.String(), files
}

func mapDiagnosticLocation(files []sourceFile, fallback string, line int) (string, int) {
	if len(files) == 0 {
		return fallback, line
	}
	if line <= 0 {
		return files[0].name, line
	}
	chosen := files[0]
	for _, file := range files[1:] {
		if file.startLine <= line {
			chosen = file
		}
	}
	return chosen.name, line - chosen.startLine + 1
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

func writeDiagnostics(w io.Writer, name string, files []sourceFile, diagnostics []compiler.Diagnostic) {
	for _, diagnostic := range diagnostics {
		fileName, line := mapDiagnosticLocation(files, name, diagnostic.Line)
		fmt.Fprintf(w, "%s:%d:%d: %s %s: %s\n", fileName, line, diagnostic.Column, diagnostic.Severity, diagnostic.Code, diagnostic.Message)
		if diagnostic.Hint != "" {
			fmt.Fprintf(w, "  hint: %s\n", diagnostic.Hint)
		}
	}
}
