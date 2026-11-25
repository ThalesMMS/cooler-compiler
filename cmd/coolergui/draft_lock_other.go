// SPDX-License-Identifier: AGPL-3.0-or-later
//go:build !windows && !unix

package main

import (
	"errors"
	"os"
)

func lockDraftFile(_ *os.File) error {
	return errors.New("durable draft locking unsupported on this platform")
}
