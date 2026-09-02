// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"io"
	"net/http"
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
		{"/app.js", "app-bootstrap.mjs"},
		{"/app-bootstrap.mjs", "bootstrapCoolerApp"},
		{"/app-state.mjs", "invalidateActiveSession"},
		{"/app-main.mjs", "/api/pipeline/start"},
		{"/client-utils.js", "assemblySpans"},
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

func TestFileActionsAreAccessibleIconButtons(t *testing.T) {
	request := httptest.NewRequest("GET", "/", nil)
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("/ returned status %d", response.Code)
	}

	body := response.Body.String()
	tests := []struct {
		id    string
		label string
	}{
		{id: "clearSourceButton", label: "Clear the COOL editor"},
		{id: "openButton", label: "Open one or more .cl files"},
		{id: "addFileButton", label: "Add another .cl file"},
		{id: "saveButton", label: "Save the current .cl file"},
		{id: "saveAsButton", label: "Save with another .cl file name"},
	}
	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			pattern := regexp.MustCompile(`(?s)<button\b[^>]*\bid="` + regexp.QuoteMeta(test.id) + `"[^>]*>(.*?)</button>`)
			match := pattern.FindStringSubmatch(body)
			if match == nil {
				t.Fatalf("/ does not contain button %s", test.id)
			}
			button := match[0]
			for _, want := range []string{
				`class="icon-button file-action-button"`,
				`title="` + test.label + `"`,
				`aria-label="` + test.label + `"`,
				`<svg`,
				`aria-hidden="true"`,
			} {
				if !strings.Contains(button, want) {
					t.Errorf("%s does not contain accessible icon contract %q: %s", test.id, want, button)
				}
			}
		})
	}
}

func TestResponsiveCompilerActionsExposeAccessibleIcons(t *testing.T) {
	request := httptest.NewRequest("GET", "/", nil)
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("/ returned status %d", response.Code)
	}

	body := response.Body.String()
	tests := []struct {
		id    string
		label string
	}{
		{id: "compileButton", label: "Compile"},
		{id: "runButton", label: "Run"},
		{id: "stepButton", label: "Step instruction"},
		{id: "stepIntoButton", label: "Step into"},
		{id: "stepOverButton", label: "Step over"},
		{id: "stepOutButton", label: "Step out"},
		{id: "runToCursorButton", label: "Run to cursor"},
		{id: "stepBackButton", label: "Step back"},
		{id: "reverseContinueButton", label: "Reverse continue"},
		{id: "resetButton", label: "Reset VM"},
	}
	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			pattern := regexp.MustCompile(`(?s)<button\b[^>]*\bid="` + regexp.QuoteMeta(test.id) + `"[^>]*>(.*?)</button>`)
			match := pattern.FindStringSubmatch(body)
			if match == nil {
				t.Fatalf("/ does not contain button %s", test.id)
			}
			button := match[0]
			for _, want := range []string{
				`responsive-action-button`,
				`aria-label="` + test.label + `"`,
				`<svg`,
				`aria-hidden="true"`,
				`class="button-label"`,
			} {
				if !strings.Contains(button, want) {
					t.Errorf("%s does not contain responsive icon contract %q: %s", test.id, want, button)
				}
			}
		})
	}
}

func TestIncrementalPipelineControlsAreServed(t *testing.T) {
	tests := []struct {
		path string
		want []string
	}{
		{"/", []string{`id="nextPhaseButton"`, "Next: Lexer"}},
		{"/app-main.mjs", []string{
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
	request := httptest.NewRequest("GET", "/app-main.mjs", nil)
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
			t.Fatalf("/app-main.mjs does not contain incremental run contract %q", want)
		}
	}
}

func TestJavaScriptEntrypointUsesModulesWithoutTextInjection(t *testing.T) {
	request := httptest.NewRequest("GET", "/app.js", nil)
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("status %d", response.Code)
	}
	body := response.Body.String()
	for _, want := range []string{
		`import("./app-bootstrap.mjs")`,
		"presentEntrypointFailure",
		`setAttribute("role", "alert")`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("/app.js does not contain module entrypoint contract %q", want)
		}
	}
	for _, forbidden := range []string{
		"script.textContent",
		"createElement(\"script\")",
		"eval(",
		"new Function",
		"app-a.js",
		"app-b.js",
		"app-c.js",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("/app.js contains forbidden loader behavior %q", forbidden)
		}
	}
}

func TestModuleAssetsUseJavaScriptContentType(t *testing.T) {
	for _, path := range []string{"/app-bootstrap.mjs", "/app-main.mjs", "/app-state.mjs"} {
		request := httptest.NewRequest("GET", path, nil)
		response := httptest.NewRecorder()
		Handler().ServeHTTP(response, request)
		if response.Code != 200 {
			t.Fatalf("%s returned status %d", path, response.Code)
		}
		if contentType := response.Header().Get("Content-Type"); !strings.Contains(contentType, "javascript") {
			t.Fatalf("%s content type = %q, want JavaScript", path, contentType)
		}
	}
}

func TestCapabilityIsInjectedOnlyIntoInitialHTML(t *testing.T) {
	const capability = "test-process-capability"
	handler := Handler(capability)
	for _, path := range []string{"/", "/index.html"} {
		request := httptest.NewRequest("GET", path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s returned status %d", path, response.Code)
		}
		body := response.Body.String()
		if !strings.Contains(body, `name="cooler-capability" content="`+capability+`"`) {
			t.Fatalf("%s does not contain the injected capability", path)
		}
		if strings.Contains(body, "{{COOLER_CAPABILITY}}") {
			t.Fatalf("%s contains the unexpanded capability placeholder", path)
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s Cache-Control = %q", path, response.Header().Get("Cache-Control"))
		}
	}

	request := httptest.NewRequest("GET", "/app.js", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if strings.Contains(response.Body.String(), capability) {
		t.Fatal("capability leaked into a static JavaScript asset")
	}
}

func TestFrontendKeepsCapabilityInMemoryAndSendsItOnAPIRequests(t *testing.T) {
	tests := []struct {
		path string
		want []string
	}{
		{"/app.js", []string{
			`meta[name="cooler-capability"]`,
			"capabilityMeta?.remove()",
			`Object.defineProperty(window, "CoolerCapability"`,
			"missing process capability",
		}},
		{"/app-main.mjs", []string{
			`"X-Cooler-Capability": window.CoolerCapability || ""`,
			`keepalive: true`,
		}},
	}
	for _, test := range tests {
		request := httptest.NewRequest("GET", test.path, nil)
		response := httptest.NewRecorder()
		Handler().ServeHTTP(response, request)
		for _, want := range test.want {
			if !strings.Contains(response.Body.String(), want) {
				t.Fatalf("%s does not contain %q", test.path, want)
			}
		}
		for _, forbidden := range []string{"localStorage.setItem(\"cooler-capability", "sessionStorage.setItem(\"cooler-capability"} {
			if strings.Contains(response.Body.String(), forbidden) {
				t.Fatalf("%s persists the capability through %q", test.path, forbidden)
			}
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
