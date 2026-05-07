package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemorySearchRequiresUserContext(t *testing.T) {
	workspace := t.TempDir()
	dataDir := t.TempDir()

	tool := NewMemorySearchTool(workspace, "agent-test", dataDir)
	result, err := tool.Execute(map[string]interface{}{"query": "destructive actions"})
	if err != nil {
		t.Fatalf("memory search execute: %v", err)
	}
	if !strings.Contains(result, "requires user context") {
		t.Fatalf("expected user context requirement, got: %s", result)
	}
}

func TestMemoryReadRestrictsToMemoryScope(t *testing.T) {
	workspace := t.TempDir()
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("one\ntwo\nthree"), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "outside.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatalf("write outside.txt: %v", err)
	}

	tool := NewMemoryReadTool(workspace, "agent-test", dataDir)
	result, err := tool.Execute(map[string]interface{}{"path": "AGENTS.md", "start_line": 2, "end_line": 2})
	if err != nil {
		t.Fatalf("memory read: %v", err)
	}
	if !strings.Contains(result, "Path: AGENTS.md") || !strings.Contains(result, "   2→two") {
		t.Fatalf("unexpected memory read result: %s", result)
	}
	if _, err := tool.Execute(map[string]interface{}{"path": "MEMORY.md"}); err == nil {
		t.Fatal("expected agent MEMORY.md to be rejected")
	}
	if _, err := tool.Execute(map[string]interface{}{"path": "outside.txt"}); err == nil {
		t.Fatal("expected non-markdown workspace file to be rejected")
	}
}
