// SPDX-License-Identifier: AGPL-3.0-or-later
//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package conformance

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureDifferentialProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateDifferentialProcess(command *exec.Cmd) error {
	if command.Process == nil {
		return os.ErrProcessDone
	}
	groupErr := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	processErr := command.Process.Kill()
	if groupErr == nil || processErr == nil || errors.Is(processErr, os.ErrProcessDone) {
		return nil
	}
	return errors.Join(groupErr, processErr)
}
