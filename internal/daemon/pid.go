package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// PIDDirectoryName PID file directory name
	PIDDirectoryName = "pid"

	// DaemonPIDFileName daemon PID file name
	DaemonPIDFileName = "evoduck-daemon.pid"

	// WorkerPIDFileName worker PID file name
	WorkerPIDFileName = "evoduck-worker.pid"
)

// PIDManager PID file manager
type PIDManager struct {
	dataDir string // data directory path (e.g., ~/.evoduck)
}

// NewPIDManager creates a new PID manager
func NewPIDManager(dataDir string) *PIDManager {
	return &PIDManager{
		dataDir: dataDir,
	}
}

// GetPIDDir returns the PID directory path
func (m *PIDManager) GetPIDDir() string {
	return filepath.Join(m.dataDir, PIDDirectoryName)
}

// GetDaemonPIDPath returns the daemon PID file path
func (m *PIDManager) GetDaemonPIDPath() string {
	return filepath.Join(m.GetPIDDir(), DaemonPIDFileName)
}

// GetWorkerPIDPath returns the worker PID file path
func (m *PIDManager) GetWorkerPIDPath() string {
	return filepath.Join(m.GetPIDDir(), WorkerPIDFileName)
}

// EnsurePIDDir ensures the PID directory exists
func (m *PIDManager) EnsurePIDDir() error {
	pidDir := m.GetPIDDir()
	if err := os.MkdirAll(pidDir, 0755); err != nil {
		return fmt.Errorf("create PID directory: %w", err)
	}
	return nil
}

// WritePID writes the current process PID to the specified file
func (m *PIDManager) WritePID(path string) error {
	if err := m.EnsurePIDDir(); err != nil {
		return err
	}

	pid := os.Getpid()
	content := strconv.Itoa(pid)

	// Write PID file with proper permissions
	err := os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		return fmt.Errorf("write PID file: %w", err)
	}

	return nil
}

// WritePIDValue writes a specific PID value to the specified file
func (m *PIDManager) WritePIDValue(path string, pid int) error {
	if err := m.EnsurePIDDir(); err != nil {
		return err
	}

	content := strconv.Itoa(pid)
	err := os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		return fmt.Errorf("write PID file: %w", err)
	}

	return nil
}

// ReadPID reads PID from the specified file
func (m *PIDManager) ReadPID(path string) (int, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, errors.New("PID file not found")
		}
		return 0, fmt.Errorf("read PID file: %w", err)
	}

	// Parse PID
	pidStr := strings.TrimSpace(string(content))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, fmt.Errorf("parse PID: %w", err)
	}

	if pid <= 0 {
		return 0, errors.New("invalid PID value")
	}

	return pid, nil
}

// RemovePID removes the PID file
func (m *PIDManager) RemovePID(path string) error {
	err := os.Remove(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File already removed, no error
		}
		return fmt.Errorf("remove PID file: %w", err)
	}
	return nil
}

// AcquireLock checks for existing PID and writes new PID if not running
// Returns: existing PID if process is running, 0 if lock acquired successfully
func (m *PIDManager) AcquireLock(path string) (int, error) {
	// Check if PID file exists
	existingPID, err := m.ReadPID(path)
	if err != nil {
		if errors.Is(err, errors.New("PID file not found")) {
			// No existing PID file, acquire lock
			if err := m.WritePID(path); err != nil {
				return 0, err
			}
			return 0, nil
		}
		return 0, err
	}

	// Check if existing process is running
	if IsProcessRunning(existingPID) {
		return existingPID, errors.New("process already running")
	}

	// Existing process not running, acquire lock
	if err := m.WritePID(path); err != nil {
		return 0, err
	}

	return 0, nil
}

// ReleaseLock removes the PID file (release lock)
func (m *PIDManager) ReleaseLock(path string) error {
	return m.RemovePID(path)
}

// CleanAllPIDs removes all PID files
func (m *PIDManager) CleanAllPIDs() error {
	if err := m.RemovePID(m.GetDaemonPIDPath()); err != nil {
		return err
	}
	if err := m.RemovePID(m.GetWorkerPIDPath()); err != nil {
		return err
	}
	return nil
}

// GetDataDir returns the data directory
func (m *PIDManager) GetDataDir() string {
	return m.dataDir
}

// DefaultDataDir returns the default data directory path
// Windows: %USERPROFILE%\.evoduck
// macOS/Linux: ~/.evoduck
func DefaultDataDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Fallback to current directory
		return ".evoduck"
	}
	return filepath.Join(homeDir, ".evoduck")
}