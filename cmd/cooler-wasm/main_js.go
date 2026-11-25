// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build js && wasm

package main

import (
	"context"
	"syscall/js"

	"github.com/ThalesMMS/cooler-compiler/internal/wasmbridge"
)

func main() {
	bridge := &wasmbridge.Bridge{}
	handle := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) != 1 || args[0].Type() != js.TypeString {
			return `{"error":"expected a JSON string"}`
		}
		return bridge.HandleJSON(context.Background(), args[0].String())
	})
	js.Global().Set("coolerHandle", handle)
	select {}
}
