package daemon

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/chawuciren/evoduck/pkg/logger"
)

// RestartPolicy defines how worker process restart should be handled
type RestartPolicy struct {
	MaxRetries    int           // Maximum restart attempts (default: 5)
	RetryDelay    time.Duration // Initial delay between restarts (default: 3s)
	BackoffFactor float64       // Backoff multiplier (default: 2.0)
}

// DefaultRestartPolicy returns the default restart policy
func DefaultRestartPolicy() RestartPolicy {
	return RestartPolicy{
		MaxRetries:    5,
		RetryDelay:    3 * time.Second,
		BackoffFactor: 2.0,
	}
}

// ProcessStatus represents the status of daemon and worker processes
type ProcessStatus struct {
	DaemonRunning bool          // Daemon process is running
	WorkerRunning bool          // Worker process is running
	DaemonPID     int           // Daemon process PID
	WorkerPID     int           // Worker process PID
	Uptime        time.Duration // Time since daemon started
	RestartCount  int           // Number of worker restarts
}

// Supervisor manages daemon and worker processes (PM2-style)
type Supervisor struct {
	mu              sync.Mutex
	pidManager      *PIDManager
	executable      string        // Executable path
	configPath      string        // Config file path
	dataDir         string        // Data directory path
	logPath         string        // Log file path
	restartPolicy   RestartPolicy // Restart policy
	gwHost          string        // Gateway host for HTTP shutdown
	gwPort          int           // Gateway port for HTTP shutdown
	ctrlPort        int           // Control port for daemon HTTP server
	currentWorker   *exec.Cmd     // Current worker process
	restartCount    int           // Number of restarts
	startTime       time.Time     // Daemon start time
	shutdownSignal  chan struct{} // Signal for daemon shutdown
	workerExitChan  chan error    // Worker exit notification
	logFile         *os.File      // Log file handle
	intentionalStop bool          // Flag to indicate intentional worker stop
	controlServer   *http.Server  // HTTP control server for daemon
}

// SupervisorConfig configuration for supervisor
type SupervisorConfig struct {
	Executable    string        // Executable path
	ConfigPath    string        // Config file path
	DataDir       string        // Data directory (default: ~/.evoduck)
	LogPath       string        // Log directory (default: dataDir/logs)
	RestartPolicy RestartPolicy // Restart policy
	GatewayHost   string        // Gateway host for HTTP shutdown (default: 127.0.0.1)
	GatewayPort   int           // Gateway port for HTTP shutdown (default: 18789)
	CtrlPort      int           // Control port for daemon HTTP server (default: 18790)
}

// NewSupervisor creates a new supervisor instance
func NewSupervisor(cfg SupervisorConfig) *Supervisor {
	if cfg.DataDir == "" {
		cfg.DataDir = DefaultDataDir()
	}
	if cfg.LogPath == "" {
		cfg.LogPath = filepath.Join(cfg.DataDir, "logs")
	}
	if cfg.RestartPolicy.MaxRetries == 0 {
		cfg.RestartPolicy = DefaultRestartPolicy()
	}

	return &Supervisor{
		pidManager:     NewPIDManager(cfg.DataDir),
		executable:     cfg.Executable,
		configPath:     cfg.ConfigPath,
		dataDir:        cfg.DataDir,
		logPath:        cfg.LogPath,
		restartPolicy:  cfg.RestartPolicy,
		gwHost:         cfg.GatewayHost,
		gwPort:         cfg.GatewayPort,
		ctrlPort:       cfg.CtrlPort,
		shutdownSignal: make(chan struct{}),
		workerExitChan: make(chan error, 1),
	}
}

// StartDaemon starts the daemon process in background
// This is called by the CLI user (evoduck start)
func (s *Supervisor) StartDaemon() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if daemon is already running
	daemonPID, err := s.pidManager.ReadPID(s.pidManager.GetDaemonPIDPath())
	if err == nil && IsProcessRunning(daemonPID) {
		return fmt.Errorf("daemon already running (PID: %d)", daemonPID)
	}

	// Ensure log directory exists
	if err := os.MkdirAll(s.logPath, 0755); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}

	// Prepare command to start daemon
	args := []string{"daemon-mode", "--config", s.configPath}
	cmd := exec.Command(s.executable, args...)

	// Set platform-specific process detachment
	cmd.SysProcAttr = GetDaemonSysProcAttr()

	// Redirect stdout/stderr to log file
	daemonLogPath := filepath.Join(s.logPath, "evoduck-daemon.log")
	logFile, err := os.OpenFile(daemonLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open daemon log file: %w", err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil

	// Start the daemon process
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("start daemon process: %w", err)
	}

	// Write daemon PID
	if err := s.pidManager.WritePIDValue(s.pidManager.GetDaemonPIDPath(), cmd.Process.Pid); err != nil {
		cmd.Process.Kill()
		logFile.Close()
		return fmt.Errorf("write daemon PID: %w", err)
	}

	logFile.Close()

	logger.Info("Daemon started", logger.Fields{
		"pid":   cmd.Process.Pid,
		"log":   daemonLogPath,
		"config": s.configPath,
	})

	return nil
}

// StopDaemon stops daemon and worker processes
// This is called by the CLI user (evoduck stop)
func (s *Supervisor) StopDaemon() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Read daemon PID
	daemonPID, err := s.pidManager.ReadPID(s.pidManager.GetDaemonPIDPath())
	if err != nil {
		return fmt.Errorf("daemon not running: %w", err)
	}

	// Check if daemon is running
	if !IsProcessRunning(daemonPID) {
		// Clean up PID files
		s.pidManager.CleanAllPIDs()
		return fmt.Errorf("daemon process not running")
	}

	// Try graceful shutdown via HTTP first
	logger.Info("Attempting graceful shutdown via HTTP", logger.Fields{
		"pid": daemonPID,
	})
	gracefulSuccess := s.tryGracefulShutdown(10 * time.Second)

	// Wait for process to exit after graceful shutdown
	if gracefulSuccess {
		logger.Info("Graceful shutdown initiated, waiting for process exit", logger.Fields{
			"pid": daemonPID,
		})
		WaitForProcessExit(daemonPID, 10*time.Second)
	}

	// Check if process exited gracefully
	if !IsProcessRunning(daemonPID) {
		s.pidManager.CleanAllPIDs()
		logger.Info("Daemon stopped gracefully", logger.Fields{
			"pid": daemonPID,
		})
		return nil
	}

	// Process still running, force terminate
	logger.Warn("Daemon still running after graceful shutdown, terminating", logger.Fields{
		"pid": daemonPID,
	})
	if err := TerminateProcess(daemonPID); err != nil {
		logger.Warn("Failed to terminate daemon process", logger.Fields{
			"pid":   daemonPID,
			"error": err.Error(),
		})
	}

	// Verify process has exited
	if IsProcessRunning(daemonPID) {
		// Force kill if still running after terminate
		logger.Warn("Daemon still running after terminate, force killing", logger.Fields{
			"pid": daemonPID,
		})
		// On Unix, use SIGKILL; on Windows, TerminateProcess already did its best
		if err := KillProcess(daemonPID); err != nil {
			logger.Warn("Force kill failed", logger.Fields{"error": err.Error()})
		}
		// Wait a bit more for cleanup
		WaitForProcessExit(daemonPID, 2*time.Second)
	}

	// Clean up PID files
	s.pidManager.CleanAllPIDs()

	logger.Info("Daemon stopped", logger.Fields{
		"pid": daemonPID,
	})

	return nil
}

// tryGracefulShutdown sends HTTP POST to daemon's control endpoint
// Returns true if shutdown request was sent successfully (not if process exited)
func (s *Supervisor) tryGracefulShutdown(timeout time.Duration) bool {
	host := s.gatewayHost()
	if host == "" {
		host = "127.0.0.1"
	}
	port := s.controlPort()

	// Shutdown endpoint is on daemon's control server
	shutdownURL := fmt.Sprintf("http://%s:%d/api/shutdown", host, port)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, shutdownURL, bytes.NewReader([]byte{}))
	if err != nil {
		logger.Warn("Failed to create shutdown request", logger.Fields{
			"error": err.Error(),
		})
		return false
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		logger.Warn("Failed to send shutdown request", logger.Fields{
			"url":   shutdownURL,
			"error": err.Error(),
		})
		return false
	}
	defer resp.Body.Close()

	// Read response body (discard)
	io.Copy(io.Discard, resp.Body)

	logger.Info("Shutdown request sent", logger.Fields{
		"url":        shutdownURL,
		"status":     resp.StatusCode,
		"graceful":   true,
	})
	return resp.StatusCode == http.StatusOK
}

func (s *Supervisor) gatewayHost() string {
	if s.gwHost != "" {
		return s.gwHost
	}
	return "127.0.0.1"
}

func (s *Supervisor) controlPort() int {
	if s.ctrlPort > 0 {
		return s.ctrlPort
	}
	// Default: gateway port + 2 to avoid conflict with worker's HTTP server (port + 1)
	if s.gwPort > 0 {
		return s.gwPort + 2
	}
	return 18791 // Default daemon control port (18789 + 2)
}

// RestartWorker restarts only the worker process (daemon keeps running)
// This is called by the CLI user (evoduck restart)
func (s *Supervisor) RestartWorker() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Read daemon PID
	daemonPID, err := s.pidManager.ReadPID(s.pidManager.GetDaemonPIDPath())
	if err != nil || !IsProcessRunning(daemonPID) {
		return fmt.Errorf("daemon not running")
	}

	// Read worker PID
	workerPID, err := s.pidManager.ReadPID(s.pidManager.GetWorkerPIDPath())
	if err == nil && IsProcessRunning(workerPID) {
		// Terminate worker process
		TerminateProcess(workerPID)
		// Wait for worker to stop
		time.Sleep(1 * time.Second)
	}

	// Remove worker PID file
	s.pidManager.RemovePID(s.pidManager.GetWorkerPIDPath())

	logger.Info("Worker restart initiated", logger.Fields{
		"daemon_pid": daemonPID,
	})

	return nil
}

// Status returns the current process status
func (s *Supervisor) Status() (ProcessStatus, error) {
	status := ProcessStatus{}

	// Check daemon
	daemonPID, err := s.pidManager.ReadPID(s.pidManager.GetDaemonPIDPath())
	if err == nil {
		status.DaemonPID = daemonPID
		status.DaemonRunning = IsProcessRunning(daemonPID)
	}

	// Check worker
	workerPID, err := s.pidManager.ReadPID(s.pidManager.GetWorkerPIDPath())
	if err == nil {
		status.WorkerPID = workerPID
		status.WorkerRunning = IsProcessRunning(workerPID)
	}

	return status, nil
}

// RunDaemonMode runs the daemon loop (blocking)
// This is called when the process is started as daemon-mode
func (s *Supervisor) RunDaemonMode() error {
	// Write daemon PID (self)
	s.pidManager.WritePID(s.pidManager.GetDaemonPIDPath())
	s.startTime = time.Now()

	logger.Info("Daemon mode started", logger.Fields{
		"pid":          os.Getpid(),
		"control_port": s.controlPort(),
	})

	// Ensure log directory
	os.MkdirAll(s.logPath, 0755)

	// Open log file for worker output
	workerLogPath := filepath.Join(s.logPath, "evoduck.log")
	logFile, err := os.OpenFile(workerLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		logger.Error("Failed to open worker log file", logger.Fields{
			"error": err.Error(),
		})
		return fmt.Errorf("open worker log file: %w", err)
	}
	s.logFile = logFile

	// Start HTTP control server for shutdown endpoint
	s.startControlServer()

	// Setup signal handler
	go s.handleSignals()

	// Start worker
	s.spawnWorker()

	// Main daemon loop
	for {
		select {
		case err := <-s.workerExitChan:
			// Worker exited
			s.handleWorkerExit(err)
		case <-s.shutdownSignal:
			// Daemon shutdown signal
			s.stopControlServer()
			s.stopWorker()
			s.pidManager.CleanAllPIDs()
			s.logFile.Close()
			logger.Info("Daemon mode stopped")
			return nil
		}
	}
}

// RunWorkerMode runs the worker process (blocking, actual business logic)
// This is called when the process is started as worker-mode
func (s *Supervisor) RunWorkerMode(workerFunc func() error) error {
	// Write worker PID (self)
	s.pidManager.WritePID(s.pidManager.GetWorkerPIDPath())

	logger.Info("Worker mode started", logger.Fields{
		"pid": os.Getpid(),
	})

	// Run the actual worker function
	err := workerFunc()

	logger.Info("Worker mode stopped", logger.Fields{
		"error": fmt.Sprintf("%v", err),
	})

	// Remove worker PID
	s.pidManager.RemovePID(s.pidManager.GetWorkerPIDPath())

	return err
}

// RunForeground runs in foreground mode (no daemon, for development)
func (s *Supervisor) RunForeground(workerFunc func() error) error {
	logger.Info("Running in foreground mode")

	// Setup signal handler for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	setupSignalHandler(sigChan)

	// Run worker
	errChan := make(chan error, 1)
	go func() {
		errChan <- workerFunc()
	}()

	// Wait for signal or worker completion
	select {
	case sig := <-sigChan:
		logger.Info("Received signal, shutting down", logger.Fields{
			"signal": sig.String(),
		})
		return nil
	case err := <-errChan:
		return err
	}
}

// spawnWorker starts a new worker process
func (s *Supervisor) spawnWorker() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Prepare worker command
	args := []string{"worker-mode", "--config", s.configPath}
	cmd := exec.Command(s.executable, args...)

	// Set platform-specific process detachment (hide console window on Windows)
	cmd.SysProcAttr = GetDaemonSysProcAttr()

	// Redirect output to log file
	if s.logFile != nil {
		cmd.Stdout = s.logFile
		cmd.Stderr = s.logFile
	}

	// Start worker
	if err := cmd.Start(); err != nil {
		logger.Error("Failed to spawn worker", logger.Fields{
			"error": err.Error(),
		})
		return err
	}

	s.currentWorker = cmd
	s.intentionalStop = false // Reset intentional stop flag when spawning

	// Write worker PID
	s.pidManager.WritePIDValue(s.pidManager.GetWorkerPIDPath(), cmd.Process.Pid)

	logger.Info("Worker spawned", logger.Fields{
		"pid": cmd.Process.Pid,
	})

	// Monitor worker exit
	go func() {
		err := cmd.Wait()
		s.workerExitChan <- err
	}()

	return nil
}

// handleWorkerExit handles worker process exit
func (s *Supervisor) handleWorkerExit(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clean worker PID
	s.pidManager.RemovePID(s.pidManager.GetWorkerPIDPath())

	// Check if this was an intentional stop (daemon shutdown)
	if s.intentionalStop {
		logger.Info("Worker stopped intentionally, not restarting")
		return
	}

	// Log exit reason
	if err != nil {
		logger.Warn("Worker exited with error", logger.Fields{
			"error": err.Error(),
		})
	} else {
		logger.Info("Worker exited, will restart to keep service alive")
	}

	// Always restart worker unless intentional stop
	// This ensures the service stays alive unless explicitly stopped
	s.restartCount++
	if s.restartCount <= s.restartPolicy.MaxRetries {
		// Calculate delay with backoff
		delay := s.restartPolicy.RetryDelay
		for i := 1; i < s.restartCount; i++ {
			delay = time.Duration(float64(delay) * s.restartPolicy.BackoffFactor)
		}

		logger.Info("Restarting worker", logger.Fields{
			"attempt":  s.restartCount,
			"max":      s.restartPolicy.MaxRetries,
			"delay_ms": delay.Milliseconds(),
		})

		// Wait before restart
		time.Sleep(delay)

		// Restart worker
		s.spawnWorker()
	} else {
		logger.Error("Worker restart limit reached, daemon shutting down", logger.Fields{
			"attempts": s.restartCount,
			"max":      s.restartPolicy.MaxRetries,
		})
		s.shutdownSignal <- struct{}{}
	}
}

// stopWorker stops the current worker process
func (s *Supervisor) stopWorker() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Mark as intentional stop to prevent auto-restart
	s.intentionalStop = true

	if s.currentWorker != nil && s.currentWorker.Process != nil {
		pid := s.currentWorker.Process.Pid
		if IsProcessRunning(pid) {
			TerminateProcess(pid)
			logger.Info("Worker stopped", logger.Fields{
				"pid": pid,
			})
		}
	}

	// Clean worker PID
	s.pidManager.RemovePID(s.pidManager.GetWorkerPIDPath())
}

// handleSignals handles OS signals for daemon
func (s *Supervisor) handleSignals() {
	sigChan := make(chan os.Signal, 1)
	setupSignalHandler(sigChan)

	sig := <-sigChan
	logger.Info("Daemon received signal", logger.Fields{
		"signal": sig.String(),
	})
	s.shutdownSignal <- struct{}{}
}

// startControlServer starts the HTTP control server for daemon
func (s *Supervisor) startControlServer() {
	port := s.controlPort()
	addr := fmt.Sprintf("%s:%d", s.gatewayHost(), port)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/shutdown", s.handleControlShutdown)

	s.controlServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		logger.Info("Daemon control server started", logger.Fields{
			"addr": addr,
		})
		if err := s.controlServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Control server error", logger.Fields{
				"error": err.Error(),
			})
		}
	}()
}

// stopControlServer stops the HTTP control server
func (s *Supervisor) stopControlServer() {
	if s.controlServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.controlServer.Shutdown(ctx)
		logger.Info("Control server stopped")
	}
}

// handleControlShutdown handles shutdown requests from CLI
func (s *Supervisor) handleControlShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	logger.Info("Received shutdown request via control server")

	// Send response first
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "{\"status\": \"shutting_down\", \"message\": \"Graceful shutdown initiated\"}")

	// Trigger shutdown asynchronously
	go func() {
		s.shutdownSignal <- struct{}{}
	}()
}

// setupSignalHandler sets up signal handling
func setupSignalHandler(sigChan chan<- os.Signal) {
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
}
type LogWatcher struct {
	logPath string
	file    *os.File
}

// NewLogWatcher creates a log watcher
func NewLogWatcher(logPath string) *LogWatcher {
	return &LogWatcher{
		logPath: logPath,
	}
}

// Follow follows log file output (like tail -f)
func (w *LogWatcher) Follow(ctx context.Context) (<-chan string, error) {
	file, err := os.Open(w.logPath)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	w.file = file

	outputChan := make(chan string, 100)

	go func() {
		defer file.Close()
		defer close(outputChan)

		// Seek to end for existing content
		file.Seek(0, io.SeekEnd)

		buf := make([]byte, 4096)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				n, err := file.Read(buf)
				if err != nil {
					if err == io.EOF {
						time.Sleep(500 * time.Millisecond)
						continue
					}
					return
				}
				if n > 0 {
					outputChan <- string(buf[:n])
				}
			}
		}
	}()

	return outputChan, nil
}

// Close closes the log watcher
func (w *LogWatcher) Close() error {
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}