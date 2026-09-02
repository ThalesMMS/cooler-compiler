// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ThalesMMS/cooler-compiler/internal/releaseartifact"
)

func main() {
	input := flag.String("input", "dist/raw", "directory containing raw release binaries")
	output := flag.String("output", "dist/release", "directory for packaged release artifacts")
	version := flag.String("version", "", "release version without a v prefix")
	commit := flag.String("commit", "", "full source commit")
	epoch := flag.Int64("source-date-epoch", 0, "deterministic Unix timestamp")
	requireMacApp := flag.Bool("require-macos-app", false, "require and package the native macOS app bundle")
	skipped := flag.String("skipped-checks", "", "semicolon-separated documented skipped checks")
	flag.Parse()

	options := releaseartifact.Options{
		InputDirectory:  *input,
		OutputDirectory: *output,
		Version:         *version,
		SourceCommit:    *commit,
		SourceDateEpoch: *epoch,
		BuildFlags: []string{
			"-buildvcs=false",
			"-trimpath",
			fmt.Sprintf("-ldflags=-s -w -X main.version=%s -X main.commit=%s -X main.buildDate=%d", *version, *commit, *epoch),
		},
		RequireMacOSApp: *requireMacApp,
	}
	for _, item := range strings.Split(*skipped, ";") {
		if item = strings.TrimSpace(item); item != "" {
			options.SkippedChecks = append(options.SkippedChecks, item)
		}
	}
	manifest, err := releaseartifact.Build(options)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cooler-release:", err)
		os.Exit(1)
	}
	fmt.Printf("built %d deterministic packages and provenance metadata in %s\n", len(manifest.Packages), *output)
}
