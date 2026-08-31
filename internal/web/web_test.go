// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"io"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestEmbeddedGUIAssets(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/", "Cooler Compiler Lab"},
		{"/app.js", "loadCoolerApp"},
		{"/app-a.js", "/api/pipeline/start"},
		{"/app-b.js", "renderDiagnostics"},
		{"/app-c.js", "renderConsole"},
		{"/client-utils.js", "concatSources"},
		{"/style.css", ".pipeline"},
		{"/style-tabs.css", ".tab-close"},
		{"/favicon.svg", "<svg"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest("GET", test.path, nil)
			response := httptest.NewRecorder()
			Handler().ServeHTTP(response, request)
			if response.Code != 200 {
				t.Fatalf("status %d", response.Code)
			}
			body, err := io.ReadAll(response.Result().Body)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(body), test.want) {
				t.Fatalf("%s does not contain %q", test.path, test.want)
			}
		})
	}
}

func TestFileExportAndParityControlsAreServed(t *testing.T) {
	request := httptest.NewRequest("GET", "/", nil)
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("status %d", response.Code)
	}
	body := response.Body.String()
	for _, want := range []string{
		`id="recentSelect"`,
		`id="clearSourceButton"`,
		`id="saveAsButton"`,
		`id="exportSelect"`,
		`value="symbols"`,
		`value="assembly"`,
		`value="trace"`,
		`value="zip"`,
		`id="sourceHighlights"`,
		`id="stageGuide"`,
		`src="/client-utils.js"`,
		`id="programInput"`,
		`id="addFileButton"`,
		`id="sourceTabs"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("/ does not contain UI contract %q", want)
		}
	}
	fileInput := regexp.MustCompile(`(?s)<input\b[^>]*\bid="fileInput"[^>]*>`).FindString(body)
	if fileInput == "" {
		t.Fatal(`/ does not contain fileInput`)
	}
	if !regexp.MustCompile(`\baccept="\.cl,\.cool,text/plain"`).MatchString(fileInput) {
		t.Fatalf("fileInput accept mismatch: %s", fileInput)
	}
	if !regexp.MustCompile(`\bmultiple\b`).MatchString(fileInput) {
		t.Fatalf("fileInput missing multiple: %s", fileInput)
	}
}

func TestIncrementalPipelineControlsAreServed(t *testing.T) {
	tests := []struct {
		path string
		want []string
	}{
		{"/", []string{`id="nextPhaseButton"`, "Next: Lexer"}},
		{"/app-a.js", []string{
			"/api/pipeline/start",
			"/api/pipeline/next",
			"/api/pipeline/complete",
			"/api/pipeline/dispose",
		}},
	}
	for _, test := range tests {
		request := httptest.NewRequest("GET", test.path, nil)
		response := httptest.NewRecorder()
		Handler().ServeHTTP(response, request)
		if response.Code != 200 {
			t.Fatalf("%s returned status %d", test.path, response.Code)
		}
		got := response.Body.String()
		for _, want := range test.want {
			if !strings.Contains(got, want) {
				t.Fatalf("%s does not contain %q", test.path, want)
			}
		}
	}
}

func TestIncrementalVMAutoRunClientIsServed(t *testing.T) {
	request := httptest.NewRequest("GET", "/app-a.js", nil)
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("status %d", response.Code)
	}
	for _, want := range []string{
		"vmRunChunkSteps",
		"state.autoRunning",
		"light: true",
		"traceFrom: client.traceEnd",
		"function setRunButtonMode",
		`"pause"`,
		"client.shouldAcceptResponse",
	} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("/app-a.js does not contain incremental run contract %q", want)
		}
	}
}

func TestAboutDialogIsServedWithProjectInformation(t *testing.T) {
	request := httptest.NewRequest("GET", "/", nil)
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, request)

	if response.Code != 200 {
		t.Fatalf("/ returned status %d", response.Code)
	}

	body := response.Body.String()
	for _, want := range []string{
		`id="aboutButton"`,
		`aria-label="About Cooler Compiler Lab"`,
		`<dialog id="aboutDialog"`,
		`aria-labelledby="aboutTitle"`,
		`id="aboutCloseButton"`,
		`https://github.com/ThalesMMS/cooler-compiler`,
		`target="_blank" rel="noreferrer"`,
		`GNU Affero General Public License v3.0 or later`,
		`Thales Matheus M Santos`,
		`Next Phase`,
		`Compile`,
		`Run`,
		`Step`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("/ does not contain About dialog contract %q", want)
		}
	}
}
