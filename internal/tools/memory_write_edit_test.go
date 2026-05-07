package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chawuciren/evoduck/pkg/models"
)

func TestMemoryWriteRoutesAgentBootstrapFileToWorkspace(t *testing.T) {
	workspace := t.TempDir()
	dataDir := t.TempDir()
	tool := NewMemoryWriteTool(workspace, "agent-test", dataDir)

	result, err := tool.ExecuteWithUserContext(context.Background(), map[string]interface{}{
		"path":    "SOUL.md",
		"content": "agent soul",
	}, models.RoleAdmin, "alice", true, workspace)
	if err != nil {
		t.Fatalf("memory_write returned error: %v", err)
	}
	if !strings.Contains(result, "scope: agent") {
		t.Fatalf("expected agent scope result, got: %s", result)
	}
	data, err := os.ReadFile(filepath.Join(workspace, "SOUL.md"))
	if err != nil {
		t.Fatalf("read SOUL.md: %v", err)
	}
	if string(data) != "agent soul" {
		t.Fatalf("expected agent file content, got %q", string(data))
	}
}

func TestMemoryWriteRoutesUserMemoryToUserWorkspace(t *testing.T) {
	workspace := t.TempDir()
	dataDir := t.TempDir()
	tool := NewMemoryWriteTool(workspace, "agent-test", dataDir)

	result, err := tool.ExecuteWithUserContext(context.Background(), map[string]interface{}{
		"path":    "memory/2026-04-30.md",
		"content": "daily note",
	}, models.RoleEmployee, "alice", true, workspace)
	if err != nil {
		t.Fatalf("memory_write returned error: %v", err)
	}
	if !strings.Contains(result, "scope: user_daily") {
		t.Fatalf("expected user_daily scope result, got: %s", result)
	}
	data, err := os.ReadFile(filepath.Join(dataDir, "users", "agent-test_user_alice", "memory", "2026-04-30.md"))
	if err != nil {
		t.Fatalf("read daily memory: %v", err)
	}
	if string(data) != "daily note" {
		t.Fatalf("expected user memory content, got %q", string(data))
	}
}

func TestMemoryEditRoutesAgentBootstrapFileToWorkspace(t *testing.T) {
	workspace := t.TempDir()
	dataDir := t.TempDir()
	path := filepath.Join(workspace, "AGENTS.md")
	if err := os.WriteFile(path, []byte("old rules"), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	tool := NewMemoryEditTool(workspace, "agent-test", dataDir)

	result, err := tool.ExecuteWithUserContext(context.Background(), map[string]interface{}{
		"path":       "AGENTS.md",
		"old_string": "old",
		"new_string": "new",
	}, models.RoleAdmin, "alice", true, workspace)
	if err != nil {
		t.Fatalf("memory_edit returned error: %v", err)
	}
	if !strings.Contains(result, "scope: agent") {
		t.Fatalf("expected agent scope result, got: %s", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if string(data) != "new rules" {
		t.Fatalf("expected edited agent content, got %q", string(data))
	}
}

func TestMemoryEditRejectsUnknownPathWithoutScope(t *testing.T) {
	workspace := t.TempDir()
	dataDir := t.TempDir()
	tool := NewMemoryEditTool(workspace, "agent-test", dataDir)

	_, err := tool.ExecuteWithUserContext(context.Background(), map[string]interface{}{
		"path":      "notes/freeform.md",
		"operation": "append",
		"content":   "note",
	}, models.RoleAdmin, "alice", true, workspace)
	if err == nil {
		t.Fatal("expected unknown path to be rejected")
	}
	if !strings.Contains(err.Error(), "memory path is not editable") {
		t.Fatalf("unexpected error: %v", err)
	}
}
