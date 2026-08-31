// SPDX-License-Identifier: AGPL-3.0-or-later

package examples

import (
	"reflect"
	"strings"
	"testing"
)

func TestCatalog(t *testing.T) {
	catalog, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 13 {
		t.Fatalf("expected 13 examples, got %d", len(catalog))
	}
	for _, example := range catalog {
		if example.ID == "" || example.Title == "" || example.Source == "" {
			t.Fatalf("incomplete example: %+v", example)
		}
	}
}

func TestStackMachineCatalogMetadataAndResources(t *testing.T) {
	catalog, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}

	var stackMachine *Example
	for i := range catalog {
		if catalog[i].ID == "13_stack_machine" {
			stackMachine = &catalog[i]
			break
		}
	}
	if stackMachine == nil {
		t.Fatal("stack-machine example is missing")
	}
	if stackMachine.Title != "Stack machine" {
		t.Fatalf("title %q, want %q", stackMachine.Title, "Stack machine")
	}
	wantTags := []string{"stack-machine", "input", "dispatch"}
	if !reflect.DeepEqual(stackMachine.Tags, wantTags) {
		t.Fatalf("tags %q, want %q", stackMachine.Tags, wantTags)
	}
	publicMetadata := strings.ToLower(strings.Join(append(
		[]string{stackMachine.Title, stackMachine.Description},
		stackMachine.Tags...,
	), " "))
	for _, legacyLabel := range []string{"pa1", "assignment", "compiladores parte 1"} {
		if strings.Contains(publicMetadata, legacyLabel) {
			t.Fatalf("public metadata still contains legacy label %q", legacyLabel)
		}
	}
	if stackMachine.Input == "" {
		t.Fatal("stack-machine program input is missing")
	}
	wantFiles := []string{"13_stack_machine.cl", "atoi.cl"}
	if len(stackMachine.Files) != len(wantFiles) {
		t.Fatalf("files %+v, want %v", stackMachine.Files, wantFiles)
	}
	for i, want := range wantFiles {
		if stackMachine.Files[i].Name != want || stackMachine.Files[i].Source == "" {
			t.Fatalf("file %d is %+v, want named %q with source", i, stackMachine.Files[i], want)
		}
	}
	if !strings.Contains(stackMachine.Source, "Programming Assignment 1\n *  Stack machine interpreter in Cool.") {
		t.Fatal("required source header changed or is missing")
	}
}
