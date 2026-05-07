package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/chawuciren/evoduck/pkg/textedit"
)

type MemoryEditTool struct {
	workspace string
	agentID   string
	dataDir   string
}

func NewMemoryEditTool(workspace string, agentID string, dataDir string) *MemoryEditTool {
	return &MemoryEditTool{
		workspace: workspace,
		agentID:   agentID,
		dataDir:   dataDir,
	}
}

func (t *MemoryEditTool) Name() string {
	return "memory_edit"
}

func (t *MemoryEditTool) Description() string {
	return "Partially edit an agent or user memory/bootstrap file. The target is routed automatically by path; do not pass scope.\n\n" +
		"Use memory_read first to inspect the target file and provide exact old_string.\n\n" +
		"## Agent Paths\n\n" +
		"- AGENTS.md: Agent operating instructions\n" +
		"- SOUL.md: Agent identity, mission, tone, and boundaries\n" +
		"- TOOLS.md, IDENTITY.md, HEARTBEAT.md, BOOTSTRAP.md: Agent bootstrap files\n\n" +
		"## User Memory Paths\n\n" +
		"- USER.md: User profile\n" +
		"- MEMORY.md: User-specific long-term memory\n" +
		"- memory/YYYY-MM-DD.md: User-specific daily medium memory\n\n" +
		"## Supported Operations\n\n" +
		"- replace_text: Replace exact text match\n" +
		"- append: Append content to end\n" +
		"- prepend: Prepend content to start\n\n" +
		"## Usage\n\n" +
		"{\"path\": \"SOUL.md\", \"old_string\": \"...\", \"new_string\": \"...\"}\n" +
		"{\"path\": \"MEMORY.md\", \"old_string\": \"...\", \"new_string\": \"...\"}\n" +
		"{\"path\": \"memory/2025-01-01.md\", \"operation\": \"append\", \"content\": \"...\"}\n\n" +
		"## Security\n" +
		"- Agent edits are limited to first-level preset bootstrap files\n" +
		"- User edits are limited to USER.md, MEMORY.md, or memory/YYYY-MM-DD.md"
}

func (t *MemoryEditTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path. Agent files: AGENTS.md, SOUL.md, TOOLS.md, IDENTITY.md, HEARTBEAT.md, BOOTSTRAP.md. User files: USER.md, MEMORY.md, memory/YYYY-MM-DD.md.",
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
			"user_id": map[string]interface{}{
				"type":        "string",
				"description": "Target user ID for user memory. Admin/system curator only; defaults to current user context.",
			},
			"replace_all": map[string]interface{}{
				"type":        "boolean",
				"description": "Replace all matches for replace_text (default false)",
			},
		},
		"required": []string{"path"},
	}
}

func (t *MemoryEditTool) Execute(args map[string]interface{}) (string, error) {
	return t.execute(args, "", false)
}

func (t *MemoryEditTool) ExecuteWithUserContext(ctx context.Context, args map[string]interface{}, role models.Role, userID string, userIsolationEnabled bool, workspace string) (string, error) {
	return t.executeWithRole(args, role, userID, userIsolationEnabled)
}

func (t *MemoryEditTool) execute(args map[string]interface{}, userID string, userIsolationEnabled bool) (string, error) {
	return t.executeWithRole(args, models.RoleCustomer, userID, userIsolationEnabled)
}

func (t *MemoryEditTool) executeWithRole(args map[string]interface{}, role models.Role, userID string, userIsolationEnabled bool) (string, error) {
	path, _ := args["path"].(string)

	path = strings.TrimSpace(path)

	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	fullPath, displayPath, routedScope, err := t.resolveMemoryEditPath(path, role, userID, userIsolationEnabled, args)
	if err != nil {
		return "", err
	}

	edit := t.buildMemoryEdit(args)
	if !isAllowedMemoryEditOperation(edit.Operation) {
		return "", fmt.Errorf("memory_edit does not support operation %q", edit.Operation)
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
		return "", fmt.Errorf("edit memory file: %w", err)
	}

	return fmt.Sprintf("%s (scope: %s)\nPath: %s\nFull path: %s", res.Message, routedScope, displayPath, fullPath), nil
}

func (t *MemoryEditTool) buildMemoryEdit(args map[string]interface{}) textedit.Edit {
	operation := t.parseMemoryEditOperation(args)

	return textedit.Edit{
		Operation:   operation,
		Content:     stringArg(args, "content"),
		OldText:     stringArg(args, "old_string"),
		NewText:     stringArg(args, "new_string"),
		ReplaceAll:  boolArg(args, "replace_all"),
	}
}

func (t *MemoryEditTool) parseMemoryEditOperation(args map[string]interface{}) textedit.Operation {
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

func isAllowedMemoryEditOperation(operation textedit.Operation) bool {
	switch operation {
	case textedit.OpReplaceText, textedit.OpAppend, textedit.OpPrepend:
		return true
	default:
		return false
	}
}

func (t *MemoryEditTool) resolveMemoryEditPath(path string, role models.Role, userID string, userIsolationEnabled bool, args map[string]interface{}) (string, string, string, error) {
	slashPath, err := cleanMemoryRelPath(path)
	if err != nil {
		return "", "", "", err
	}

	if isAllowedAgentMemoryWritePath(slashPath) {
		fullPath := filepath.Join(t.workspace, filepath.FromSlash(slashPath))
		if !isPathWithin(fullPath, t.workspace) {
			return "", "", "", fmt.Errorf("memory path escapes workspace")
		}
		return fullPath, slashPath, "agent", nil
	}

	if isAllowedUserMemoryPath(slashPath) {
		if !userIsolationEnabled {
			return "", "", "", fmt.Errorf("user memory edit requires user isolation")
		}
		targetUserID, err := memoryTargetUserID(args, role, userID, userIsolationEnabled)
		if err != nil {
			return "", "", "", err
		}

		userDir := t.getUserWorkspace(targetUserID)
		fullPath := filepath.Join(userDir, filepath.FromSlash(slashPath))
		if !isPathWithin(fullPath, userDir) {
			return "", "", "", fmt.Errorf("memory path escapes user directory")
		}
		return fullPath, slashPath, memoryScopeForRel(slashPath, "user"), nil
	}

	return "", "", "", fmt.Errorf("memory path is not editable: %s", path)
}

func (t *MemoryEditTool) getUserWorkspace(userID string) string {
	return memoryUserWorkspace(t.dataDir, t.agentID, userID)
}

func (t *MemoryEditTool) SetWorkspace(workspace string) {
	t.workspace = workspace
}
