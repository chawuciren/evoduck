//go:build linux

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	// DesktopFileName is the XDG autostart desktop file name
	DesktopFileName = "evoduck.desktop"
)

// LinuxAutostartManager manages Linux XDG autostart
type LinuxAutostartManager struct{}

// NewPlatformAutostartManager creates Linux autostart manager
func NewPlatformAutostartManager() AutostartManager {
	return &LinuxAutostartManager{}
}

// GetAutostartFolder returns XDG autostart folder path
func (m *LinuxAutostartManager) GetAutostartFolder() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, ".config", "autostart")
}

// GetPath returns autostart file path
func (m *LinuxAutostartManager) GetPath() string {
	return filepath.Join(m.GetAutostartFolder(), DesktopFileName)
}

// Enable creates XDG autostart desktop file
func (m *LinuxAutostartManager) Enable(execPath, configPath string) error {
	autostartFolder := m.GetAutostartFolder()
	if autostartFolder == "" {
		return fmt.Errorf("cannot find autostart folder")
	}

	// Ensure autostart folder exists
	if err := os.MkdirAll(autostartFolder, 0755); err != nil {
		return fmt.Errorf("create autostart folder: %w", err)
	}

	// Create desktop file content
	desktopContent := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=EvoDuck
Comment=EvoDuck AI Agent Framework
Exec=%s start --config %s
Icon=evoduck
Hidden=false
NoDisplay=false
X-GNOME-Autostart-enabled=true
`, execPath, configPath)

	// Write desktop file
	desktopPath := filepath.Join(autostartFolder, DesktopFileName)
	err := os.WriteFile(desktopPath, []byte(desktopContent), 0644)
	if err != nil {
		return fmt.Errorf("write desktop file: %w", err)
	}

	return nil
}

// Disable removes XDG autostart desktop file
func (m *LinuxAutostartManager) Disable() error {
	desktopPath := m.GetPath()
	err := os.Remove(desktopPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File already removed
		}
		return fmt.Errorf("remove desktop file: %w", err)
	}
	return nil
}

// Status checks if autostart desktop file exists
func (m *LinuxAutostartManager) Status() (bool, error) {
	desktopPath := m.GetPath()
	if _, err := os.Stat(desktopPath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("check autostart file: %w", err)
	}
	return true, nil
}