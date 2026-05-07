package tools

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/chawuciren/evoduck/pkg/textedit"
)

type FileEditTool struct {
	permissions AgentPermissions
}

func NewFileEditTool(permissions AgentPermissions) *FileEditTool {
	return &FileEditTool{permissions: permissions}
}

func (t *FileEditTool) Name() string {
	return "file_edit"
}

func (t *FileEditTool) Description() string {
	return `Partially edit a single text file in the agent's workspace.

Use file_read first to inspect the target file and provide exact anchors or replacement text.

Supported operations: append, prepend, replace_text, insert_before, insert_after, replace_between.
Line-number editing is intentionally not supported; line numbers are for reading and references only.`
}

func (t *FileEditTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path":              map[string]interface{}{"type": "string", "description": "File path relative to workspace"},
			"operation":         map[string]interface{}{"type": "string", "description": "Edit operation", "enum": []string{"append", "prepend", "replace_text", "insert_before", "insert_after", "replace_between"}},
			"content":           map[string]interface{}{"type": "string", "description": "Text content for append, prepend, insert, or replace_between"},
			"old_text":          map[string]interface{}{"type": "string", "description": "Exact text to replace for replace_text"},
			"new_text":          map[string]interface{}{"type": "string", "description": "Replacement text for replace_text"},
			"replace_all":       map[string]interface{}{"type": "boolean", "description": "Replace all matches for replace_text (default false)"},
			"anchor":            map[string]interface{}{"type": "string", "description": "Exact anchor text for insert_before or insert_after"},
			"start_marker":      map[string]interface{}{"type": "string", "description": "Exact start marker for replace_between"},
			"end_marker":        map[string]interface{}{"type": "string", "description": "Exact end marker for replace_between"},
			"occurrence":        map[string]interface{}{"type": "integer", "description": "1-indexed occurrence to target (default 1; replace_text without occurrence requires unique old_text)"},
			"create_if_missing": map[string]interface{}{"type": "boolean", "description": "Create file for append/prepend when missing (default true)"},
			"include_markers":   map[string]interface{}{"type": "boolean", "description": "Replace markers too for replace_between (default false)"},
			"mode":              map[string]interface{}{"type": "string", "description": "File permissions when creating a file (optional, default 0644)"},
		},
		"required": []string{"path", "operation"},
	}
}

func (t *FileEditTool) Execute(args map[string]interface{}) (string, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return "", fmt.Errorf("path is required")
	}

	fullPath, err := t.permissions.ResolvePath(path)
	if err != nil {
		return "", err
	}
	if err := t.permissions.CanAccessPath(fullPath); err != nil {
		return "", err
	}
	if isSensitiveFilePath(fullPath) {
		return "", fmt.Errorf("cannot edit sensitive file: %s", path)
	}

	edit := buildTextEdit(args)
	if !isAllowedFileEditOperation(edit.Operation) {
		return "", fmt.Errorf("file_edit does not support operation %q", edit.Operation)
	}

	res, err := textedit.ApplyFile(edit, fullPath, textedit.ParseFileMode(args["mode"]))
	if err != nil {
		return "", err
	}
	return formatResult(res, path), nil
}

func isAllowedFileEditOperation(operation textedit.Operation) bool {
	switch operation {
	case textedit.OpAppend, textedit.OpPrepend, textedit.OpReplaceText, textedit.OpInsertBefore, textedit.OpInsertAfter, textedit.OpReplaceBetween:
		return true
	default:
		return false
	}
}

func isSensitiveFilePath(path string) bool {
	filename := strings.ToLower(filepath.Base(path))
	sensitiveFiles := []string{
		".env",
		".env.local",
		".env.production",
		".env.development",
		"credentials.json",
		"secrets.json",
		"id_rsa",
		"id_ed25519",
		".gitconfig",
		".npmrc",
		".pypirc",
	}
	for _, sensitive := range sensitiveFiles {
		if filename == sensitive {
			return true
		}
	}
	return false
}
