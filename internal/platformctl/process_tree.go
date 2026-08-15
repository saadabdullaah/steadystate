package platformctl

import (
	"context"
	"errors"
	"os/exec"
	"time"
)

const processTreeShutdownGrace = 15 * time.Second

type commandProcessTree interface {
	afterStart(*exec.Cmd) error
	terminate(*exec.Cmd) error
	close() error
}

// runCommandInProcessTree gives every lifecycle stage an exact OS-owned
// process boundary. This matters on Windows where killing pwsh alone leaves
// docker/build descendants alive with inherited output handles.
func runCommandInProcessTree(ctx context.Context, command *exec.Cmd) error {
	tree, err := newCommandProcessTree(command)
	if err != nil {
		return err
	}
	defer func() { _ = tree.close() }()

	if err := command.Start(); err != nil {
		return err
	}
	if err := tree.afterStart(command); err != nil {
		_ = command.Process.Kill()
		_, _ = waitForCommand(command, processTreeShutdownGrace)
		return err
	}

	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	select {
	case err := <-wait:
		return err
	case <-ctx.Done():
		terminateErr := tree.terminate(command)
		if _, completed := waitForResult(wait, processTreeShutdownGrace); !completed {
			return errors.Join(ctx.Err(), terminateErr, errors.New("process tree did not exit within the shutdown grace period"))
		}
		return errors.Join(ctx.Err(), terminateErr)
	}
}

func waitForCommand(command *exec.Cmd, timeout time.Duration) (error, bool) {
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	return waitForResult(wait, timeout)
}

func waitForResult(wait <-chan error, timeout time.Duration) (error, bool) {
	select {
	case err := <-wait:
		return err, true
	case <-time.After(timeout):
		return nil, false
	}
}
