//go:build windows

package tools

import (
	"fmt"
	"os/exec"
	"syscall"
)

// cmdSysProcAttrForCmd builds a SysProcAttr that bypasses Go's argv→command-line
// escaping by setting CmdLine directly. The command is double-wrapped when it starts
// with " so that cmd.exe's /c quote-stripping produces a correctly quoted result.
//
// Without this, Go's exec.Command("cmd", "/c", quotedArg) on Windows would add
// backslash-escaping that cmd.exe misinterprets as literal characters.
func cmdSysProcAttrForCmd(command string) *syscall.SysProcAttr {
	wrappedCmd := wrapCmdCommand(command)
	return &syscall.SysProcAttr{
		CmdLine:        `cmd /c ` + wrappedCmd,
		CreationFlags:  syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

func setPlatformSysProcAttr(cmd *exec.Cmd) {
	// If CmdLine was already set (by getShellCommand for cmd shell),
	// preserve it and only add CreationFlags.
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.CmdLine != "" {
		cmd.SysProcAttr.CreationFlags = syscall.CREATE_NEW_PROCESS_GROUP
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

func killProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	killer := exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprintf("%d", cmd.Process.Pid))
	if err := killer.Run(); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
