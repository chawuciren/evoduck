package gateway

import (
	"testing"

	"github.com/chawuciren/evoduck/internal/knowledge"
)

func TestKnowledgeWebsocketBackingStore(t *testing.T) {
	dataDir := t.TempDir()
	_, err := knowledge.WriteEntry(dataDir, knowledge.WriteInput{
		Path:    "product/roadmap",
		Title:   "Product Roadmap",
		Tags:    []string{"product", "roadmap"},
		Content: "Shared roadmap content",
	})
	if err != nil {
		t.Fatalf("write knowledge entry: %v", err)
	}

	entries, err := knowledge.ListEntries(dataDir, "roadmap")
	if err != nil {
		t.Fatalf("list knowledge entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 knowledge entry, got %d", len(entries))
	}
	if entries[0].Path != "product/roadmap.md" {
		t.Fatalf("unexpected entry path: %s", entries[0].Path)
	}
}
