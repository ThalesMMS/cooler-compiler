// SPDX-License-Identifier: AGPL-3.0-or-later
//go:build windows

package main

import (
	"os"
	"syscall"
	"unsafe"
)

var draftLockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")

// Windows releases this exclusive byte-range lock when the handle/process closes.
func lockDraftFile(file *os.File) error {
	var overlapped syscall.Overlapped
	ok, _, err := draftLockFileEx.Call(file.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	if ok == 0 {
		return err
	}
	return nil
}
