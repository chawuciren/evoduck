//go:build darwin

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	// LaunchAgentName is the LaunchAgent identifier
	LaunchAgentName = "com.evoduck.plist"
)

// DarwinAutostartManager manages macOS LaunchAgent autostart
type DarwinAutostartManager struct{}

// NewPlatformAutostartManager creates macOS autostart manager
func NewPlatformAutostartManager() AutostartManager {
	return &DarwinAutostartManager{}
}

// GetLaunchAgentsFolder returns macOS LaunchAgents folder path
func (m *DarwinAutostartManager) GetLaunchAgentsFolder() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, "Library", "LaunchAgents")
}

// GetPath returns autostart file path
func (m *DarwinAutostartManager) GetPath() string {
	return filepath.Join(m.GetLaunchAgentsFolder(), LaunchAgentName)
}

// Enable creates LaunchAgent plist file
func (m *DarwinAutostartManager) Enable(execPath, configPath string) error {
	launchAgentsFolder := m.GetLaunchAgentsFolder()
	if launchAgentsFolder == "" {
		return fmt.Errorf("cannot find LaunchAgents folder")
	}

	// Ensure LaunchAgents folder exists
	if err := os.MkdirAll(launchAgentsFolder, 0755); err != nil {
		return fmt.Errorf("create LaunchAgents folder: %w", err)
	}

	// Create plist content
	plistContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.evoduck</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>start</string>
        <string>--config</string>
        <string>%s</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <false/>
    <key>StandardOutPath</key>
    <string>%s/logs/evoduck.log</string>
    <key>StandardErrorPath</key>
    <string>%s/logs/evoduck.log</string>
</dict>
</plist>
`, execPath, configPath, DefaultDataDir(), DefaultDataDir())

	// Write plist file
	plistPath := filepath.Join(launchAgentsFolder, LaunchAgentName)
	err := os.WriteFile(plistPath, []byte(plistContent), 0644)
	if err != nil {
		return fmt.Errorf("write plist file: %w", err)
	}

	return nil
}

// Disable removes LaunchAgent plist file
func (m *DarwinAutostartManager) Disable() error {
	plistPath := m.GetPath()
	err := os.Remove(plistPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File already removed
		}
		return fmt.Errorf("remove plist file: %w", err)
	}
	return nil
}

// Status checks if LaunchAgent plist file exists
func (m *DarwinAutostartManager) Status() (bool, error) {
	plistPath := m.GetPath()
	if _, err := os.Stat(plistPath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("check autostart file: %w", err)
	}
	return true, nil
}