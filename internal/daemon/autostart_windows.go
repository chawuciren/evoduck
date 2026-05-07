//go:build windows

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	// AutostartFileName is the autostart batch file name
	AutostartFileName = "evoduck.bat"
)

// WindowsAutostartManager manages Windows Startup folder autostart
type WindowsAutostartManager struct{}

// NewPlatformAutostartManager creates Windows autostart manager
func NewPlatformAutostartManager() AutostartManager {
	return &WindowsAutostartManager{}
}

// GetStartupFolder returns Windows Startup folder path
func (m *WindowsAutostartManager) GetStartupFolder() string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return ""
	}
	return filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
}

// GetPath returns autostart file path
func (m *WindowsAutostartManager) GetPath() string {
	return filepath.Join(m.GetStartupFolder(), AutostartFileName)
}

// Enable creates autostart batch file in Startup folder
func (m *WindowsAutostartManager) Enable(execPath, configPath string) error {
	startupFolder := m.GetStartupFolder()
	if startupFolder == "" {
		return fmt.Errorf("cannot find Startup folder")
	}

	// Ensure Startup folder exists
	if err := os.MkdirAll(startupFolder, 0755); err != nil {
		return fmt.Errorf("create Startup folder: %w", err)
	}

	// Create batch file content
	batchContent := fmt.Sprintf(`@echo off
REM EvoDuck Autostart
"%s" start --config "%s"
`, execPath, configPath)

	// Write batch file
	batchPath := filepath.Join(startupFolder, AutostartFileName)
	err := os.WriteFile(batchPath, []byte(batchContent), 0644)
	if err != nil {
		return fmt.Errorf("write batch file: %w", err)
	}

	return nil
}

// Disable removes autostart batch file
func (m *WindowsAutostartManager) Disable() error {
	batchPath := m.GetPath()
	err := os.Remove(batchPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File already removed
		}
		return fmt.Errorf("remove batch file: %w", err)
	}
	return nil
}

// Status checks if autostart batch file exists
func (m *WindowsAutostartManager) Status() (bool, error) {
	batchPath := m.GetPath()
	if _, err := os.Stat(batchPath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("check autostart file: %w", err)
	}
	return true, nil
}