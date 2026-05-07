package tools

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
)

type AgentPermissions struct {
	Role                 models.Role
	Workspace            string
	HasDirectoryOverride bool
	AllowedDirectories   []string
	HasToolOverride      bool
	AllowedTools         map[string]bool
}

func NewAgentPermissions(role models.Role, workspace string, cfg config.AgentPermissionConfig) AgentPermissions {
	perms := AgentPermissions{
		Role:      role,
		Workspace: workspace,
	}

	if len(cfg.AuthorizedDirectories) > 0 {
		perms.HasDirectoryOverride = true
		perms.AllowedDirectories = make([]string, 0, len(cfg.AuthorizedDirectories))
		for _, dir := range cfg.AuthorizedDirectories {
			dir = strings.TrimSpace(dir)
			if dir == "" {
				continue
			}
			perms.AllowedDirectories = append(perms.AllowedDirectories, dir)
		}
	}

	if len(cfg.AuthorizedTools) > 0 {
		perms.HasToolOverride = true
		perms.AllowedTools = make(map[string]bool, len(cfg.AuthorizedTools))
		for _, name := range cfg.AuthorizedTools {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			perms.AllowedTools[name] = true
		}
	}

	return perms
}

func (p AgentPermissions) CanUseTool(toolName string, defaultAllowed bool) bool {
	if p.HasToolOverride {
		if p.AllowedTools["*"] {
			return true
		}
		return p.AllowedTools[toolName]
	}
	return defaultAllowed
}

func (p AgentPermissions) CanAccessPath(path string) error {
	if path == "" {
		return fmt.Errorf("path is required")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("get absolute path: %w", err)
	}

	allowedRoots, unrestricted, err := p.allowedRoots()
	if err != nil {
		return err
	}
	if unrestricted {
		return nil
	}

	for _, root := range allowedRoots {
		if isWithinRoot(absPath, root) {
			return nil
		}
	}

	return fmt.Errorf("access denied: path outside authorized directories")
}

func (p AgentPermissions) ResolvePath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}

	if p.HasDirectoryOverride && len(p.AllowedDirectories) == 1 && p.AllowedDirectories[0] != "*" {
		root := strings.TrimSpace(p.AllowedDirectories[0])
		if root != "" {
			if filepath.IsAbs(root) {
				return filepath.Join(root, path), nil
			}
			if p.Workspace != "" {
				return filepath.Join(p.Workspace, root, path), nil
			}
			return filepath.Join(root, path), nil
		}
	}

	if p.Workspace != "" {
		return filepath.Join(p.Workspace, path), nil
	}

	return filepath.Clean(path), nil
}

func (p AgentPermissions) allowedRoots() ([]string, bool, error) {
	if p.HasDirectoryOverride {
		roots := make([]string, 0, len(p.AllowedDirectories))
		for _, dir := range p.AllowedDirectories {
			if dir == "*" {
				return nil, true, nil
			}
			resolved, err := p.ResolvePath(dir)
			if err != nil {
				return nil, false, err
			}
			absRoot, err := filepath.Abs(resolved)
			if err != nil {
				return nil, false, fmt.Errorf("get authorized directory absolute path: %w", err)
			}
			roots = append(roots, filepath.Clean(absRoot))
		}
		return roots, false, nil
	}

	if p.Role == models.RoleAdmin {
		return nil, true, nil
	}

	if strings.TrimSpace(p.Workspace) == "" {
		return nil, false, nil
	}

	absWorkspace, err := filepath.Abs(p.Workspace)
	if err != nil {
		return nil, false, fmt.Errorf("get workspace absolute path: %w", err)
	}
	return []string{filepath.Clean(absWorkspace)}, false, nil
}

func isWithinRoot(path string, root string) bool {
	relPath, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if relPath == "." {
		return true
	}
	return !strings.HasPrefix(relPath, "..")
}
