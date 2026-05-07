package knowledge

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAndReadEntry(t *testing.T) {
	dataDir := t.TempDir()
	entry, err := WriteEntry(dataDir, WriteInput{
		Path:    "product/roadmap",
		Title:   "Product Roadmap",
		Tags:    []string{"product", "roadmap"},
		Content: "# Q3\nFocus on onboarding.",
	})
	if err != nil {
		t.Fatalf("write entry: %v", err)
	}
	if entry.Path != "product/roadmap.md" {
		t.Fatalf("unexpected path: %s", entry.Path)
	}

	loaded, err := ReadEntry(dataDir, "product/roadmap.md")
	if err != nil {
		t.Fatalf("read entry: %v", err)
	}
	if loaded.Title != "Product Roadmap" {
		t.Fatalf("unexpected title: %s", loaded.Title)
	}
	if len(loaded.Tags) != 2 {
		t.Fatalf("expected tags, got: %#v", loaded.Tags)
	}

	if _, err := normalizeEntryPath("../escape"); err == nil {
		t.Fatal("expected invalid path error")
	}

	root := RootDir(dataDir)
	if got := filepath.Join(root, filepath.FromSlash(loaded.Path)); got == "" {
		t.Fatal("expected non-empty absolute path")
	}
}

func TestListEntriesMatchesQuery(t *testing.T) {
	dataDir := t.TempDir()
	_, _ = WriteEntry(dataDir, WriteInput{Path: "ops/release-checklist", Tags: []string{"ops", "release"}, Content: "Release procedure"})
	_, _ = WriteEntry(dataDir, WriteInput{Path: "product/personas", Tags: []string{"research"}, Content: "User personas"})

	entries, err := ListEntries(dataDir, "release")
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one matching entry, got %d", len(entries))
	}
	if entries[0].Path != "ops/release-checklist.md" {
		t.Fatalf("unexpected entry path: %s", entries[0].Path)
	}
}

func TestDeleteEntryRemovesFileAndEmptyDirectory(t *testing.T) {
	dataDir := t.TempDir()
	_, err := WriteEntry(dataDir, WriteInput{Path: "ops/releases/checklist", Content: "delete me"})
	if err != nil {
		t.Fatalf("write entry: %v", err)
	}

	if err := DeleteEntry(dataDir, "ops/releases/checklist.md"); err != nil {
		t.Fatalf("delete entry: %v", err)
	}

	if _, err := ReadEntry(dataDir, "ops/releases/checklist.md"); err == nil {
		t.Fatal("expected deleted entry read to fail")
	}

	entries, err := ListEntries(dataDir, "")
	if err != nil {
		t.Fatalf("list entries after delete: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no entries after delete, got %d", len(entries))
	}
}

func TestMoveEntryMovesFileAndCleansOldDirectory(t *testing.T) {
	dataDir := t.TempDir()
	_, err := WriteEntry(dataDir, WriteInput{
		Path:    "ops/releases/checklist",
		Title:   "Checklist",
		Tags:    []string{"ops"},
		Content: "move me",
	})
	if err != nil {
		t.Fatalf("write entry: %v", err)
	}

	moved, err := MoveEntry(dataDir, "ops/releases/checklist.md", "product/runbooks/checklist.md")
	if err != nil {
		t.Fatalf("move entry: %v", err)
	}
	if moved.Path != "product/runbooks/checklist.md" {
		t.Fatalf("unexpected moved path: %s", moved.Path)
	}
	if moved.Directory != "product/runbooks" {
		t.Fatalf("unexpected moved directory: %s", moved.Directory)
	}

	if _, err := ReadEntry(dataDir, "ops/releases/checklist.md"); err == nil {
		t.Fatal("expected old path to be gone after move")
	}
	if _, err := ReadEntry(dataDir, "product/runbooks/checklist.md"); err != nil {
		t.Fatalf("expected moved entry to exist: %v", err)
	}
}

func TestCreateDirectoryCreatesEmptyKnowledgeFolder(t *testing.T) {
	dataDir := t.TempDir()
	dir, err := CreateDirectory(dataDir, "research/interviews")
	if err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if dir != "research/interviews" {
		t.Fatalf("unexpected directory: %s", dir)
	}
}

func TestDeleteDirectoryRemovesEmptyKnowledgeFolder(t *testing.T) {
	dataDir := t.TempDir()
	_, err := CreateDirectory(dataDir, "research/interviews")
	if err != nil {
		t.Fatalf("create directory: %v", err)
	}

	dir, err := DeleteDirectory(dataDir, "research/interviews")
	if err != nil {
		t.Fatalf("delete directory: %v", err)
	}
	if dir != "research/interviews" {
		t.Fatalf("unexpected directory: %s", dir)
	}
}

func TestDeleteDirectoryRejectsNonEmptyFolder(t *testing.T) {
	dataDir := t.TempDir()
	_, err := WriteEntry(dataDir, WriteInput{Path: "research/interviews/session-notes", Content: "present"})
	if err != nil {
		t.Fatalf("write entry: %v", err)
	}

	if _, err := DeleteDirectory(dataDir, "research/interviews"); err == nil {
		t.Fatal("expected delete directory to fail for non-empty folder")
	}
}

func TestListDirectoriesIncludesPersistedKnowledgeFolders(t *testing.T) {
	dataDir := t.TempDir()
	_, err := CreateDirectory(dataDir, "research/interviews")
	if err != nil {
		t.Fatalf("create directory: %v", err)
	}
	_, err = WriteEntry(dataDir, WriteInput{Path: "product/roadmap", Content: "planned"})
	if err != nil {
		t.Fatalf("write entry: %v", err)
	}

	directories, err := ListDirectories(dataDir)
	if err != nil {
		t.Fatalf("list directories: %v", err)
	}
	joined := strings.Join(directories, ",")
	if !strings.Contains(joined, "research") || !strings.Contains(joined, "research/interviews") || !strings.Contains(joined, "product") {
		t.Fatalf("unexpected directories: %v", directories)
	}
}
