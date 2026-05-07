package tools

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/chawuciren/evoduck/internal/knowledge"
)

type KnowledgeDeleteTool struct {
	dataDir string
}

func NewKnowledgeDeleteTool(dataDir string) *KnowledgeDeleteTool {
	return &KnowledgeDeleteTool{dataDir: dataDir}
}

func (t *KnowledgeDeleteTool) Name() string {
	return "knowledge_delete"
}

func (t *KnowledgeDeleteTool) Description() string {
	return "Delete a shared knowledge entry.\n\n" +
		"## Usage\n\n" +
		"{\"path\": \"product/old-roadmap.md\"}\n" +
		"{\"path\": \"tech/deprecated.md\"}\n\n" +
		"## Security\n" +
		"- Only deletes files in data/knowledge/ directory\n" +
		"- Cannot escape knowledge root\n" +
		"- Parent directories are cleaned up if empty"
}

func (t *KnowledgeDeleteTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Knowledge entry path to delete",
			},
		},
		"required": []string{"path"},
	}
}

func (t *KnowledgeDeleteTool) Execute(args map[string]interface{}) (string, error) {
	path, _ := args["path"].(string)

	path = strings.TrimSpace(path)

	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	root, err := knowledge.EnsureRoot(t.dataDir)
	if err != nil {
		return "", fmt.Errorf("ensure knowledge root: %w", err)
	}

	normalized, err := knowledge.NormalizeEntryPath(path)
	if err != nil {
		return "", err
	}

	fullPath := filepath.Join(root, filepath.FromSlash(normalized))
	if !isPathWithin(fullPath, root) {
		return "", fmt.Errorf("knowledge path escapes root")
	}

	if err := knowledge.DeleteEntry(t.dataDir, path); err != nil {
		return "", fmt.Errorf("delete knowledge entry: %w", err)
	}

	return fmt.Sprintf("Successfully deleted knowledge entry\nPath: %s\nFull path: %s", normalized, fullPath), nil
}

func (t *KnowledgeDeleteTool) SetDataDir(dataDir string) {
	t.dataDir = dataDir
}