// SPDX-License-Identifier: AGPL-3.0-or-later
//go:build windows

package conformance

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"time"
)

func configureDifferentialProcess(_ *exec.Cmd) {}

func terminateDifferentialProcess(command *exec.Cmd) error {
	if command.Process == nil {
		return os.ErrProcessDone
	}
	pid := strconv.Itoa(command.Process.Pid)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	treeErr := exec.CommandContext(ctx, "taskkill", "/T", "/F", "/PID", pid).Run()
	processErr := command.Process.Kill()
	if treeErr == nil || processErr == nil || errors.Is(processErr, os.ErrProcessDone) {
		return nil
	}
	return errors.Join(treeErr, processErr)
}
