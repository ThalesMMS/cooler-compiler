// SPDX-License-Identifier: AGPL-3.0-or-later

package lsp

import (
	"context"
	"testing"
)

func TestLSPUnusedWarningLocationAndSelection(t *testing.T) {
	uri := "file:///workspace/warning.cl"
	source := `class Main { main():Object {let unused:Int in 0}; };`
	server := testServer(map[string]documentSnapshot{uri: {URI: uri, Path: "/workspace/warning.cl", Text: source, Open: true, Version: 3}})
	defer server.stopBackground()
	result, err := server.analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Report.Diagnostics) != 1 {
		t.Fatalf("%+v", result.Report.Diagnostics)
	}
	diagnostic := lspDiagnostic(result.Documents, uri, result.Report.Diagnostics[0])
	if diagnostic.Severity != 2 || diagnostic.Code != "COL3180" || diagnostic.Range.Start != nthPosition(source, "unused", 1) {
		t.Fatalf("%+v", diagnostic)
	}
	if err := server.changeConfiguration(context.Background(), nil, "none"); err != nil {
		t.Fatal(err)
	}
	result, err = server.analyze(context.Background())
	if err != nil || len(result.Report.Diagnostics) != 0 {
		t.Fatalf("suppression: %v %+v", err, result)
	}
}
