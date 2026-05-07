package tools

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chawuciren/evoduck/pkg/textedit"
)

type FileWriteTool struct {
	permissions AgentPermissions
}

func NewFileWriteTool(permissions AgentPermissions) *FileWriteTool {
	return &FileWriteTool{
		permissions: permissions,
	}
}

func (t *FileWriteTool) Name() string {
	return "file_write"
}

func (t *FileWriteTool) Description() string {
	return `Create or overwrite a file in the agent's workspace.

**Features:**
- Create new files or overwrite existing ones
- Create parent directories if they don't exist
- Supports text and binary content (base64)
- For partial edits, use file_edit

**Security:**
- Only files within the workspace can be written
- Cannot overwrite critical files (.env, credentials, etc.)

**Parameters:**
- path: File path (relative to workspace)
- operation: create | write (optional, default write)
- content: Text content to write
- content_base64: Base64-encoded binary content (alternative to content)
- mode: File permissions (optional, default 0644)`
}

func (t *FileWriteTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "File path relative to workspace",
			},
			"operation": map[string]interface{}{
				"type":        "string",
				"description": "Write operation: create or write",
				"enum":        []string{"create", "write"},
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Text content to write (use this OR content_base64)",
			},
			"content_base64": map[string]interface{}{
				"type":        "string",
				"description": "Base64-encoded binary content (alternative to content)",
			},
			"mode": map[string]interface{}{
				"type":        "string",
				"description": "File permissions (optional, e.g., '0755')",
			},
		},
		"required": []string{"path"},
	}
}

func (t *FileWriteTool) Execute(args map[string]interface{}) (string, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return "", fmt.Errorf("path is required")
	}

	fullPath, err := t.resolvePath(path)
	if err != nil {
		return "", err
	}

	if err := t.validatePath(fullPath); err != nil {
		return "", err
	}

	if t.isSensitiveFile(fullPath) {
		return "", fmt.Errorf("cannot write to sensitive file: %s", path)
	}

	operation := parseFileWriteOperation(args)
	if operation != textedit.OpCreate && operation != textedit.OpWrite {
		return "", fmt.Errorf("file_write only supports create or write; use file_edit for partial edits")
	}
	content, err := fileWriteContent(args)
	if err != nil {
		return "", err
	}
	mode := textedit.ParseFileMode(args["mode"])

	if operation == textedit.OpCreate {
		if _, err := os.Stat(fullPath); err == nil {
			return "", fmt.Errorf("file already exists: %s", fullPath)
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("stat file: %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return "", fmt.Errorf("create directories: %w", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), mode); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	res := &textedit.Result{Operation: operation, Message: "Successfully wrote file"}
	if operation == textedit.OpCreate {
		res.Message = "Successfully created file"
	}

	return formatResult(res, path), nil
}

func parseFileWriteOperation(args map[string]interface{}) textedit.Operation {
	raw, _ := args["operation"].(string)
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return textedit.OpWrite
	}
	return textedit.Operation(raw)
}

func fileWriteContent(args map[string]interface{}) (string, error) {
	if content, ok := args["content"].(string); ok {
		return content, nil
	}
	if base64Content, ok := args["content_base64"].(string); ok && base64Content != "" {
		decoded, err := base64.StdEncoding.DecodeString(base64Content)
		if err != nil {
			return "", fmt.Errorf("decode content_base64: %w", err)
		}
		return string(decoded), nil
	}
	return "", nil
}

func buildTextEdit(args map[string]interface{}) textedit.Edit {
	operation := parseOperation(args)

	edit := textedit.Edit{
		Operation:       operation,
		Content:         stringArg(args, "content"),
		OldText:         stringArg(args, "old_text"),
		NewText:         stringArg(args, "new_text"),
		StartLine:       intArg(args, "start_line"),
		EndLine:         intArg(args, "end_line"),
		Line:            intArg(args, "line"),
		Expected:        stringArg(args, "expected"),
		Anchor:          stringArg(args, "anchor"),
		StartMarker:     stringArg(args, "start_marker"),
		EndMarker:       stringArg(args, "end_marker"),
		ReplaceAll:      boolArg(args, "replace_all"),
		Occurrence:      intArg(args, "occurrence"),
		CreateIfMissing: boolArgDefault(args, "create_if_missing", true),
		IncludeMarkers:  boolArg(args, "include_markers"),
	}

	// Handle content_base64 for create/write
	if edit.Content == "" && (edit.Operation == textedit.OpCreate || edit.Operation == textedit.OpWrite) {
		if base64Content, ok := args["content_base64"].(string); ok && base64Content != "" {
			decoded, err := base64.StdEncoding.DecodeString(base64Content)
			if err == nil {
				edit.Content = string(decoded)
			}
		}
	}

	return edit
}

func parseOperation(args map[string]interface{}) textedit.Operation {
	raw, _ := args["operation"].(string)
	raw = strings.ToLower(strings.TrimSpace(raw))

	if raw == "" {
		if _, hasOld := args["old_text"]; hasOld {
			if _, hasNew := args["new_text"]; hasNew {
				return textedit.OpReplaceText
			}
		}
		return textedit.OpWrite
	}

	return textedit.Operation(raw)
}

func stringArg(args map[string]interface{}, key string) string {
	val, _ := args[key].(string)
	return val
}

func intArg(args map[string]interface{}, key string) int {
	return parseIntArg(args[key], 0)
}

func boolArg(args map[string]interface{}, key string) bool {
	val, _ := args[key].(bool)
	return val
}

func boolArgDefault(args map[string]interface{}, key string, fallback bool) bool {
	if val, ok := args[key].(bool); ok {
		return val
	}
	return fallback
}

func formatResult(res *textedit.Result, displayPath string) string {
	if res.Message != "" {
		return res.Message + " in " + displayPath
	}
	return fmt.Sprintf("Successfully %s %s", res.Operation, displayPath)
}

func (t *FileWriteTool) resolvePath(path string) (string, error) {
	return t.permissions.ResolvePath(path)
}

func (t *FileWriteTool) validatePath(path string) error {
	return t.permissions.CanAccessPath(path)
}

func (t *FileWriteTool) isSensitiveFile(path string) bool {
	return isSensitiveFilePath(path)
}

func (t *FileWriteTool) SetWorkspace(workspace string) {
	t.permissions.Workspace = workspace
}
