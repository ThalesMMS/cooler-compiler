# SPDX-License-Identifier: AGPL-3.0-or-later

APP_NAME := Cooler Compiler Lab
VERSION ?= 1.0.0
COMMIT ?= $(shell git rev-parse --verify HEAD 2>/dev/null)
SOURCE_DATE_EPOCH ?= $(shell git show -s --format=%ct HEAD 2>/dev/null)
BUILD_DATE ?= $(SOURCE_DATE_EPOCH)
GO_BUILD_FLAGS := -buildvcs=false -trimpath
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)
DIST := dist
BIN := bin
CONFORMANCE_REPORT ?= $(DIST)/conformance-report.json
WINDOWS_CC ?= x86_64-w64-mingw32-gcc
WINDOWS_CXX ?= x86_64-w64-mingw32-g++
DURATION ?= 10m
FUZZ_SMOKE_DURATION ?= 1s
FUZZ_WORKERS ?= 4
SESSION_STRESS_COUNT ?= 20
SESSION_STRESS_TIMEOUT ?= 5m
BENCH_COUNT ?= 3
BENCH_TIME ?= 100ms
BENCH_WEB_ITERATIONS ?= 100
BENCH_NAME ?= local
BENCH_THRESHOLD ?= 10
PROFILE_TARGET ?= compile-large
PROFILE_TIME ?= 3s
PROFILE_DIR ?= dist/profiles

.PHONY: test vet fmt lint check docs-check conformance differential fuzz-smoke fuzz property-test session-stress benchmark benchmark-save benchmark-compare benchmark-smoke profile release-check release-artifacts build build-all build-windows-native run browser clean macos-app test-macos-quit package

test:
	CGO_ENABLED=0 go test ./...

vet:
	CGO_ENABLED=0 go vet ./...

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './dist/*')

lint:
	bash ./scripts/run-lint.sh

check: test vet
	find internal/web/static -type f \( -name '*.js' -o -name '*.mjs' \) -print0 | xargs -0 -n1 node --check
	node --test internal/web/static/about.test.js internal/web/static/client-utils.test.js internal/web/static/app-modules.test.mjs

docs-check:
	go run ./cmd/cooler-doc-check

conformance:
	go run ./cmd/cooler-conformance -report "$(CONFORMANCE_REPORT)"

differential:
	go run ./cmd/cooler-differential $(if $(RULE),-rule "$(RULE)",) $(if $(FIXTURE),-fixture "$(FIXTURE)",) $(if $(filter 1 true yes,$(STRICT)),-strict,)

fuzz-smoke:
	FUZZ_WORKERS="$(FUZZ_WORKERS)" bash ./scripts/run-fuzz.sh all "$(FUZZ_SMOKE_DURATION)"

fuzz:
	FUZZ_WORKERS="$(FUZZ_WORKERS)" bash ./scripts/run-fuzz.sh "$(TARGET)" "$(DURATION)"

property-test:
	go test -count=1 ./internal/compiler -run '^TestGenerated(TypedPrograms|ProgramsMetamorphicTransformations)$$'

session-stress:
	bash ./scripts/run-session-stress.sh "$(SESSION_STRESS_COUNT)" "$(SESSION_STRESS_TIMEOUT)"

benchmark:
	go run ./cmd/cooler-bench run -count "$(BENCH_COUNT)" -benchtime "$(BENCH_TIME)" -web-iterations "$(BENCH_WEB_ITERATIONS)" -name "$(BENCH_NAME)"

benchmark-save:
	go run ./cmd/cooler-bench run -count "$(BENCH_COUNT)" -benchtime "$(BENCH_TIME)" -web-iterations "$(BENCH_WEB_ITERATIONS)" -name "$(BENCH_NAME)" -save-dir benchmarks/baselines

benchmark-compare:
	go run ./cmd/cooler-bench compare -base "$(BASE)" -new "$(NEW)" -threshold "$(BENCH_THRESHOLD)"

benchmark-smoke:
	go run ./cmd/cooler-bench run -count 1 -benchtime 1x -web-iterations 2 -name smoke >/dev/null

profile:
	bash ./scripts/profile-benchmark.sh "$(PROFILE_TARGET)" "$(PROFILE_TIME)" "$(PROFILE_DIR)"

release-check:
	bash ./scripts/release-check.sh

release-artifacts:
	VERSION="$(VERSION)" COMMIT="$(COMMIT)" SOURCE_DATE_EPOCH="$(SOURCE_DATE_EPOCH)" bash ./scripts/build-release.sh "$(DIST)"

build:
	mkdir -p $(BIN)
	CGO_ENABLED=0 go build $(GO_BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o "$(BIN)/coolergui" ./cmd/coolergui
	CGO_ENABLED=0 go build $(GO_BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o "$(BIN)/coolerc" ./cmd/coolerc
	CGO_ENABLED=0 go build $(GO_BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o "$(BIN)/cooler-lsp" ./cmd/cooler-lsp

build-all:
	mkdir -p $(DIST)
	cp LICENSE NOTICE THIRD_PARTY_NOTICES.md "$(DIST)/"
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build $(GO_BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o "$(DIST)/coolergui-linux-amd64" ./cmd/coolergui
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build $(GO_BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o "$(DIST)/coolerc-linux-amd64" ./cmd/coolerc
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build $(GO_BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o "$(DIST)/cooler-lsp-linux-amd64" ./cmd/cooler-lsp
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build $(GO_BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o "$(DIST)/coolergui-linux-arm64" ./cmd/coolergui
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build $(GO_BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o "$(DIST)/coolerc-linux-arm64" ./cmd/coolerc
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build $(GO_BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o "$(DIST)/cooler-lsp-linux-arm64" ./cmd/cooler-lsp
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build $(GO_BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o "$(DIST)/coolergui-windows-amd64.exe" ./cmd/coolergui
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build $(GO_BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o "$(DIST)/coolerc-windows-amd64.exe" ./cmd/coolerc
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build $(GO_BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o "$(DIST)/cooler-lsp-windows-amd64.exe" ./cmd/cooler-lsp
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build $(GO_BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o "$(DIST)/coolergui-macos-arm64-browser" ./cmd/coolergui
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build $(GO_BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o "$(DIST)/coolerc-macos-arm64" ./cmd/coolerc
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build $(GO_BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o "$(DIST)/cooler-lsp-macos-arm64" ./cmd/cooler-lsp

build-windows-native:
	mkdir -p $(DIST)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC="$(WINDOWS_CC)" CXX="$(WINDOWS_CXX)" go build $(GO_BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o "$(DIST)/coolergui-windows-amd64-native.exe" ./cmd/coolergui
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build $(GO_BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o "$(DIST)/coolerc-windows-amd64.exe" ./cmd/coolerc

run:
	go run ./cmd/coolergui

browser:
	go run ./cmd/coolergui --browser

macos-app:
	DIST_DIR="$(abspath $(DIST))" COMMIT="$(COMMIT)" SOURCE_DATE_EPOCH="$(SOURCE_DATE_EPOCH)" bash ./scripts/build-macos-app.sh "$(VERSION)"

test-macos-quit: macos-app
	bash ./scripts/test-macos-quit.sh "$(abspath $(DIST))/Cooler Compiler Lab.app"

package: check build macos-app

clean:
	rm -rf $(BIN) $(DIST) coverage.out
