// SPDX-License-Identifier: AGPL-3.0-or-later

package lsp

import (
	"context"
	"fmt"
	publiccompiler "github.com/ThalesMMS/cooler-compiler/compiler"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// A nil source set preserves the original whole-workspace behavior.
// Entries are exact relative files or directory prefixes ending in '/'.
type sourceSet struct {
	Include []string `json:"include"`
	Exclude []string `json:"exclude"`
}

func (set *sourceSet) validate() error {
	if set == nil {
		return nil
	}
	if len(set.Include)+len(set.Exclude) > 256 {
		return fmt.Errorf("sourceSet exceeds 256 entries")
	}
	for _, entries := range [][]string{set.Include, set.Exclude} {
		for _, entry := range entries {
			trimmed := strings.TrimSuffix(entry, "/")
			if len(entry) > 4096 || trimmed == "" || trimmed == "." || trimmed == ".." || strings.HasPrefix(trimmed, "../") || path.IsAbs(trimmed) || path.Clean(trimmed) != trimmed || strings.ContainsAny(entry, `\:*?[]`+"\x00") {
				return fmt.Errorf("sourceSet entry %q must be a relative file or directory ending in /", entry)
			}
		}
	}
	return nil
}

func (set *sourceSet) matches(file string, roots []string) bool {
	if set == nil {
		return true
	}
	included := false
	for _, root := range roots {
		relative, err := filepath.Rel(root, file)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		relative = filepath.ToSlash(relative)
		matches := func(entries []string) bool {
			for _, entry := range entries {
				if relative == entry || strings.HasSuffix(entry, "/") && strings.HasPrefix(relative, entry) {
					return true
				}
			}
			return false
		}
		if matches(set.Exclude) {
			return false
		}
		if len(set.Include) == 0 || matches(set.Include) {
			included = true
		}
	}
	return included
}

func (s *Server) changeSourceSet(ctx context.Context, set *sourceSet) error {
	s.mu.Lock()
	warnings := s.warningRules
	s.mu.Unlock()
	return s.changeConfiguration(ctx, set, warnings)
}

func validateWarningRules(rules string) error {
	_, err := publiccompiler.NormalizeOptions(publiccompiler.Options{WarningRules: rules})
	return err
}

func (s *Server) changeConfiguration(ctx context.Context, set *sourceSet, warnings string, hints ...*inlayHintSettings) error {
	if err := validateWarningRules(warnings); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := set.validate(); err != nil {
		return err
	}
	s.mu.Lock()
	roots := append([]string(nil), s.roots...)
	retained := map[string]*document{}
	for uri, doc := range s.documents {
		if doc.Open {
			copy := *doc
			retained[uri] = &copy
		}
	}
	s.mu.Unlock()
	scanCtx, cancel := context.WithTimeout(ctx, s.config.WorkspaceScanTimeout)
	defer cancel()
	documents, err := s.scanWorkspaceSelection(scanCtx, roots, set, retained)
	// Configuration changes are transactional: never replace a working project
	// with a partial scan or discard retained editor contents on failure.
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := scanCtx.Err(); err != nil {
		return err
	}
	for uri, doc := range s.documents {
		if doc.Open {
			documents[uri] = doc
		}
	}
	total := 0
	for _, doc := range documents {
		total += len(doc.Text)
	}
	if len(documents) > s.config.MaxWorkspaceFiles || total > s.config.MaxWorkspaceBytes {
		return fmt.Errorf("sourceSet and retained open buffers exceed workspace limits")
	}
	cleared := []string{}
	for uri, doc := range s.documents {
		if !set.matches(doc.Path, roots) || documents[uri] == nil {
			cleared = append(cleared, uri)
		}
	}
	s.documents = documents
	s.sourceSet = set
	s.warningRules = warnings
	if len(hints) > 0 {
		s.inlayHints = hints[0].apply(s.inlayHints)
	}
	s.invalidateLocked()
	// Serialize the clearing notifications with the generation transition so
	// stale analysis cannot republish diagnostics for the previous exercise.
	sort.Strings(cleared)
	for _, uri := range cleared {
		s.notify("textDocument/publishDiagnostics", publishDiagnosticsParams{URI: uri, Diagnostics: []diagnostic{}})
	}
	s.scheduleDiagnosticsLockedContext(ctx)
	return nil
}
