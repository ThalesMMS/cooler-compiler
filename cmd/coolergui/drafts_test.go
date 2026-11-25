// SPDX-License-Identifier: AGPL-3.0-or-later
package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDraftStoreSurvivesRestartAndClear(t *testing.T) {
	dir := t.TempDir()
	store := &draftStore{directory: dir}
	call := func(store *draftStore, body string) (int, draftEnvelope) {
		t.Helper()
		w := httptest.NewRecorder()
		store.handler(w, httptest.NewRequest("POST", "/api/drafts", strings.NewReader(body)))
		var data draftEnvelope
		_ = json.Unmarshal(w.Body.Bytes(), &data)
		return w.Code, data
	}
	status, saved := call(store, `{"action":"save","revision":0,"workspace":{"version":1,"active":1,"tabs":[{"name":"one.cl","content":"first"},{"name":"two.cl","content":"unsaved"}]}}`)
	if status != 200 || saved.Revision != 1 {
		t.Fatalf("save %d %+v", status, saved)
	}
	restarted := &draftStore{directory: dir}
	status, got := call(restarted, `{"action":"read"}`)
	if status != 200 || got.Workspace.Active != 1 || got.Workspace.Tabs[1].Content != "unsaved" {
		t.Fatalf("restart %d %+v", status, got)
	}
	status, _ = call(restarted, `{"action":"save","revision":0,"workspace":{"version":1,"active":0,"tabs":[{"name":"stale.cl","content":"lost"}]}}`)
	if status != 409 {
		t.Fatalf("stale save %d", status)
	}
	orphan := filepath.Join(dir, ".draft-interrupted")
	if err := os.WriteFile(orphan, []byte("interrupted source"), 0600); err != nil {
		t.Fatal(err)
	}
	status, got = call(restarted, `{"action":"clear","revision":1}`)
	if status != 200 || got.Workspace != nil || got.Revision != 2 {
		t.Fatalf("clear %d %+v", status, got)
	}
	data, err := os.ReadFile(filepath.Join(dir, "drafts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("unsaved")) {
		t.Fatal("clear retained source")
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("clear retained interrupted copy: %v", err)
	}
}

func TestDraftStoreRejectsCorruptionLimitsAndUnavailableStorage(t *testing.T) {
	dir := t.TempDir()
	store := &draftStore{directory: dir}
	if err := os.WriteFile(filepath.Join(dir, "drafts.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		body string
		want int
	}{
		{`{"action":"read"}`, 503},
		{`{"action":"clear","revision":0}`, 200},
		{`{"action":"save","revision":1,"workspace":{"version":2,"active":0,"tabs":[]}}`, 400},
		{`{"action":"save","revision":1,"capability":"secret"}`, 400},
		{`{"action":"save","revision":1,"workspace":{"version":1,"active":2,"tabs":[{"name":"one.cl","content":"x"}]}}`, 400},
	} {
		w := httptest.NewRecorder()
		store.handler(w, httptest.NewRequest("POST", "/api/drafts", strings.NewReader(tc.body)))
		if w.Code != tc.want {
			t.Fatalf("%s: %d %s", tc.body, w.Code, w.Body.String())
		}
	}
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	unavailable := &draftStore{directory: blocked}
	w := httptest.NewRecorder()
	unavailable.handler(w, httptest.NewRequest("POST", "/api/drafts", strings.NewReader(`{"action":"read"}`)))
	if w.Code != 503 {
		t.Fatalf("unavailable %d", w.Code)
	}
	workspace := draftWorkspace{Version: 1, Tabs: []draftTab{{Name: "large.cl", Content: strings.Repeat("x", 128*1024+1)}}}
	if validateDraftWorkspace(&workspace) == nil {
		t.Fatal("accepted oversized source")
	}
	var envelope draftEnvelope
	if err := decodeDraftJSON(bytes.NewReader([]byte{0xff}), &envelope); err == nil {
		t.Fatal("accepted invalid UTF-8")
	}
}

func TestDraftStoreIndependentHandlesCannotOverwrite(t *testing.T) {
	dir := t.TempDir()
	lock, err := os.OpenFile(filepath.Join(dir, "drafts.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Close() })
	if err := lockDraftFile(lock); err != nil {
		if err.Error() == "durable draft locking unsupported on this platform" {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	store := &draftStore{directory: dir}
	w := httptest.NewRecorder()
	store.handler(w, httptest.NewRequest("POST", "/api/drafts", strings.NewReader(`{"action":"clear"}`)))
	if w.Code != 503 {
		t.Fatalf("independent handle bypassed lock: %d", w.Code)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	store.handler(w, httptest.NewRequest("POST", "/api/drafts", strings.NewReader(`{"action":"clear"}`)))
	if w.Code != 200 {
		t.Fatalf("released lock remains held: %d", w.Code)
	}
}
