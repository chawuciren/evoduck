//go:build !windows

package daemon

import (
	"os"
	"syscall"
	"time"
)

// IsProcessRunning checks if a process with given PID is running
// Unix implementation using signal(0)
func IsProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// Signal(0) is a special signal that does nothing but checks if process exists
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// TerminateProcess terminates a process with given PID and waits for it to exit
// Unix implementation using SIGTERM followed by wait
func TerminateProcess(pid int) error {
	if pid <= 0 {
		return nil
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}

	// Send SIGTERM
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return err
	}

	// Wait for process to exit (up to 5 seconds)
	// This ensures the executable is no longer in use before returning
	WaitForProcessExit(pid, 5*time.Second)
	return nil
}

// KillProcess kills a process with given PID using SIGKILL
func KillProcess(pid int) error {
	if pid <= 0 {
		return nil
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}

	return process.Signal(syscall.SIGKILL)
}

// WaitForProcessExit waits for a process to exit with timeout
func WaitForProcessExit(pid int, timeout time.Duration) bool {
	if pid <= 0 {
		return true
	}

	// Poll until process exits or timeout
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !IsProcessRunning(pid) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return !IsProcessRunning(pid)
}

// CreateNewSession returns SysProcAttr with Setsid=true for creating new session
func CreateNewSession() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setsid: true,
	}
}

// DetachedProcessFlags returns 0 on Unix (not used, use CreateNewSession instead)
// This is defined for cross-platform compatibility in supervisor code
func DetachedProcessFlags() uint32 {
	return 0
}

// GetDaemonSysProcAttr returns the appropriate SysProcAttr for starting daemon processes
// Unix: uses Setsid for new session
func GetDaemonSysProcAttr() *syscall.SysProcAttr {
	return CreateNewSession()
}