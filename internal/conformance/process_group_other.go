// SPDX-License-Identifier: AGPL-3.0-or-later
//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package conformance

import (
	"os"
	"os/exec"
)

func configureDifferentialProcess(_ *exec.Cmd) {}

func terminateDifferentialProcess(command *exec.Cmd) error {
	if command.Process == nil {
		return os.ErrProcessDone
	}
	return command.Process.Kill()
}
