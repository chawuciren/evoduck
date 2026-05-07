//go:build windows

package daemon

import (
	"syscall"
	"time"
)

const (
	// StillActive is the exit code for a still running process on Windows
	StillActive uint32 = 259 // STATUS_PENDING

	// Windows process creation flags
	PROCESS_QUERY_INFORMATION = 0x0400
	PROCESS_TERMINATE         = 0x0001
	SYNCHRONIZE               = 0x00100000
	CREATE_NEW_PROCESS_GROUP  = 0x00000200
	DETACHED_PROCESS          = 0x00000008

	// Wait timeout constants
	WAIT_OBJECT_0    uint32 = 0
	WAIT_TIMEOUT     uint32 = 0x00000102
	WAIT_ABANDONED   uint32 = 0x00000080
	INFINITE_TIMEOUT uint32 = 0xFFFFFFFF
)

// IsProcessRunning checks if a process with given PID is running
// Windows implementation using OpenProcess and GetExitCodeProcess
func IsProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}

	handle, err := syscall.OpenProcess(PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(handle)

	var exitCode uint32
	err = syscall.GetExitCodeProcess(handle, &exitCode)
	return err == nil && exitCode == StillActive
}

// TerminateProcess terminates a process with given PID and waits for it to exit
// Windows implementation using TerminateProcess with WaitForSingleObject
func TerminateProcess(pid int) error {
	if pid <= 0 {
		return nil
	}

	// Open process with both TERMINATE and SYNCHRONIZE access rights
	// SYNCHRONIZE allows us to wait for the process to exit
	handle, err := syscall.OpenProcess(PROCESS_TERMINATE|SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(handle)

	// Terminate the process
	if err := syscall.TerminateProcess(handle, 1); err != nil {
		return err
	}

	// Wait for process to actually exit (up to 5 seconds)
	// This ensures the exe is no longer in use before returning
	waitResult, _ := waitForSingleObject(handle, 5000)
	if waitResult == WAIT_TIMEOUT {
		// Process didn't exit within timeout, but we already sent terminate
		// The process should exit eventually
	}
	return nil
}

// waitForSingleObject wraps the Windows WaitForSingleObject API
func waitForSingleObject(handle syscall.Handle, timeoutMs uint32) (uint32, error) {
	// WaitForSingleObject is available via syscall
	// We use lazy DLL loading to avoid build-time dependency issues
	dll := syscall.NewLazyDLL("kernel32.dll")
	proc := dll.NewProc("WaitForSingleObject")
	ret, _, err := proc.Call(uintptr(handle), uintptr(timeoutMs))
	return uint32(ret), err
}

// WaitForProcessExit waits for a process to exit with timeout
func WaitForProcessExit(pid int, timeout time.Duration) bool {
	if pid <= 0 {
		return true
	}

	handle, err := syscall.OpenProcess(SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return true // Process not found, consider it exited
	}
	defer syscall.CloseHandle(handle)

	timeoutMs := uint32(timeout.Milliseconds())
	waitResult, _ := waitForSingleObject(handle, timeoutMs)
	return waitResult == WAIT_OBJECT_0
}

// KillProcess forcefully terminates a process (same as TerminateProcess on Windows)
func KillProcess(pid int) error {
	return TerminateProcess(pid)
}

// DetachedProcessFlags returns Windows process creation flags for detached process
func DetachedProcessFlags() uint32 {
	return CREATE_NEW_PROCESS_GROUP | DETACHED_PROCESS
}

// CreateNewSession returns nil on Windows (not applicable)
// On Unix, this creates a new session with setsid
func CreateNewSession() *syscall.SysProcAttr {
	// Windows doesn't use setsid, return nil
	// The caller should use DetachedProcessFlags() instead
	return nil
}

// GetDaemonSysProcAttr returns the appropriate SysProcAttr for starting daemon processes
// Windows: uses CreationFlags for detached process
// Unix: uses Setsid for new session
func GetDaemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: DetachedProcessFlags(),
	}
}