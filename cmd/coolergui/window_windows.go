//go:build windows && cgo

// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"log"

	"github.com/webview/webview_go"
)

func runNativeWindow(url string, debug bool) error {
	log.Println("Starting native window with WebView2...")

	w := webview.New(debug)
	defer w.Destroy()

	w.SetTitle("Cooler Compiler")
	w.SetSize(1280, 800, webview.HintNone)
	w.SetSize(980, 640, webview.HintMin)
	w.Navigate(url)

	w.Run()
	return nil
}
