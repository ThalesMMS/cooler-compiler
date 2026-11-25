// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !js || !wasm

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/ThalesMMS/cooler-compiler/internal/wasmbridge"
)

// The native target generates comparison fixtures for the static experiment.
func main() {
	fixtures, err := wasmbridge.NativeFixtures(context.Background())
	if err == nil {
		err = json.NewEncoder(os.Stdout).Encode(fixtures)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
