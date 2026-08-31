//go:build (!darwin && !windows) || (darwin && !cgo) || (windows && !cgo)

// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import "fmt"

func runNativeWindow(url string, debug bool) error {
	return fmt.Errorf("%w: requires macOS or Windows with CGO_ENABLED=1", errNativeWindowUnavailable)
}
