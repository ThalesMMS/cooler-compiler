// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !windows

package lsp

import (
	"testing"
	"time"
)

func newBenchmarkClock(_ testing.TB) func() time.Duration {
	base := time.Now()
	return func() time.Duration { return time.Since(base) }
}
