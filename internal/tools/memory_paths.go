package tools

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/chawuciren/evoduck/pkg/models"
)

var userDailyMemoryPathRe = regexp.MustCompile(`^memory/(\d{4}-\d{2}-\d{2})\.md$`)

var agentMemoryReadWhitelist = map[string]struct{}{
	"AGENTS.md":    {},
	"SOUL.md":      {},
	"TOOLS.md":     {},
	"IDENTITY.md":  {},
	"HEARTBEAT.md": {},
	"BOOTSTRAP.md": {},
}

func sanitizeMemoryID(id string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9_\-]`)
	return re.ReplaceAllString(id, "_")
}

func memoryUserWorkspace(dataDir, agentID, userID string) string {
	safeUserID := sanitizeMemoryID(userID)
	safeAgentID := sanitizeMemoryID(agentID)
	return filepath.Join(dataDir, "users", safeAgentID+"_user_"+safeUserID)
}

func memoryTargetUserID(args map[string]interface{}, role models.Role, currentUserID string, userIsolationEnabled bool) (string, error) {
	targetUserID, _ := args["user_id"].(string)
	targetUserID = strings.TrimSpace(targetUserID)
	currentUserID = strings.TrimSpace(currentUserID)
	if targetUserID == "" {
		targetUserID = currentUserID
	}
	if targetUserID == "" {
		return "", fmt.Errorf("user memory access requires user context or user_id")
	}
	if targetUserID != currentUserID && role != models.RoleAdmin {
		return "", fmt.Errorf("user_id override requires admin role")
	}
	return targetUserID, nil
}

func cleanMemoryRelPath(path string) (string, error) {
	cleanPath := filepath.Clean(strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"), "/"))
	if cleanPath == "." || strings.HasPrefix(cleanPath, "..") {
		return "", fmt.Errorf("invalid memory path: %s", path)
	}
	return filepath.ToSlash(cleanPath), nil
}

func isAllowedUserMemoryPath(relPath string) bool {
	return relPath == "USER.md" || relPath == "MEMORY.md" || userDailyMemoryPathRe.MatchString(relPath)
}

func isAllowedAgentMemoryReadPath(relPath string) bool {
	return isAllowedAgentMemoryPath(relPath)
}

func isAllowedAgentMemoryWritePath(relPath string) bool {
	return isAllowedAgentMemoryPath(relPath)
}

func isAllowedAgentMemoryPath(relPath string) bool {
	if strings.Contains(relPath, "/") {
		return false
	}
	_, ok := agentMemoryReadWhitelist[relPath]
	return ok
}

func memoryScopeForRel(relPath, baseScope string) string {
	if strings.HasPrefix(relPath, "memory/") {
		return baseScope + "_daily"
	}
	return baseScope
}
