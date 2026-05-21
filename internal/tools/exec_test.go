package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
)

func TestExecToolTimeoutStopsCommand(t *testing.T) {
	workspace := t.TempDir()
	tool := NewExecTool(NewAgentPermissions(models.RoleAdmin, workspace, config.AgentPermissionConfig{}), nil)

	command := longRunningShellCommand()

	started := time.Now()
	result, err := tool.ExecuteWithContext(context.Background(), map[string]interface{}{
		"command": command,
		"timeout": float64(1),
	})
	if err != nil {
		t.Fatalf("exec returned unexpected error: %v", err)
	}

	elapsed := time.Since(started)
	if elapsed > 5*time.Second {
		t.Fatalf("expected timeout to stop quickly, took %v", elapsed)
	}
	if !strings.Contains(result, "Timeout") {
		t.Fatalf("expected timeout result, got: %s", result)
	}
}

func TestProcessToolKillStopsProcessTree(t *testing.T) {
	workspace := t.TempDir()
	tool := NewProcessTool(NewAgentPermissions(models.RoleAdmin, workspace, config.AgentPermissionConfig{}), nil)

	command := longRunningShellCommand()

	started, err := tool.ExecuteWithContext(context.Background(), map[string]interface{}{
		"action":  "start",
		"command": command,
	})
	if err != nil {
		t.Fatalf("process start failed: %v", err)
	}

	sessionID := extractTestSessionID(t, started)
	if _, err := tool.ExecuteWithContext(context.Background(), map[string]interface{}{
		"action":     "kill",
		"session_id": sessionID,
	}); err != nil {
		t.Fatalf("process kill failed: %v", err)
	}

	result, err := tool.ExecuteWithContext(context.Background(), map[string]interface{}{
		"action":     "wait",
		"session_id": sessionID,
		"timeout":    float64(3),
	})
	if err != nil {
		t.Fatalf("process wait failed: %v", err)
	}
	if !strings.Contains(result, "exit code") {
		t.Fatalf("expected wait result after kill, got: %s", result)
	}
}

func TestProcessToolInputAndPoll(t *testing.T) {
	workspace := t.TempDir()
	tool := NewProcessTool(NewAgentPermissions(models.RoleAdmin, workspace, config.AgentPermissionConfig{}), nil)

	started, err := tool.ExecuteWithContext(context.Background(), map[string]interface{}{
		"action":  "start",
		"command": interactiveEchoCommand(t, workspace),
	})
	if err != nil {
		t.Fatalf("process start failed: %v", err)
	}
	sessionID := extractTestSessionID(t, started)

	promptOutput := waitForProcessOutput(t, tool, sessionID, 3*time.Second, "ready:")
	if !strings.Contains(promptOutput, "ready:") {
		t.Fatalf("expected prompt output, got: %s", promptOutput)
	}

	if _, err := tool.ExecuteWithContext(context.Background(), map[string]interface{}{
		"action":         "input",
		"session_id":     sessionID,
		"text":           "duck",
		"append_newline": true,
		"close_stdin":    true,
	}); err != nil {
		t.Fatalf("process input failed: %v", err)
	}

	echoOutput := waitForProcessOutput(t, tool, sessionID, 3*time.Second, "echo:duck")
	if !strings.Contains(echoOutput, "echo:duck") {
		t.Fatalf("expected echoed output, got: %s", echoOutput)
	}

	result, err := tool.ExecuteWithContext(context.Background(), map[string]interface{}{
		"action":     "wait",
		"session_id": sessionID,
		"timeout":    float64(3),
	})
	if err != nil {
		t.Fatalf("process wait failed: %v", err)
	}
	if !strings.Contains(result, "status completed") {
		t.Fatalf("expected completed status, got: %s", result)
	}
}

func TestSleepToolWaits(t *testing.T) {
	tool := NewSleepTool()
	started := time.Now()
	result, err := tool.ExecuteWithRole(context.Background(), map[string]interface{}{
		"seconds": 0.05,
	}, models.RoleAdmin)
	if err != nil {
		t.Fatalf("sleep failed: %v", err)
	}
	if time.Since(started) < 40*time.Millisecond {
		t.Fatalf("expected sleep to wait long enough, got %v", time.Since(started))
	}
	if !strings.Contains(result, "Slept for") {
		t.Fatalf("unexpected sleep result: %s", result)
	}
}

func extractTestSessionID(t *testing.T, started string) string {
	t.Helper()
	for _, line := range strings.Split(started, "\n") {
		if strings.HasPrefix(line, "Session ID: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Session ID: "))
		}
	}
	t.Fatalf("session id not found in: %s", started)
	return ""
}

func waitForProcessOutput(t *testing.T, tool *ProcessTool, sessionID string, timeout time.Duration, want string) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		result, err := tool.ExecuteWithContext(context.Background(), map[string]interface{}{
			"action":     "poll",
			"session_id": sessionID,
		})
		if err != nil {
			t.Fatalf("process poll failed: %v", err)
		}
		if strings.Contains(result, want) {
			return result
		}
		time.Sleep(20 * time.Millisecond)
	}
	result, err := tool.ExecuteWithContext(context.Background(), map[string]interface{}{
		"action":     "log",
		"session_id": sessionID,
	})
	if err != nil {
		t.Fatalf("process log failed: %v", err)
	}
	t.Fatalf("timed out waiting for %q in process output: %s", want, result)
	return ""
}

func isWindowsShellDefault() bool {
	return getDefaultShell() == "cmd"
}

func longRunningShellCommand() string {
	if isWindowsShellDefault() {
		return "ping -n 20 127.0.0.1 > nul"
	}
	return "sleep 20"
}

func TestWrapCmdCommand(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{``, ``},
		{`simple`, `simple`},
		{`echo hello`, `echo hello`},
		{`"C:\Program Files\app.exe" -version`, `""C:\Program Files\app.exe" -version"`},
		{`"C:\path with spaces\ffmpeg.exe" -i input.mp4`, `""C:\path with spaces\ffmpeg.exe" -i input.mp4"`},
		{`""`, `""""`},
	}
	for _, tt := range tests {
		got := wrapCmdCommand(tt.input)
		if got != tt.expected {
			t.Errorf("wrapCmdCommand(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestExecWithSpacedPathViaCmdLine(t *testing.T) {
	if !isWindowsShellDefault() {
		t.Skip("test only runs on Windows (cmd default)")
	}

	workspace := t.TempDir()
	tool := NewExecTool(NewAgentPermissions(models.RoleAdmin, workspace, config.AgentPermissionConfig{}), nil)

	// Create a tiny Go helper .exe in a path with spaces
	spacedDir := filepath.Join(workspace, "my tools")
	if err := os.MkdirAll(spacedDir, 0o755); err != nil {
		t.Fatalf("create spaced dir: %v", err)
	}
	helperSrc := filepath.Join(spacedDir, "helper.go")
	srcContent := `package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	fmt.Println("HELPER-OK args=[" + strings.Join(os.Args[1:], ",") + "]")
}
`
	if err := os.WriteFile(helperSrc, []byte(srcContent), 0o644); err != nil {
		t.Fatalf("write helper source: %v", err)
	}
	helperExe := filepath.Join(spacedDir, "helper.exe")
	buildCmd := exec.Command("go", "build", "-o", helperExe, helperSrc)
	buildCmd.Dir = workspace
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build test helper: %v\n%s", err, out)
	}

	// Simulate what the LLM generates: a quoted path with spaces
	llmCommand := `"` + helperExe + `" --test "my file.txt"`
	result, err := tool.ExecuteWithContext(context.Background(), map[string]interface{}{
		"command": llmCommand,
		"timeout": float64(10),
	})
	if err != nil {
		t.Fatalf("exec returned unexpected error: %v\nResult: %s", err, result)
	}
	if !strings.Contains(result, "HELPER-OK") {
		t.Fatalf("expected HELPER-OK in output, got: %s", result)
	}
	if !strings.Contains(result, "--test") {
		t.Errorf("expected --test arg in output, got: %s", result)
	}
	if !strings.Contains(result, "my file.txt") {
		t.Errorf("expected 'my file.txt' arg in output, got: %s", result)
	}
}

func TestExecPipedCommandViaCmdShell(t *testing.T) {
	if !isWindowsShellDefault() {
		t.Skip("test only runs on Windows (cmd default)")
	}

	workspace := t.TempDir()
	tool := NewExecTool(NewAgentPermissions(models.RoleAdmin, workspace, config.AgentPermissionConfig{}), nil)

	// Commands with shell features (|, >, <, &) MUST still go through cmd /c
	// via SysProcAttr.CmdLine with proper quote wrapping.
	result, err := tool.ExecuteWithContext(context.Background(), map[string]interface{}{
		"command": "echo hello | findstr hello",
		"timeout": float64(5),
	})
	if err != nil {
		t.Fatalf("exec piped command failed: %v\nResult: %s", err, result)
	}
	if !strings.Contains(result, "hello") {
		t.Fatalf("expected hello in output, got: %s", result)
	}
}

func interactiveEchoCommand(t *testing.T, workspace string) string {
	t.Helper()
	helperPath := filepath.Join(workspace, "interactive_echo_helper.go")
	helperSource := `package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	fmt.Print("ready:")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && len(line) == 0 {
		return
	}
	fmt.Printf("echo:%s", line)
}
`
	if err := os.WriteFile(helperPath, []byte(helperSource), 0o644); err != nil {
		t.Fatalf("write helper program: %v", err)
	}
	return `go run ` + filepath.ToSlash(helperPath)
}
