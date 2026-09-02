// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ThalesMMS/cooler-compiler/internal/lsp"
)

var version = "1.0.0"

func main() {
	maxMessage := flag.Int("max-message-bytes", 4<<20, "maximum JSON-RPC payload size")
	maxFiles := flag.Int("max-workspace-files", 1_024, "maximum COOL files in one workspace")
	maxRoots := flag.Int("max-workspace-roots", 32, "maximum initialized workspace roots")
	maxEntries := flag.Int("max-workspace-entries", 100_000, "maximum filesystem entries visited during initialization")
	maxOpen := flag.Int("max-open-documents", 256, "maximum simultaneously open documents")
	maxChanges := flag.Int("max-content-changes", 4_096, "maximum incremental changes in one notification")
	analysisTimeout := flag.Duration("analysis-timeout", 5*time.Second, "maximum time for one analysis")
	scanTimeout := flag.Duration("workspace-scan-timeout", 5*time.Second, "maximum time for initial workspace discovery")
	showVersion := flag.Bool("version", false, "print version to stderr and exit")
	flag.Parse()
	if *showVersion {
		fmt.Fprintf(os.Stderr, "cooler-lsp %s\n", version)
		return
	}
	config := lsp.DefaultConfig()
	config.Version = version
	config.MaxMessageBytes = *maxMessage
	config.MaxWorkspaceFiles = *maxFiles
	config.MaxWorkspaceRoots = *maxRoots
	config.MaxWorkspaceEntries = *maxEntries
	config.MaxOpenDocuments = *maxOpen
	config.MaxContentChanges = *maxChanges
	config.AnalysisTimeout = *analysisTimeout
	config.WorkspaceScanTimeout = *scanTimeout
	os.Exit(lsp.Run(context.Background(), os.Stdin, os.Stdout, os.Stderr, config))
}
