package daemon

import (
	"fmt"
	"os"
	"path/filepath"
)

// AutostartManager manages OS-specific autostart configuration
// Windows: Startup folder batch file
// macOS: LaunchAgent plist
// Linux: XDG autostart desktop file
type AutostartManager interface {
	// Enable creates autostart configuration
	Enable(execPath, configPath string) error

	// Disable removes autostart configuration
	Disable() error

	// Status returns current autostart status
	Status() (bool, error)

	// GetPath returns autostart file path
	GetPath() string
}

// GetAutostartManager returns the platform-specific autostart manager
func GetAutostartManager() AutostartManager {
	return NewPlatformAutostartManager()
}

// InstallAutostart installs autostart configuration
// This is the main function called by CLI (evoduck install)
func InstallAutostart(execPath, configPath string) error {
	manager := GetAutostartManager()

	// Check and add to PATH
	exists, added, err := CheckAndAddToPATH(execPath)
	if err != nil {
		// PATH modification failed, show manual instructions
		fmt.Println("⚠️  Cannot automatically add to PATH:")
		fmt.Println(GetPATHAddInstruction(filepath.Dir(execPath)))
		fmt.Println()
		fmt.Println("Please add to PATH manually and then run 'evoduck install' again.")
		// Continue with autostart setup anyway
	} else {
		if !exists {
			if added {
				fmt.Println("✓ Added evoduck to PATH")
			} else {
				fmt.Println("ℹ️  evoduck directory is in PATH")
			}
		}
	}

	// Enable autostart
	err = manager.Enable(execPath, configPath)
	if err != nil {
		return fmt.Errorf("enable autostart: %w", err)
	}

	fmt.Printf("✓ Autostart enabled\n")
	fmt.Printf("  Location: %s\n", manager.GetPath())

	return nil
}

// UninstallAutostart removes autostart configuration
// This is the main function called by CLI (evoduck uninstall)
func UninstallAutostart() error {
	manager := GetAutostartManager()

	err := manager.Disable()
	if err != nil {
		return fmt.Errorf("disable autostart: %w", err)
	}

	fmt.Println("✓ Autostart disabled")

	return nil
}

// CheckAutostartStatus checks current autostart status
func CheckAutostartStatus() (bool, error) {
	manager := GetAutostartManager()
	return manager.Status()
}

// GetDefaultConfigPath returns the default config file path
func GetDefaultConfigPath() string {
	dataDir := DefaultDataDir()
	return filepath.Join(dataDir, "config.yaml")
}

// EnsureDefaultConfig creates default config if not exists
func EnsureDefaultConfig() error {
	configPath := GetDefaultConfigPath()
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Create default config directory
		dataDir := DefaultDataDir()
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			return fmt.Errorf("create data directory: %w", err)
		}
		// Create empty config file (user should fill it)
		if err := os.WriteFile(configPath, []byte("# EvoDuck Configuration\n"), 0644); err != nil {
			return fmt.Errorf("create config file: %w", err)
		}
	}
	return nil
}