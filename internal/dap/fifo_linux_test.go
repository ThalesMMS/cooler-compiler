// SPDX-License-Identifier: AGPL-3.0-or-later
//go:build linux

package dap

import (
	"path/filepath"
	"syscall"
	"testing"
)

func TestLaunchRejectsFIFOWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fifo.cl")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	c := newClient(t)
	c.ok("initialize", nil)
	if c.request("launch", object{"program": path})["success"] != false {
		t.Fatal("FIFO accepted")
	}
	c.ok("disconnect", nil)
}
