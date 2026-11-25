// SPDX-License-Identifier: AGPL-3.0-or-later
package main

import (
	"flag"
	"fmt"
	"github.com/ThalesMMS/cooler-compiler/internal/formatter"
	"io"
	"os"
	"path/filepath"
)

func runFormat(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("fmt", flag.ContinueOnError)
	flags.SetOutput(stderr)
	write := flags.Bool("w", false, "atomically replace one source file")
	check := flags.Bool("check", false, "check files without writing; exit 1 if formatting differs")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}
	files := flags.Args()
	if len(files) == 0 {
		files = []string{"-"}
	}
	if *write && (*check || len(files) != 1 || files[0] == "-") || !*check && len(files) > 1 {
		if _, writeErr := fmt.Fprintln(stderr, "fmt: use -check for multiple files; -w accepts exactly one file"); writeErr != nil {
			return exitTool
		}
		return exitUsage
	}
	changed := false
	for _, path := range files {
		var content []byte
		var mode os.FileMode
		if path == "-" {
			data, err := io.ReadAll(io.LimitReader(stdin, formatter.MaxBytes+1))
			if err != nil {
				if _, writeErr := fmt.Fprintln(stderr, err); writeErr != nil {
					return exitTool
				}
				return exitTool
			}
			content = data
		} else {
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() || info.Size() > formatter.MaxBytes {
				if _, writeErr := fmt.Fprintln(stderr, "fmt: source must be a regular file of at most 1 MiB:", path); writeErr != nil {
					return exitTool
				}
				return exitError
			}
			mode = info.Mode()
			file, err := os.Open(path)
			if err != nil {
				if _, writeErr := fmt.Fprintln(stderr, err); writeErr != nil {
					return exitTool
				}
				return exitTool
			}
			data, readErr := io.ReadAll(io.LimitReader(file, formatter.MaxBytes+1))
			closeErr := file.Close()
			if readErr != nil || closeErr != nil {
				if _, writeErr := fmt.Fprintln(stderr, "fmt: read failed", path); writeErr != nil {
					return exitTool
				}
				return exitTool
			}
			content = data
		}
		result, err := formatter.Format(string(content))
		if err != nil {
			if _, writeErr := fmt.Fprintln(stderr, path+":", err); writeErr != nil {
				return exitTool
			}
			return exitError
		}
		if result != string(content) {
			changed = true
		}
		if *check {
			continue
		}
		if *write {
			if result != string(content) {
				if err := writeFormattedFile(path, result, mode); err != nil {
					if _, writeErr := fmt.Fprintln(stderr, err); writeErr != nil {
						return exitTool
					}
					return exitTool
				}
			}
		} else {
			if _, err := io.WriteString(stdout, result); err != nil {
				return exitTool
			}
		}
	}
	if *check && changed {
		return exitError
	}
	return exitOK
}

func writeFormattedFile(path, text string, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".cooler-fmt-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer func() { _ = os.Remove(name) }()
	if err = file.Chmod(mode.Perm()); err == nil {
		_, err = file.WriteString(text)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}
