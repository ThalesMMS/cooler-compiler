// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCLIWarningSelection(t *testing.T) {
	for _, rules := range []string{"all", "none", "COL3180"} {
		var out, errOut bytes.Buffer
		code := run([]string{"check", "--warnings=" + rules}, strings.NewReader(`class Main { main():Object {let unused:Int in 0}; };`), &out, &errOut)
		if code != exitOK {
			t.Fatalf("%s: code=%d %s", rules, code, errOut.String())
		}
		if strings.Contains(errOut.String(), "COL3180") != (rules != "none") {
			t.Fatalf("%s: %s", rules, errOut.String())
		}
		if rules != "none" && !strings.Contains(errOut.String(), "warning") {
			t.Fatal("warning severity absent")
		}
	}
}

func TestCLIRejectsUnknownWarningBeforeReadingSource(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"check", "--warnings=unknown"}, strings.NewReader(""), &out, &errOut); code != exitUsage {
		t.Fatalf("invalid warning code=%d want=%d: %s", code, exitUsage, errOut.String())
	}
}
