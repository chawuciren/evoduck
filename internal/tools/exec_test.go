package tools

import (
	"context"
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

func isWindowsShellDefault() bool {
	return getDefaultShell() == "cmd"
}

func longRunningShellCommand() string {
	if isWindowsShellDefault() {
		return "ping -n 20 127.0.0.1 > nul"
	}
	return "sleep 20"
}
