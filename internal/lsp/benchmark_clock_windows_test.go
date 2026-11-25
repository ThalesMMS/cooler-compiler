// SPDX-License-Identifier: AGPL-3.0-or-later

package lsp

import (
	"math/bits"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// Like Go's testing/testing_windows.go, use QPC for sub-tick intervals.
// time.Time's Windows clock can round short request durations down to zero.
func newBenchmarkClock(t testing.TB) func() time.Duration {
	t.Helper()
	library := syscall.NewLazyDLL("kernel32.dll")
	counter := library.NewProc("QueryPerformanceCounter")
	frequency := library.NewProc("QueryPerformanceFrequency")
	var hz int64
	if ok, _, err := frequency.Call(uintptr(unsafe.Pointer(&hz))); ok == 0 || hz <= 0 {
		t.Fatalf("performance clock frequency: %v", err)
	}
	read := func() int64 {
		var ticks int64
		if ok, _, err := counter.Call(uintptr(unsafe.Pointer(&ticks))); ok == 0 {
			panic(fmtClockError(err))
		}
		return ticks
	}
	base := read()
	return func() time.Duration {
		hi, lo := bits.Mul64(uint64(read()-base), uint64(time.Second))
		nanos, _ := bits.Div64(hi, lo, uint64(hz))
		return time.Duration(nanos)
	}
}

func fmtClockError(err error) string { return "QueryPerformanceCounter failed: " + err.Error() }
