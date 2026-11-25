// SPDX-License-Identifier: AGPL-3.0-or-later

package lsp

import "time"

type measurementProbe struct {
	clock  func() time.Duration
	record func(string, time.Duration)
}

func beginMeasurement(observer *measurementProbe, stage string) func() {
	if observer == nil {
		return func() {}
	}
	start := observer.clock()
	return func() { observer.record(stage, observer.clock()-start) }
}
