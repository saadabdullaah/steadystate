//go:build !windows

package platformctl

import (
	"errors"
	"os/exec"
	"syscall"
)

type unixCommandProcessTree struct{}

func newCommandProcessTree(command *exec.Cmd) (commandProcessTree, error) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return &unixCommandProcessTree{}, nil
}

func (*unixCommandProcessTree) afterStart(_ *exec.Cmd) error { return nil }

func (*unixCommandProcessTree) terminate(command *exec.Cmd) error {
	err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func (*unixCommandProcessTree) close() error { return nil }
