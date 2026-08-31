# SPDX-License-Identifier: AGPL-3.0-or-later

APP_NAME := Cooler Compiler Lab
VERSION ?= 0.1.0
LDFLAGS := -s -w -X main.version=$(VERSION)
DIST := dist
BIN := bin
WINDOWS_CC ?= x86_64-w64-mingw32-gcc
WINDOWS_CXX ?= x86_64-w64-mingw32-g++

.PHONY: test vet fmt check release-check build build-all build-windows-native run browser clean macos-app test-macos-quit package

test:
	CGO_ENABLED=0 go test ./...

vet:
	CGO_ENABLED=0 go vet ./...

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './dist/*')

check: test vet
	node --check internal/web/static/about.js
	node --check internal/web/static/client-utils.js
	node --check internal/web/static/app.js
	node --test internal/web/static/about.test.js internal/web/static/client-utils.test.js

release-check:
	./scripts/release-check.sh

build:
	mkdir -p $(BIN)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o "$(BIN)/coolergui" ./cmd/coolergui
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o "$(BIN)/coolerc" ./cmd/coolerc

build-all:
	mkdir -p $(DIST)
	cp LICENSE NOTICE THIRD_PARTY_NOTICES.md "$(DIST)/"
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o "$(DIST)/coolergui-linux-amd64" ./cmd/coolergui
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o "$(DIST)/coolerc-linux-amd64" ./cmd/coolerc
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o "$(DIST)/coolergui-linux-arm64" ./cmd/coolergui
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -o "$(DIST)/coolerc-linux-arm64" ./cmd/coolerc
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o "$(DIST)/coolergui-windows-amd64.exe" ./cmd/coolergui
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o "$(DIST)/coolerc-windows-amd64.exe" ./cmd/coolerc
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o "$(DIST)/coolergui-macos-arm64-browser" ./cmd/coolergui
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -o "$(DIST)/coolerc-macos-arm64" ./cmd/coolerc

build-windows-native:
	mkdir -p $(DIST)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC="$(WINDOWS_CC)" CXX="$(WINDOWS_CXX)" go build -trimpath -ldflags "$(LDFLAGS)" -o "$(DIST)/coolergui-windows-amd64-native.exe" ./cmd/coolergui
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o "$(DIST)/coolerc-windows-amd64.exe" ./cmd/coolerc

run:
	go run ./cmd/coolergui

browser:
	go run ./cmd/coolergui --browser

macos-app:
	DIST_DIR="$(abspath $(DIST))" ./scripts/build-macos-app.sh "$(VERSION)"

test-macos-quit: macos-app
	./scripts/test-macos-quit.sh "$(abspath $(DIST))/Cooler Compiler Lab.app"

package: check build macos-app

clean:
	rm -rf $(BIN) $(DIST) coverage.out
