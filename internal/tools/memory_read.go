package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/chawuciren/evoduck/pkg/models"
)

type MemoryReadTool struct {
	workspace string
	agentID   string
	dataDir   string
}

func NewMemoryReadTool(workspace string, agentID string, dataDir string) *MemoryReadTool {
	return &MemoryReadTool{workspace: workspace, agentID: agentID, dataDir: dataDir}
}

func (t *MemoryReadTool) Name() string { return "memory_read" }

func (t *MemoryReadTool) Description() string {
	return "Read an authorized markdown file by path or full_path, optionally with start_line/end_line. Agent reads are limited to first-level preset prompt files. User reads are limited to USER.md, MEMORY.md, and memory/YYYY-MM-DD.md."
}

func (t *MemoryReadTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path":       map[string]interface{}{"type": "string", "description": "Path such as AGENTS.md, SOUL.md, USER.md, MEMORY.md, or memory/YYYY-MM-DD.md"},
			"full_path":  map[string]interface{}{"type": "string", "description": "Full path returned by memory_search"},
			"user_id":    map[string]interface{}{"type": "string", "description": "Target user ID for user memory. Admin/system curator only; defaults to current user context."},
			"start_line": map[string]interface{}{"type": "integer", "description": "1-indexed start line"},
			"end_line":   map[string]interface{}{"type": "integer", "description": "1-indexed end line, inclusive"},
		},
	}
}

func (t *MemoryReadTool) Execute(args map[string]interface{}) (string, error) {
	return t.execute(args, "", false)
}

func (t *MemoryReadTool) ExecuteWithUserContext(ctx context.Context, args map[string]interface{}, role models.Role, userID string, userIsolationEnabled bool, workspace string) (string, error) {
	return t.executeWithRole(args, role, userID, userIsolationEnabled)
}

func (t *MemoryReadTool) execute(args map[string]interface{}, userID string, userIsolationEnabled bool) (string, error) {
	return t.executeWithRole(args, models.RoleCustomer, userID, userIsolationEnabled)
}

func (t *MemoryReadTool) executeWithRole(args map[string]interface{}, role models.Role, userID string, userIsolationEnabled bool) (string, error) {
	fullPath, displayPath, scope, err := t.resolveMemoryReadPathWithRole(args, role, userID, userIsolationEnabled)
	if err != nil {
		return "", err
	}
	startLine := parseIntArg(args["start_line"], 0)
	endLine := parseIntArg(args["end_line"], 0)
	content, start, end, total, err := readMarkdownLines(fullPath, startLine, endLine)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Path: %s\nFull path: %s\nScope: %s\nLines: %d-%d of %d\n\n%s", displayPath, fullPath, scope, start, end, total, content), nil
}

func (t *MemoryReadTool) resolveMemoryReadPath(args map[string]interface{}, userID string, userIsolationEnabled bool) (string, string, string, error) {
	return t.resolveMemoryReadPathWithRole(args, models.RoleCustomer, userID, userIsolationEnabled)
}

func (t *MemoryReadTool) resolveMemoryReadPathWithRole(args map[string]interface{}, role models.Role, userID string, userIsolationEnabled bool) (string, string, string, error) {
	path, _ := args["path"].(string)
	fullPath, _ := args["full_path"].(string)
	path = strings.TrimSpace(path)
	fullPath = strings.TrimSpace(fullPath)

	if fullPath != "" {
		return t.resolveMemoryFullPath(fullPath, args, role, userID, userIsolationEnabled)
	}
	if path == "" {
		return "", "", "", fmt.Errorf("path or full_path is required")
	}
	relPath, err := cleanMemoryRelPath(path)
	if err != nil {
		return "", "", "", err
	}

	if isAllowedAgentMemoryReadPath(relPath) {
		full := filepath.Join(t.workspace, filepath.FromSlash(relPath))
		if !isPathWithin(full, t.workspace) {
			return "", "", "", fmt.Errorf("memory path escapes workspace")
		}
		return full, relPath, "agent", nil
	}

	if userIsolationEnabled {
		targetUserID, err := memoryTargetUserID(args, role, userID, userIsolationEnabled)
		if err != nil {
			return "", "", "", err
		}
		if !isAllowedUserMemoryPath(relPath) {
			return "", "", "", fmt.Errorf("user scope only allows USER.md, MEMORY.md, or memory/YYYY-MM-DD.md, got: %s", path)
		}
		userDir := memoryUserWorkspace(t.dataDir, t.agentID, targetUserID)
		full := filepath.Join(userDir, filepath.FromSlash(relPath))
		if !isPathWithin(full, userDir) {
			return "", "", "", fmt.Errorf("memory path escapes user directory")
		}
		return full, relPath, memoryScopeForRel(relPath, "user"), nil
	}

	return "", "", "", fmt.Errorf("memory path is not authorized: %s", path)
}

func (t *MemoryReadTool) resolveMemoryFullPath(fullPath string, args map[string]interface{}, role models.Role, userID string, userIsolationEnabled bool) (string, string, string, error) {
	cleanFullPath := filepath.Clean(fullPath)
	if isPathWithin(cleanFullPath, t.workspace) {
		rel, _ := filepath.Rel(t.workspace, cleanFullPath)
		relPath := filepath.ToSlash(rel)
		if isAllowedAgentMemoryReadPath(relPath) {
			return cleanFullPath, relPath, "agent", nil
		}
	}

	if userIsolationEnabled {
		targetUserID, err := memoryTargetUserID(args, role, userID, userIsolationEnabled)
		if err != nil {
			return "", "", "", err
		}
		userDir := memoryUserWorkspace(t.dataDir, t.agentID, targetUserID)
		if isPathWithin(cleanFullPath, userDir) {
			rel, _ := filepath.Rel(userDir, cleanFullPath)
			relPath := filepath.ToSlash(rel)
			if isAllowedUserMemoryPath(relPath) {
				return cleanFullPath, relPath, memoryScopeForRel(relPath, "user"), nil
			}
		}
	}

	return "", "", "", fmt.Errorf("memory_read cannot read outside authorized memory paths")
}
