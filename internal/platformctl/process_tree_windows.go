//go:build windows

package platformctl

import (
	"fmt"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsCommandProcessTree struct {
	job windows.Handle
}

func newCommandProcessTree(_ *exec.Cmd) (commandProcessTree, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create lifecycle process boundary: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("configure lifecycle process boundary: %w", err)
	}
	return &windowsCommandProcessTree{job: job}, nil
}

func (tree *windowsCommandProcessTree) afterStart(command *exec.Cmd) error {
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(command.Process.Pid))
	if err != nil {
		return fmt.Errorf("open lifecycle process: %w", err)
	}
	defer func() { _ = windows.CloseHandle(process) }()
	if err := windows.AssignProcessToJobObject(tree.job, process); err != nil {
		return fmt.Errorf("assign lifecycle process boundary: %w", err)
	}
	return nil
}

func (tree *windowsCommandProcessTree) terminate(_ *exec.Cmd) error {
	if err := windows.TerminateJobObject(tree.job, 1); err != nil {
		return fmt.Errorf("terminate lifecycle process tree: %w", err)
	}
	return nil
}

func (tree *windowsCommandProcessTree) close() error {
	if tree.job == 0 {
		return nil
	}
	err := windows.CloseHandle(tree.job)
	tree.job = 0
	return err
}
