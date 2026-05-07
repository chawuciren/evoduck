//go:build !windows

package tools

import (
	"os/exec"
)

func setPlatformSysProcAttr(cmd *exec.Cmd) {
	// No platform-specific setup needed for non-Windows
}
