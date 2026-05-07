package tools

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/chawuciren/evoduck/internal/knowledge"
	"github.com/chawuciren/evoduck/pkg/textedit"
)

type KnowledgeEditTool struct {
	dataDir string
}

func NewKnowledgeEditTool(dataDir string) *KnowledgeEditTool {
	return &KnowledgeEditTool{dataDir: dataDir}
}

func (t *KnowledgeEditTool) Name() string {
	return "knowledge_edit"
}

func (t *KnowledgeEditTool) Description() string {
	return "Partially edit a shared knowledge entry.\n\n" +
		"Use knowledge_read first to inspect the target file and provide exact old_string.\n\n" +
		"## Supported Operations\n\n" +
		"- replace_text: Replace exact text match\n" +
		"- append: Append content to end\n" +
		"- prepend: Prepend content to start\n\n" +
		"## Usage\n\n" +
		"{\"path\": \"product/roadmap.md\", \"old_string\": \"...\", \"new_string\": \"...\"}\n" +
		"{\"path\": \"tech/architecture.md\", \"operation\": \"append\", \"content\": \"...\"}\n\n" +
		"## Security\n" +
		"- Only edits files in data/knowledge/ directory\n" +
		"- Cannot escape knowledge root"
}

func (t *KnowledgeEditTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Knowledge entry path relative to knowledge root",
			},
			"operation": map[string]interface{}{
				"type":        "string",
				"description": "Edit operation: replace_text, append, prepend (default: replace_text when old_string provided)",
				"enum":        []string{"replace_text", "append", "prepend"},
			},
			"old_string": map[string]interface{}{
				"type":        "string",
				"description": "Exact text to replace for replace_text",
			},
			"new_string": map[string]interface{}{
				"type":        "string",
				"description": "Replacement text for replace_text",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Content for append or prepend operations",
			},
			"replace_all": map[string]interface{}{
				"type":        "boolean",
				"description": "Replace all matches for replace_text (default false)",
			},
		},
		"required": []string{"path"},
	}
}

func (t *KnowledgeEditTool) Execute(args map[string]interface{}) (string, error) {
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

	edit := t.buildKnowledgeEdit(args)
	if !isAllowedKnowledgeEditOperation(edit.Operation) {
		return "", fmt.Errorf("knowledge_edit does not support operation %q", edit.Operation)
	}

	if edit.Operation == textedit.OpReplaceText {
		if edit.OldText == "" {
			return "", fmt.Errorf("old_string is required for replace_text")
		}
	} else {
		if edit.Content == "" {
			return "", fmt.Errorf("content is required for %s", edit.Operation)
		}
	}

	edit.CreateIfMissing = true

	res, err := textedit.ApplyFile(edit, fullPath, 0644)
	if err != nil {
		return "", fmt.Errorf("edit knowledge entry: %w", err)
	}

	return fmt.Sprintf("%s\nPath: %s\nFull path: %s", res.Message, normalized, fullPath), nil
}

func (t *KnowledgeEditTool) buildKnowledgeEdit(args map[string]interface{}) textedit.Edit {
	operation := t.parseKnowledgeEditOperation(args)

	return textedit.Edit{
		Operation:  operation,
		Content:    stringArg(args, "content"),
		OldText:    stringArg(args, "old_string"),
		NewText:    stringArg(args, "new_string"),
		ReplaceAll: boolArg(args, "replace_all"),
	}
}

func (t *KnowledgeEditTool) parseKnowledgeEditOperation(args map[string]interface{}) textedit.Operation {
	raw, _ := args["operation"].(string)
	raw = strings.ToLower(strings.TrimSpace(raw))

	if raw == "" {
		if _, hasOld := args["old_string"]; hasOld {
			return textedit.OpReplaceText
		}
		return textedit.OpWrite
	}

	return textedit.Operation(raw)
}

func isAllowedKnowledgeEditOperation(operation textedit.Operation) bool {
	switch operation {
	case textedit.OpReplaceText, textedit.OpAppend, textedit.OpPrepend:
		return true
	default:
		return false
	}
}

func (t *KnowledgeEditTool) SetDataDir(dataDir string) {
	t.dataDir = dataDir
}