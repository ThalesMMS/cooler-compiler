// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failingWriter struct{ err error }

func (writer failingWriter) Write([]byte) (int, error) { return 0, writer.err }

func TestParseGoBenchmarkOutput(t *testing.T) {
	output := []byte("goos: darwin\ngoarch: arm64\npkg: example/compiler\ncpu: Example CPU\nBenchmarkCompile/small-12  5  120 ns/op  64 B/op  2 allocs/op  12 source-bytes/op\nBenchmarkCompile/small-12  5  100 ns/op  60 B/op  2 allocs/op\nPASS\n")
	samples, cpu, err := parseGoBenchmarkOutput(output)
	if err != nil {
		t.Fatal(err)
	}
	if cpu != "Example CPU" || len(samples) != 2 || samples[0].Benchmark != "BenchmarkCompile/small" || samples[1].Iteration != 2 {
		t.Fatalf("unexpected parse: cpu=%q samples=%+v", cpu, samples)
	}
	if samples[0].Metrics["ns/op"] != 120 || samples[0].Metrics["source-bytes/op"] != 12 {
		t.Fatalf("unexpected metrics: %+v", samples[0].Metrics)
	}
}

func TestReadBaselineRejectsUnknownFieldsAndSchema(t *testing.T) {
	directory := t.TempDir()
	unknown := filepath.Join(directory, "unknown.json")
	if err := os.WriteFile(unknown, []byte(`{"schemaVersion":"1.0.0","unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readBaseline(unknown); err == nil {
		t.Fatal("unknown field was accepted")
	}
	wrong := filepath.Join(directory, "wrong.json")
	if err := os.WriteFile(wrong, []byte(`{"schemaVersion":"2.0.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readBaseline(wrong); err == nil {
		t.Fatal("unknown schema was accepted")
	}
}

func TestMedian(t *testing.T) {
	if got := median([]float64{9, 1, 5}); got != 5 {
		t.Fatalf("odd median = %v", got)
	}
	if got := median([]float64{10, 2}); got != 6 {
		t.Fatalf("even median = %v", got)
	}
}

func TestPrintComparisonPropagatesWriterError(t *testing.T) {
	want := errors.New("write failed")
	if err := printComparison(failingWriter{err: want}, baseline{Name: "base"}, baseline{Name: "candidate"}, 10); !errors.Is(err, want) {
		t.Fatalf("printComparison error = %v, want %v", err, want)
	}
}

func TestRunRejectsUnsafeOptionsBeforeWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.txt")
	err := run([]string{"run", "-count", "0", "-output", path})
	if err == nil || !strings.Contains(err.Error(), "count") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("invalid run wrote output: %v", statErr)
	}
}
