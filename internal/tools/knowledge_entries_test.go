package tools

import (
	"strings"
	"testing"

	"github.com/chawuciren/evoduck/internal/knowledge"
)

func TestKnowledgeTreeReadAndSearchTools(t *testing.T) {
	dataDir := t.TempDir()
	readTool := NewKnowledgeReadTool(dataDir)
	treeTool := NewKnowledgeTreeTool(dataDir)
	searchTool := NewKnowledgeSearchTool(dataDir)

	_, err := knowledge.WriteEntry(dataDir, knowledge.WriteInput{
		Path:    "ops/release-checklist",
		Title:   "Release Checklist",
		Content: "Check migrations before release.",
		Tags:    []string{"ops", "release"},
	})
	if err != nil {
		t.Fatalf("write knowledge fixture: %v", err)
	}

	treeResult, err := treeTool.Execute(map[string]interface{}{})
	if err != nil {
		t.Fatalf("tree tool execute: %v", err)
	}
	if !strings.Contains(treeResult, "ops/release-checklist.md") || !strings.Contains(treeResult, `title="Release Checklist"`) || !strings.Contains(treeResult, "tags=ops,release") {
		t.Fatalf("unexpected tree result: %s", treeResult)
	}

	readResult, err := readTool.Execute(map[string]interface{}{"path": "ops/release-checklist.md"})
	if err != nil {
		t.Fatalf("read tool execute: %v", err)
	}
	if !strings.Contains(readResult, "Release Checklist") || !strings.Contains(readResult, "Check migrations before release.") {
		t.Fatalf("unexpected read result: %s", readResult)
	}
	if !strings.Contains(readResult, "Full path:") || !strings.Contains(readResult, "Lines:") || !strings.Contains(readResult, "→") {
		t.Fatalf("expected line-addressable read result: %s", readResult)
	}

	_, err = knowledge.WriteEntry(dataDir, knowledge.WriteInput{
		Path:    "research/interviews/session-notes",
		Title:   "Session Notes",
		Content: "Customer interview insights.",
		Tags:    []string{"research", "interview"},
	})
	if err != nil {
		t.Fatalf("write knowledge fixture: %v", err)
	}

	searchResult, err := searchTool.Execute(map[string]interface{}{"query": "interview"})
	if err != nil {
		t.Fatalf("search tool execute: %v", err)
	}
	if !strings.Contains(searchResult, "research/interviews/session-notes.md") {
		t.Fatalf("expected search result to include saved note, got: %s", searchResult)
	}
	if !strings.Contains(searchResult, "Full path:") || !strings.Contains(searchResult, "Truncated:") {
		t.Fatalf("expected locatable search metadata, got: %s", searchResult)
	}
}
