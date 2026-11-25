// SPDX-License-Identifier: AGPL-3.0-or-later

package lsp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type document struct {
	URI            string
	Path           string
	LanguageID     string
	Version        int
	Text           string
	Open           bool
	Desynchronized bool
}

type documentSnapshot struct {
	URI       string
	Path      string
	Version   int
	Text      string
	Open      bool
	Positions *textPositionIndex
}

var (
	errSourceFileTooLarge = errors.New("source file exceeds configured limit")
	errSourceFileType     = errors.New("source path is not a regular file")
	errWorkspaceScanLimit = errors.New("workspace scan limit exceeded")
)

func (s *Server) initializeWorkspace(parent context.Context, params initializeParams) error {
	if err := validateWarningRules(params.InitializationOptions.WarningRules); err != nil {
		return err
	}
	selection := params.InitializationOptions.SourceSet
	if err := selection.validate(); err != nil {
		return err
	}
	if err := parent.Err(); err != nil {
		return err
	}
	roots := []string{}
	if params.RootURI != "" {
		path, err := pathFromURI(params.RootURI)
		if err != nil {
			return fmt.Errorf("invalid rootUri: %w", err)
		}
		roots = append(roots, path)
	} else if params.RootPath != "" {
		roots = append(roots, params.RootPath)
	}
	for _, folder := range params.WorkspaceFolders {
		path, err := pathFromURI(folder.URI)
		if err != nil {
			return fmt.Errorf("invalid workspace folder URI: %w", err)
		}
		roots = append(roots, path)
	}
	roots, err := uniqueSortedPaths(roots)
	if err != nil {
		return err
	}
	if len(roots) > s.config.MaxWorkspaceRoots {
		return fmt.Errorf("%w: root limit %d exceeded", errWorkspaceScanLimit, s.config.MaxWorkspaceRoots)
	}
	ctx, cancel := context.WithTimeout(parent, s.config.WorkspaceScanTimeout)
	defer cancel()
	documents, err := s.scanWorkspaceSelection(ctx, roots, selection, nil)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errWorkspaceScanLimit) {
		return err
	}
	if err != nil {
		s.logger.Printf("workspace scan: %v", err)
	}
	s.mu.Lock()
	s.roots = roots
	s.sourceSet = selection
	s.warningRules = params.InitializationOptions.WarningRules
	s.inlayHints = params.InitializationOptions.InlayHints.apply(s.inlayHints)
	s.documentChanges = params.Capabilities.Workspace.WorkspaceEdit.DocumentChanges
	for uri, doc := range documents {
		s.documents[uri] = doc
	}
	s.invalidateLocked()
	s.mu.Unlock()
	return nil
}

func (s *Server) scanWorkspace(roots []string) (map[string]*document, error) {
	return s.scanWorkspaceContext(context.Background(), roots)
}

func (s *Server) scanWorkspaceContext(ctx context.Context, roots []string) (map[string]*document, error) {
	s.mu.Lock()
	selection := s.sourceSet
	s.mu.Unlock()
	return s.scanWorkspaceSelection(ctx, roots, selection, nil)
}

func (s *Server) scanWorkspaceSelection(ctx context.Context, roots []string, selection *sourceSet, retained map[string]*document) (map[string]*document, error) {
	result := map[string]*document{}
	totalBytes := 0
	for uri, doc := range retained {
		result[uri] = doc
		totalBytes += len(doc.Text)
	}
	visitedEntries := 0
	var problems []error
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			visitedEntries++
			if visitedEntries > s.config.MaxWorkspaceEntries {
				return fmt.Errorf("%w: entry limit %d exceeded", errWorkspaceScanLimit, s.config.MaxWorkspaceEntries)
			}
			if walkErr != nil {
				problems = append(problems, walkErr)
				return nil
			}
			if entry.IsDir() {
				switch entry.Name() {
				case ".git", "dist", "bin", "vendor", "node_modules":
					if path != root {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if !isCoolPath(path) || !selection.matches(path, roots) {
				return nil
			}
			uri := uriFromPath(path)
			if result[uri] != nil {
				return nil
			}
			if len(result) >= s.config.MaxWorkspaceFiles {
				return fmt.Errorf("%w: file limit %d exceeded", errWorkspaceScanLimit, s.config.MaxWorkspaceFiles)
			}
			info, err := entry.Info()
			if err != nil {
				problems = append(problems, err)
				return nil
			}
			if !info.Mode().IsRegular() {
				problems = append(problems, fmt.Errorf("%w: %s", errSourceFileType, path))
				return nil
			}
			if info.Size() > int64(s.config.MaxFileBytes) || totalBytes+int(info.Size()) > s.config.MaxWorkspaceBytes {
				problems = append(problems, fmt.Errorf("workspace source limit excludes %s", path))
				return nil
			}
			maximum := min(s.config.MaxFileBytes, s.config.MaxWorkspaceBytes-totalBytes)
			content, err := readSourceFile(path, maximum)
			if err != nil {
				problems = append(problems, err)
				return nil
			}
			result[uri] = &document{URI: uri, Path: path, Text: string(content), Version: 0}
			totalBytes += len(content)
			return nil
		})
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errWorkspaceScanLimit) {
			return result, err
		}
		if err != nil {
			problems = append(problems, err)
		}
	}
	return result, errors.Join(problems...)
}

func (s *Server) didOpen(params didOpenParams) {
	s.didOpenContext(context.Background(), params)
}

func (s *Server) didOpenContext(ctx context.Context, params didOpenParams) {
	item := params.TextDocument
	if !isCoolURI(item.URI) {
		return
	}
	if len(item.Text) > s.config.MaxFileBytes {
		s.logger.Printf("didOpen rejected %s: file exceeds %d bytes", item.URI, s.config.MaxFileBytes)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing := s.documents[item.URI]; existing != nil && existing.Open && item.Version <= existing.Version {
		return
	}
	openCount := 0
	for _, doc := range s.documents {
		if doc.Open {
			openCount++
		}
	}
	if current := s.documents[item.URI]; (current == nil || !current.Open) && openCount >= s.config.MaxOpenDocuments {
		s.logger.Printf("didOpen rejected %s: open-document limit %d reached", item.URI, s.config.MaxOpenDocuments)
		return
	}
	if s.documents[item.URI] == nil && len(s.documents) >= s.config.MaxWorkspaceFiles {
		s.logger.Printf("didOpen rejected %s: workspace-file limit %d reached", item.URI, s.config.MaxWorkspaceFiles)
		return
	}
	if len(item.Text) > s.config.MaxWorkspaceBytes-s.workspaceTextBytesLocked(item.URI) {
		s.logger.Printf("didOpen rejected %s: combined workspace source limit %d exceeded", item.URI, s.config.MaxWorkspaceBytes)
		return
	}
	path, err := pathFromURI(item.URI)
	if err != nil {
		return
	}
	s.documents[item.URI] = &document{URI: item.URI, Path: path, LanguageID: item.LanguageID, Version: item.Version, Text: item.Text, Open: true}
	s.invalidateLocked()
	s.scheduleDiagnosticsLockedContext(ctx)
}

func (s *Server) didChange(params didChangeParams) {
	s.didChangeContext(context.Background(), params)
}

func (s *Server) didChangeContext(ctx context.Context, params didChangeParams) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc := s.documents[params.TextDocument.URI]
	if doc == nil || !doc.Open || params.TextDocument.Version <= doc.Version {
		return
	}
	text := doc.Text
	changes := params.ContentChanges
	maximumTextBytes := min(s.config.MaxFileBytes, s.config.MaxWorkspaceBytes-s.workspaceTextBytesLocked(doc.URI))
	if len(changes) > s.config.MaxContentChanges {
		s.logger.Printf("didChange ignored for %s: change limit %d exceeded", doc.URI, s.config.MaxContentChanges)
		s.markDesynchronizedLocked(doc, params.TextDocument.Version)
		return
	}
	if doc.Desynchronized {
		full := -1
		for index := range changes {
			if changes[index].Range == nil {
				full = index
				break
			}
		}
		if full < 0 {
			doc.Version = params.TextDocument.Version
			return
		}
		text = changes[full].Text
		if len(text) > maximumTextBytes {
			s.logger.Printf("didChange ignored for %s: source byte limit exceeded", doc.URI)
			s.markDesynchronizedLocked(doc, params.TextDocument.Version)
			return
		}
		changes = changes[full+1:]
	}
	for _, change := range changes {
		if change.Range == nil {
			text = change.Text
		} else {
			start, err := byteOffsetAt(text, change.Range.Start)
			if err != nil {
				s.logger.Printf("didChange ignored for %s: %v", doc.URI, err)
				s.markDesynchronizedLocked(doc, params.TextDocument.Version)
				return
			}
			end, err := byteOffsetAt(text, change.Range.End)
			if err != nil || end < start {
				s.logger.Printf("didChange ignored for %s: invalid range", doc.URI)
				s.markDesynchronizedLocked(doc, params.TextDocument.Version)
				return
			}
			text = text[:start] + change.Text + text[end:]
		}
		if len(text) > maximumTextBytes {
			s.logger.Printf("didChange ignored for %s: source byte limit exceeded", doc.URI)
			s.markDesynchronizedLocked(doc, params.TextDocument.Version)
			return
		}
	}
	doc.Text = text
	doc.Version = params.TextDocument.Version
	doc.Desynchronized = false
	s.invalidateLocked()
	s.scheduleDiagnosticsLockedContext(ctx)
}

func (s *Server) markDesynchronizedLocked(doc *document, version int) {
	doc.Desynchronized = true
	doc.Version = version
	s.invalidateLocked()
}

func (s *Server) didClose(params didCloseParams) {
	s.didCloseContext(context.Background(), params)
}

func (s *Server) didCloseContext(ctx context.Context, params didCloseParams) {
	s.mu.Lock()
	doc := s.documents[params.TextDocument.URI]
	if doc == nil || !doc.Open {
		s.mu.Unlock()
		return
	}
	path := doc.Path
	s.mu.Unlock()
	var content []byte
	var readErr error
	if path != "" && isCoolPath(path) {
		content, readErr = readSourceFile(path, s.config.MaxFileBytes)
	}
	s.mu.Lock()
	if s.documents[params.TextDocument.URI] != doc || !doc.Open {
		s.mu.Unlock()
		return
	}
	removed := false
	if path != "" && isCoolPath(path) {
		if readErr == nil && s.sourceSet.matches(path, s.roots) && len(content) <= s.config.MaxFileBytes && len(content) <= s.config.MaxWorkspaceBytes-s.workspaceTextBytesLocked(doc.URI) {
			doc.Text = string(content)
			doc.Version = 0
			doc.Open = false
			doc.Desynchronized = false
		} else {
			delete(s.documents, doc.URI)
			removed = true
		}
	} else {
		delete(s.documents, doc.URI)
		removed = true
	}
	s.invalidateLocked()
	s.scheduleDiagnosticsLockedContext(ctx)
	s.mu.Unlock()
	if removed {
		s.notify("textDocument/publishDiagnostics", publishDiagnosticsParams{URI: params.TextDocument.URI, Diagnostics: []diagnostic{}})
	}
}

func (s *Server) didChangeWatchedFiles(params jsonWatchedFiles) {
	s.didChangeWatchedFilesContext(context.Background(), params)
}

func (s *Server) didChangeWatchedFilesContext(ctx context.Context, params jsonWatchedFiles) {
	s.mu.Lock()
	selection, roots := s.sourceSet, append([]string(nil), s.roots...)
	s.mu.Unlock()
	type watchedUpdate struct {
		uri, path string
		kind      int
		content   []byte
	}
	capacity := min(len(params.Changes), s.config.MaxWorkspaceFiles)
	reversed := make([]watchedUpdate, 0, capacity)
	seen := make(map[string]struct{}, capacity)
	totalBytes := 0
	for index := len(params.Changes) - 1; index >= 0; index-- {
		change := params.Changes[index]
		filePath, pathErr := pathFromURI(change.URI)
		if pathErr != nil || !selection.matches(filePath, roots) {
			continue
		}
		if change.Type < 1 || change.Type > 3 || !isCoolURI(change.URI) {
			continue
		}
		if _, exists := seen[change.URI]; exists {
			continue
		}
		if len(seen) >= s.config.MaxWorkspaceFiles {
			s.logger.Printf("watched-file batch truncated at %d unique documents", s.config.MaxWorkspaceFiles)
			break
		}
		seen[change.URI] = struct{}{}
		if change.Type == 3 {
			reversed = append(reversed, watchedUpdate{uri: change.URI, kind: change.Type})
			continue
		}
		path, err := pathFromURI(change.URI)
		if err != nil {
			continue
		}
		maximum := min(s.config.MaxFileBytes, s.config.MaxWorkspaceBytes-totalBytes)
		content, err := readSourceFile(path, maximum)
		if err != nil {
			continue
		}
		totalBytes += len(content)
		reversed = append(reversed, watchedUpdate{uri: change.URI, path: path, kind: change.Type, content: content})
	}
	updates := make([]watchedUpdate, len(reversed))
	for index := range reversed {
		updates[len(reversed)-1-index] = reversed[index]
	}
	s.mu.Lock()
	changed := false
	cleared := []string{}
	for _, update := range updates {
		if update.kind != 3 {
			continue
		}
		if current := s.documents[update.uri]; current == nil || current.Open {
			continue
		}
		delete(s.documents, update.uri)
		changed = true
		cleared = append(cleared, update.uri)
	}
	for _, update := range updates {
		if update.kind == 3 {
			continue
		}
		if !s.sourceSet.matches(update.path, s.roots) {
			continue
		}
		if current := s.documents[update.uri]; current != nil && current.Open {
			continue
		}
		if s.documents[update.uri] == nil && len(s.documents) >= s.config.MaxWorkspaceFiles {
			s.logger.Printf("watched file ignored %s: workspace-file limit %d reached", update.uri, s.config.MaxWorkspaceFiles)
			continue
		}
		if len(update.content) > s.config.MaxWorkspaceBytes-s.workspaceTextBytesLocked(update.uri) {
			s.logger.Printf("watched file ignored %s: combined workspace source limit %d reached", update.uri, s.config.MaxWorkspaceBytes)
			continue
		}
		s.documents[update.uri] = &document{URI: update.uri, Path: update.path, Text: string(update.content)}
		changed = true
	}
	if changed {
		s.invalidateLocked()
		s.scheduleDiagnosticsLockedContext(ctx)
	}
	s.mu.Unlock()
	for _, uri := range cleared {
		s.notify("textDocument/publishDiagnostics", publishDiagnosticsParams{URI: uri, Diagnostics: []diagnostic{}})
	}
}

type jsonWatchedFiles struct {
	Changes []struct {
		URI  string `json:"uri"`
		Type int    `json:"type"`
	} `json:"changes"`
}

func (s *Server) snapshotDocuments() (uint64, []documentSnapshot) {
	generation, documents, _ := s.snapshotAnalysisInputs()
	return generation, documents
}

func (s *Server) snapshotAnalysisInputs() (uint64, []documentSnapshot, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	documents := make([]documentSnapshot, 0, len(s.documents))
	for _, doc := range s.documents {
		if doc.Desynchronized || !s.sourceSet.matches(doc.Path, s.roots) {
			continue
		}
		documents = append(documents, documentSnapshot{
			URI: doc.URI, Path: doc.Path, Version: doc.Version, Text: doc.Text, Open: doc.Open,
			Positions: newTextPositionIndex(doc.Text),
		})
	}
	sort.Slice(documents, func(i, j int) bool { return documents[i].URI < documents[j].URI })
	return s.generation, documents, s.warningRules
}

// workspaceTextBytesLocked returns retained source bytes excluding one URI.
// The caller holds s.mu.
func (s *Server) workspaceTextBytesLocked(exceptURI string) int {
	total := 0
	for uri, doc := range s.documents {
		if uri != exceptURI {
			total += len(doc.Text)
		}
	}
	return total
}

func uniqueSortedPaths(paths []string) ([]string, error) {
	set := map[string]struct{}{}
	for _, path := range paths {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve workspace root %q: %w", path, err)
		}
		set[filepath.Clean(absolute)] = struct{}{}
	}
	result := make([]string, 0, len(set))
	for path := range set {
		result = append(result, path)
	}
	sort.Strings(result)
	return result, nil
}

func isCoolPath(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	return extension == ".cl" || extension == ".cool"
}

func isCoolURI(uri string) bool {
	parsed, err := url.Parse(uri)
	if err != nil || parsed.Scheme != "file" || !isCoolPath(parsed.Path) {
		return false
	}
	_, err = pathFromURI(uri)
	return err == nil
}

func pathFromURI(uri string) (string, error) {
	return pathFromURIForOS(uri, runtime.GOOS)
}

func pathFromURIForOS(uri, goos string) (string, error) {
	parsed, err := url.Parse(uri)
	if err != nil || parsed.Scheme != "file" || parsed.Opaque != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("unsupported document URI %q", uri)
	}
	path := parsed.Path
	host := parsed.Host
	if host != "" && !strings.EqualFold(host, "localhost") {
		if goos != "windows" || strings.Contains(host, ":") {
			return "", fmt.Errorf("unsupported document URI authority %q", host)
		}
		path = "//" + host + path
	} else if goos == "windows" && len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return filepath.FromSlash(path), nil
}

func uriFromPath(path string) string {
	absolute, _ := filepath.Abs(path)
	return uriFromSlashedPath(filepath.ToSlash(absolute), runtime.GOOS)
}

func uriFromSlashedPath(slashed, goos string) string {
	if goos == "windows" && strings.HasPrefix(slashed, "//") {
		remainder := strings.TrimPrefix(slashed, "//")
		host, share, found := strings.Cut(remainder, "/")
		if found && host != "" {
			return (&url.URL{Scheme: "file", Host: host, Path: "/" + share}).String()
		}
	}
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return (&url.URL{Scheme: "file", Path: slashed}).String()
}

func readSourceFile(path string, maximum int) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s", errSourceFileType, path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	openedInfo, statErr := file.Stat()
	if statErr != nil || !openedInfo.Mode().IsRegular() {
		return nil, errors.Join(statErr, file.Close(), fmt.Errorf("%w: %s", errSourceFileType, path))
	}
	content, readErr := readBounded(file, maximum)
	return content, errors.Join(readErr, file.Close())
}

func readBounded(reader io.Reader, maximum int) ([]byte, error) {
	if maximum < 0 {
		return nil, errSourceFileTooLarge
	}
	limited := &io.LimitedReader{R: reader, N: int64(maximum)}
	content, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	var extra [1]byte
	if _, err := io.ReadFull(reader, extra[:]); err == nil {
		return nil, fmt.Errorf("%w: more than %d bytes", errSourceFileTooLarge, maximum)
	} else if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}
	return content, nil
}

func byteOffsetAt(text string, position Position) (int, error) {
	if position.Line < 0 || position.Character < 0 {
		return 0, errors.New("negative LSP position")
	}
	line, character := 0, 0
	for offset := 0; offset < len(text); {
		if line == position.Line && character == position.Character {
			return offset, nil
		}
		r, size := utf8.DecodeRuneInString(text[offset:])
		if r == utf8.RuneError && size == 1 {
			return 0, errors.New("invalid UTF-8 document")
		}
		if r == '\n' {
			if line == position.Line {
				return 0, errors.New("character exceeds line length")
			}
			line++
			character = 0
		} else {
			width := 1
			if r > 0xFFFF {
				width = len(utf16.Encode([]rune{r}))
			}
			if line == position.Line && character+width > position.Character {
				return 0, errors.New("position splits a UTF-16 surrogate pair")
			}
			character += width
		}
		offset += size
	}
	if line == position.Line && character == position.Character {
		return len(text), nil
	}
	return 0, errors.New("position is outside document")
}

func utf16Length(text string) int {
	length := 0
	for _, r := range text {
		if r > 0xFFFF {
			length += 2
		} else {
			length++
		}
	}
	return length
}
