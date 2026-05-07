package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DefaultInstallDir returns the default installation directory
// Windows: %USERPROFILE%\.local\bin
// macOS/Linux: ~/.local/bin
func DefaultInstallDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, ".local", "bin")
}

// IsInPATH checks if a directory is in PATH environment variable
// Compatible with all Windows versions (including XP) - uses direct env parsing
func IsInPATH(dir string) bool {
	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		return false
	}

	// Normalize the directory path
	dir = filepath.Clean(dir)

	// Split PATH by separator
	var separator string
	if runtime.GOOS == "windows" {
		separator = ";"
	} else {
		separator = ":"
	}

	paths := strings.Split(pathEnv, separator)
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		p = filepath.Clean(p)

		if runtime.GOOS == "windows" {
			// Windows: case-insensitive comparison
			if strings.EqualFold(p, dir) {
				return true
			}
		} else {
			// Unix: case-sensitive comparison
			if p == dir {
				return true
			}
		}
	}

	return false
}

// IsExecutableInPATH checks if evoduck executable can be found in PATH
// Uses direct PATH parsing instead of external commands (where/which)
func IsExecutableInPATH(execName string) bool {
	// Get the executable directory
	execPath, err := os.Executable()
	if err != nil {
		return false
	}
	execDir := filepath.Dir(execPath)

	return IsInPATH(execDir)
}

// CheckAndAddToPATH checks if evoduck directory is in PATH and adds if not
// Returns: exists (true if already in PATH), added (true if successfully added), error
func CheckAndAddToPATH(execPath string) (exists bool, added bool, err error) {
	// Get executable directory
	execDir := filepath.Dir(execPath)

	// Check if already in PATH
	if IsInPATH(execDir) {
		return true, false, nil
	}

	// Try to add to PATH
	err = AddToUserPATH(execDir)
	if err != nil {
		return false, false, err
	}

	return false, true, nil
}

// GetPATHAddInstruction returns instructions for manually adding to PATH
func GetPATHAddInstruction(dir string) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf(`Please manually add the following path to system PATH:
  Path: %s
  Steps:
  1. Open "System Properties" → "Advanced" → "Environment Variables"
  2. Under "User variables", find "PATH" and click "Edit"
  3. Click "New" and add: %s
  4. Click "OK" to save`, dir, dir)
	}

	// macOS/Linux
	shell := os.Getenv("SHELL")
	var rcFile string
	if strings.Contains(shell, "zsh") {
		rcFile = "~/.zshrc"
	} else if strings.Contains(shell, "bash") {
		rcFile = "~/.bashrc"
	} else {
		rcFile = "~/.profile"
	}

	return fmt.Sprintf(`Please manually add the following to your shell config file:
  File: %s
  Add: export PATH="$PATH:%s"

After adding, run: source %s`, rcFile, dir, rcFile)
}

// AddToUserPATH adds directory to user PATH environment variable
// Platform-specific implementation
func AddToUserPATH(dir string) error {
	if runtime.GOOS == "windows" {
		return addToUserPATHWindows(dir)
	}
	return addToUserPATHUnix(dir)
}

// addToUserPATHWindows adds to user PATH via Windows registry
func addToUserPATHWindows(dir string) error {
	return fmt.Errorf("automatic PATH modification requires registry access - please add manually")
}

// addToUserPATHUnix adds to user PATH via shell config file
func addToUserPATHUnix(dir string) error {
	return fmt.Errorf("automatic PATH modification requires shell config modification - please add manually")
}