package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/chawuciren/evoduck/pkg/textedit"
)

type MemoryWriteTool struct {
	workspace string
	agentID   string
	dataDir   string
}

func NewMemoryWriteTool(workspace string, agentID string, dataDir string) *MemoryWriteTool {
	return &MemoryWriteTool{
		workspace: workspace,
		agentID:   agentID,
		dataDir:   dataDir,
	}
}

func (t *MemoryWriteTool) Name() string {
	return "memory_write"
}

func (t *MemoryWriteTool) Description() string {
	return "Create or overwrite an agent or user memory/bootstrap file. The target is routed automatically by path; do not pass scope.\n\n" +
		"## Agent Paths\n\n" +
		"- AGENTS.md: Agent operating instructions\n" +
		"- SOUL.md: Agent identity, mission, tone, and boundaries\n" +
		"- TOOLS.md, IDENTITY.md, HEARTBEAT.md, BOOTSTRAP.md: Agent bootstrap files\n\n" +
		"## User Memory Paths\n\n" +
		"- USER.md: User profile\n" +
		"- MEMORY.md: User-specific long-term memory\n" +
		"- memory/YYYY-MM-DD.md: User-specific daily medium memory\n\n" +
		"## Usage\n\n" +
		"{\"path\": \"SOUL.md\", \"content\": \"...\"}\n" +
		"{\"path\": \"USER.md\", \"content\": \"...\"}\n" +
		"{\"path\": \"MEMORY.md\", \"content\": \"...\"}\n" +
		"{\"path\": \"memory/2025-01-01.md\", \"content\": \"...\"}\n\n" +
		"## Security\n" +
		"- Agent writes are limited to first-level preset bootstrap files\n" +
		"- User writes are limited to USER.md, MEMORY.md, or memory/YYYY-MM-DD.md"
}

func (t *MemoryWriteTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path. Agent files: AGENTS.md, SOUL.md, TOOLS.md, IDENTITY.md, HEARTBEAT.md, BOOTSTRAP.md. User files: USER.md, MEMORY.md, memory/YYYY-MM-DD.md.",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Markdown content to write",
			},
			"user_id": map[string]interface{}{
				"type":        "string",
				"description": "Target user ID for user memory. Admin/system curator only; defaults to current user context.",
			},
		},
		"required": []string{"path", "content"},
	}
}

func (t *MemoryWriteTool) Execute(args map[string]interface{}) (string, error) {
	return t.execute(args, "", false)
}

func (t *MemoryWriteTool) ExecuteWithUserContext(ctx context.Context, args map[string]interface{}, role models.Role, userID string, userIsolationEnabled bool, workspace string) (string, error) {
	return t.executeWithRole(args, role, userID, userIsolationEnabled)
}

func (t *MemoryWriteTool) execute(args map[string]interface{}, userID string, userIsolationEnabled bool) (string, error) {
	return t.executeWithRole(args, models.RoleCustomer, userID, userIsolationEnabled)
}

func (t *MemoryWriteTool) executeWithRole(args map[string]interface{}, role models.Role, userID string, userIsolationEnabled bool) (string, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)

	path = strings.TrimSpace(path)
	content = strings.TrimSpace(content)

	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if content == "" {
		return "", fmt.Errorf("content is required")
	}
	fullPath, displayPath, routedScope, err := t.resolveMemoryWritePath(path, role, userID, userIsolationEnabled, args)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return "", fmt.Errorf("create memory directory: %w", err)
	}

	edit := textedit.Edit{
		Operation: textedit.OpWrite,
		Content:   content,
	}
	_, err = textedit.ApplyFile(edit, fullPath, 0644)
	if err != nil {
		return "", fmt.Errorf("write memory file: %w", err)
	}

	return fmt.Sprintf("Successfully wrote %s (scope: %s)\nPath: %s\nFull path: %s", displayPath, routedScope, displayPath, fullPath), nil
}

func (t *MemoryWriteTool) resolveMemoryWritePath(path string, role models.Role, userID string, userIsolationEnabled bool, args map[string]interface{}) (string, string, string, error) {
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
			return "", "", "", fmt.Errorf("user memory write requires user isolation")
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

	return "", "", "", fmt.Errorf("memory path is not writable: %s", path)
}

func (t *MemoryWriteTool) getUserWorkspace(userID string) string {
	return memoryUserWorkspace(t.dataDir, t.agentID, userID)
}

func (t *MemoryWriteTool) SetWorkspace(workspace string) {
	t.workspace = workspace
}
