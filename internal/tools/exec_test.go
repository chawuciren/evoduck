package tools

import (
	"context"
	"os"
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
