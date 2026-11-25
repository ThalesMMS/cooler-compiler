// SPDX-License-Identifier: AGPL-3.0-or-later
//go:build unix

package main

import (
	"os"
	"syscall"
)

func lockDraftFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}
