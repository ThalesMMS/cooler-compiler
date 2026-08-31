// SPDX-License-Identifier: AGPL-3.0-or-later

package examples

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed *.cl
//go:embed stack-machine/*
var sources embed.FS

type ExampleFile struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

type Example struct {
	ID               string        `json:"id"`
	Title            string        `json:"title"`
	Description      string        `json:"description"`
	Path             string        `json:"path"`
	Source           string        `json:"source"`
	Tags             []string      `json:"tags,omitempty"`
	Expected         string        `json:"expected,omitempty"`
	IntentionalError bool          `json:"intentionalError,omitempty"`
	Input            string        `json:"input,omitempty"`
	Files            []ExampleFile `json:"files,omitempty"`
}

type metadata struct {
	title, description, expected string
	tags                         []string
	intentionalError             bool
	input                        string
}

var descriptions = map[string]metadata{
	"01_hello.cl":                    {"Hello, COOL", "Minimal Main class and IO output.", "Hello, COOL!\n", []string{"entry point", "IO"}, false, ""},
	"02_factorial.cl":                {"Recursive factorial", "Method calls, recursion, arithmetic and conditionals.", "5! = 120\n", []string{"recursion", "if"}, false, ""},
	"03_inheritance_dispatch.cl":     {"Dynamic dispatch", "Attributes, inheritance, overriding and SELF_TYPE.", "3\n", []string{"inheritance", "SELF_TYPE"}, false, ""},
	"04_let_while.cl":                {"Let and while", "Lexical bindings, mutation and loop execution.", "sum = 15\n", []string{"let", "while"}, false, ""},
	"05_case_expression.cl":          {"Case expression", "Runtime type selection chooses the most specific branch.", "cat\n", []string{"case", "types"}, false, ""},
	"06_static_dispatch.cl":          {"Static dispatch", "Compare dynamic dispatch with an explicit ancestor dispatch.", "child\nparent\n", []string{"dispatch", "inheritance"}, false, ""},
	"07_strings.cl":                  {"String built-ins", "length, concat and substr in the COOL prelude.", "compiler lab\nlength = 8\ncomp\n", []string{"String", "built-ins"}, false, ""},
	"08_self_type_copy.cl":           {"SELF_TYPE and copy", "Clone an object and inspect its dynamic type.", "7\nBox\n", []string{"SELF_TYPE", "Object"}, false, ""},
	"09_semantic_error.cl":           {"Semantic diagnostics", "Deliberate override, identifier and type errors.", "", []string{"diagnostics", "invalid"}, true, ""},
	"10_runtime_division_by_zero.cl": {"Runtime division error", "A nonconstant divisor reaches zero at runtime.", "about to divide\n", []string{"runtime", "error"}, true, ""},
	"11_dispatch_on_void.cl":         {"Dispatch on void", "Demonstrates COOL's runtime error for an uninitialized object receiver.", "", []string{"runtime", "void"}, true, ""},
	"12_debugger_loop.cl":            {"Debugger loop", "Useful for stepping and line breakpoints.", "0 1 2 3 done\n", []string{"debugger", "breakpoints"}, false, ""},
	"13_stack_machine.cl":            {"Stack machine", "Interactive stack-machine interpreter with A2I conversion.", ">>>>>>>s\n2\n+\n1\n>>>3\n>>>>>>>s\ns\ns\n1\n+\n3\n>>>>>>>4\n>", []string{"stack-machine", "input", "dispatch"}, false, ""},
}

func Catalog() ([]Example, error) {
	entries, err := sources.ReadDir(".")
	if err != nil {
		return nil, err
	}
	result := make([]Example, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".cl") {
			continue
		}
		content, err := sources.ReadFile(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", entry.Name(), err)
		}
		meta := descriptions[entry.Name()]
		id := strings.TrimSuffix(entry.Name(), ".cl")
		ex := Example{
			ID: id, Title: meta.title, Description: meta.description,
			Path: entry.Name(), Source: string(content), Tags: meta.tags,
			Expected: meta.expected, IntentionalError: meta.intentionalError,
			Input: meta.input,
		}
		if entry.Name() == "13_stack_machine.cl" {
			atoi, err := sources.ReadFile("stack-machine/atoi.cl")
			if err != nil {
				return nil, fmt.Errorf("read stack-machine/atoi.cl: %w", err)
			}
			stackTest, err := sources.ReadFile("stack-machine/stack.test")
			if err != nil {
				return nil, fmt.Errorf("read stack-machine/stack.test: %w", err)
			}
			stackSrc := string(content)
			atoiSrc := string(atoi)
			joined := stackSrc
			if !strings.HasSuffix(joined, "\n") {
				joined += "\n"
			}
			joined += atoiSrc
			ex.Source = joined
			ex.Input = string(stackTest)
			ex.Files = []ExampleFile{
				{Name: "13_stack_machine.cl", Source: stackSrc},
				{Name: "atoi.cl", Source: atoiSrc},
			}
		}
		result = append(result, ex)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}
