// SPDX-License-Identifier: AGPL-3.0-or-later
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

const maxDraftBytes = 2 << 20

type draftTab struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}
type draftWorkspace struct {
	Version int        `json:"version"`
	Active  int        `json:"active"`
	Tabs    []draftTab `json:"tabs"`
}
type draftEnvelope struct {
	Revision  uint64          `json:"revision"`
	Workspace *draftWorkspace `json:"workspace"`
}
type draftStore struct {
	mu        sync.Mutex
	directory string
}

func validateDraftWorkspace(w *draftWorkspace) error {
	if w == nil || w.Version != 1 || len(w.Tabs) < 1 || len(w.Tabs) > 16 || w.Active < 0 || w.Active >= len(w.Tabs) {
		return errors.New("drafts require version 1, 1–16 tabs and a valid active tab")
	}
	total := 0
	for _, tab := range w.Tabs {
		if len(tab.Name) < 1 || len(tab.Name) > 128 || !utf8.ValidString(tab.Name) || strings.ContainsAny(tab.Name, "\x00\r\n") || !utf8.ValidString(tab.Content) || len(tab.Content) > 128<<10 {
			return errors.New("draft name or content exceeds limits")
		}
		total += len(tab.Content)
	}
	if total > 256<<10 {
		return errors.New("draft contents exceed 256 KiB")
	}
	return nil
}

func decodeDraftJSON(r io.Reader, value any) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if !utf8.Valid(data) {
		return errors.New("draft JSON must be UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("unexpected trailing draft data")
	}
	return nil
}

func (s *draftStore) read() (draftEnvelope, error) {
	if s.directory == "" {
		return draftEnvelope{}, errors.New("draft storage is unavailable")
	}
	if info, err := os.Stat(s.directory); err == nil && !info.IsDir() {
		return draftEnvelope{}, errors.New("draft location is not a directory")
	}
	file, err := os.Open(filepath.Join(s.directory, "drafts.json"))
	if errors.Is(err, os.ErrNotExist) {
		return draftEnvelope{}, nil
	}
	if err != nil {
		return draftEnvelope{}, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return draftEnvelope{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxDraftBytes {
		return draftEnvelope{}, errors.New("invalid draft file")
	}
	var data draftEnvelope
	if err := decodeDraftJSON(io.LimitReader(file, maxDraftBytes+1), &data); err != nil {
		return draftEnvelope{}, err
	}
	if data.Workspace != nil {
		if err := validateDraftWorkspace(data.Workspace); err != nil {
			return draftEnvelope{}, err
		}
	}
	return data, nil
}

func (s *draftStore) write(data draftEnvelope) error {
	if s.directory == "" {
		return errors.New("draft storage is unavailable")
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if len(encoded) > maxDraftBytes {
		return errors.New("encoded draft exceeds limit")
	}
	if err := os.MkdirAll(s.directory, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(s.directory, ".draft-*")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer func() { _ = file.Close(); _ = os.Remove(temp) }()
	if _, err := file.Write(encoded); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temp, filepath.Join(s.directory, "drafts.json"))
}

func (s *draftStore) handler(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Action    string          `json:"action"`
		Revision  uint64          `json:"revision"`
		Workspace *draftWorkspace `json:"workspace"`
	}
	if err := decodeDraftJSON(http.MaxBytesReader(w, r.Body, maxDraftBytes), &request); err != nil {
		writeJSON(w, 400, map[string]string{"error": "Invalid draft request"})
		return
	}
	if request.Action != "read" && request.Action != "save" && request.Action != "clear" {
		writeJSON(w, 400, map[string]string{"error": "Unknown draft action"})
		return
	}
	if request.Action == "save" {
		if err := validateDraftWorkspace(request.Workspace); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
	} else if request.Workspace != nil {
		writeJSON(w, 400, map[string]string{"error": "Only save accepts a workspace"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.directory == "" {
		writeJSON(w, 503, map[string]string{"error": "Draft storage is unavailable"})
		return
	}
	if err := os.MkdirAll(s.directory, 0700); err != nil {
		writeJSON(w, 503, map[string]string{"error": "Cannot open local draft storage"})
		return
	}
	lock, err := os.OpenFile(filepath.Join(s.directory, "drafts.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": "Cannot lock local draft storage"})
		return
	}
	defer func() { _ = lock.Close() }()
	if err := lockDraftFile(lock); err != nil {
		writeJSON(w, 503, map[string]string{"error": "Another process is saving drafts. Recovery saving is paused; reload to retry."})
		return
	}
	current, err := s.read()
	if err != nil && !(request.Action == "clear" && request.Revision == 0) {
		writeJSON(w, 503, map[string]string{"error": "Draft storage cannot be read. Your editor contents are unchanged."})
		return
	}
	if request.Action == "read" {
		writeJSON(w, 200, current)
		return
	}
	if request.Revision != current.Revision {
		writeJSON(w, 409, map[string]string{"error": "Drafts changed in another window. Reload before saving recovery data."})
		return
	}
	if current.Revision == ^uint64(0) {
		writeJSON(w, 503, map[string]string{"error": "Draft revision exhausted"})
		return
	}
	if request.Action == "clear" {
		entries, err := os.ReadDir(s.directory)
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "Cannot clear temporary recovery data"})
			return
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasPrefix(entry.Name(), ".draft-") {
				if err := os.Remove(filepath.Join(s.directory, entry.Name())); err != nil {
					writeJSON(w, 503, map[string]string{"error": "Cannot clear temporary recovery data"})
					return
				}
			}
		}
	}
	next := draftEnvelope{Revision: current.Revision + 1, Workspace: request.Workspace}
	if err := s.write(next); err != nil {
		writeJSON(w, 503, map[string]string{"error": fmt.Sprintf("Could not %s local drafts. Your editor contents are unchanged.", request.Action)})
		return
	}
	writeJSON(w, 200, next)
}
