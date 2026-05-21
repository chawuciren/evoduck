//go:build !windows

package tools

import (
	"os/exec"
	"syscall"
)

func cmdSysProcAttrForCmd(command string) *syscall.SysProcAttr {
	return nil // never called on non-Windows; gated by runtime.GOOS check
}

func setPlatformSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
