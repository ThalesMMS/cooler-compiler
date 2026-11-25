// SPDX-License-Identifier: AGPL-3.0-or-later
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	publiccompiler "github.com/ThalesMMS/cooler-compiler/compiler"
	"io"
	"net/http"
	"strings"
)

func reportImportHandler(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodyBytes+1))
	if err != nil || len(data) > maxRequestBodyBytes {
		writeJSON(w, 400, map[string]string{"error": "Report must be at most 1 MiB"})
		return
	}
	report, sources, err := decodeInspectionReport(data)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"report": report, "sources": sources})
}

func decodeInspectionReport(data []byte) (publiccompiler.Report, []publiccompiler.SourceUnit, error) {
	fail := func(message string) (publiccompiler.Report, []publiccompiler.SourceUnit, error) {
		return publiccompiler.Report{}, nil, fmt.Errorf("cannot inspect report: %s", message)
	}
	if len(data) > maxRequestBodyBytes {
		return fail("maximum size is 1 MiB")
	}
	var tree any
	if err := json.Unmarshal(data, &tree); err != nil {
		return fail("invalid JSON")
	}
	count := 0
	var bounded func(any, int) bool
	bounded = func(value any, depth int) bool {
		count++
		if count > 50000 || depth > 64 {
			return false
		}
		switch v := value.(type) {
		case map[string]any:
			for _, item := range v {
				if !bounded(item, depth+1) {
					return false
				}
			}
		case []any:
			for _, item := range v {
				if !bounded(item, depth+1) {
					return false
				}
			}
		}
		return true
	}
	if !bounded(tree, 0) {
		return fail("report exceeds structural limits")
	}
	var extra struct {
		SchemaVersion string                      `json:"schemaVersion"`
		Sources       []publiccompiler.SourceUnit `json:"sources"`
	}
	if err := json.Unmarshal(data, &extra); err != nil || extra.SchemaVersion == "" {
		return fail("a versioned report schema is required")
	}
	result, err := publiccompiler.DecodeResult(data)
	if err != nil {
		return fail(err.Error())
	}
	report := result.Report()
	if len(extra.Sources) > 0 {
		for _, minor := range []string{"0", "1", "2", "3", "4"} {
			if strings.HasPrefix(extra.SchemaVersion, "1."+minor+".") {
				return fail("source extensions require report schema 1.5 or later")
			}
		}
	}
	sources := extra.Sources
	if len(sources) == 0 && len(report.SourceUnits) == 1 && report.Source != "" {
		unit := report.SourceUnits[0]
		sources = []publiccompiler.SourceUnit{{ID: unit.ID, Name: unit.Name, Content: report.Source}}
	}
	if len(sources) > 0 {
		if len(sources) != len(report.SourceUnits) || len(sources) > 16 {
			return fail("source contents do not match report units or exceed 16 files")
		}
		for i, source := range sources {
			unit := report.SourceUnits[i]
			sum := sha256.Sum256([]byte(source.Content))
			if source.ID != unit.ID || source.Name != unit.Name || len(source.Content) != unit.ByteSize || hex.EncodeToString(sum[:]) != unit.SHA256 {
				return fail("source content hash or identity does not match report")
			}
		}
	}
	return report, sources, nil
}
