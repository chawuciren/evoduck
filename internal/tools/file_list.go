package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileListTool 列出目录内容
type FileListTool struct {
	permissions AgentPermissions
	maxFiles    int
}

func NewFileListTool(permissions AgentPermissions) *FileListTool {
	return &FileListTool{
		permissions: permissions,
		maxFiles:    1000,
	}
}

func (t *FileListTool) Name() string {
	return "file_list"
}

func (t *FileListTool) Description() string {
	return `List files and directories in a path.

**Features:**
- Tree-style display with indentation
- File/directory indicators
- Sorted by name (directories first)
- Respects .gitignore patterns

**Parameters:**
- path: Directory path (optional, default: workspace root)
- recursive: List recursively (optional, default: false)
- offset: Start index within the current directory level (0-indexed, optional)
- limit: Maximum entries to show from the current directory level (optional)
- ignore: Patterns to ignore (optional, e.g., ["node_modules", "*.log"])`
}

func (t *FileListTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Directory path relative to workspace (default: root)",
			},
			"recursive": map[string]interface{}{
				"type":        "boolean",
				"description": "List recursively (default: false)",
			},
			"offset": map[string]interface{}{
				"type":        "integer",
				"description": "Start index within the current directory level (0-indexed, optional)",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum entries to show from the current directory level (optional)",
			},
			"ignore": map[string]interface{}{
				"type":        "array",
				"description": "Patterns to ignore (e.g., ['node_modules', '*.log'])",
				"items": map[string]interface{}{
					"type": "string",
				},
			},
		},
		"required": []string{},
	}
}

func (t *FileListTool) Execute(args map[string]interface{}) (string, error) {
	// 解析路径
	path := "."
	if p, ok := args["path"].(string); ok && p != "" {
		path = p
	}

	recursive := false
	if r, ok := args["recursive"].(bool); ok {
		recursive = r
	}
	offset := parseIntArg(args["offset"], 0)
	limit := parseIntArg(args["limit"], 0)
	if offset < 0 || limit < 0 {
		return "", fmt.Errorf("offset and limit cannot be negative")
	}

	// 解析忽略模式
	var ignorePatterns []string
	if ignore, ok := args["ignore"].([]interface{}); ok {
		for _, i := range ignore {
			if s, ok := i.(string); ok {
				ignorePatterns = append(ignorePatterns, s)
			}
		}
	}

	// 解析完整路径
	fullPath, err := t.resolvePath(path)
	if err != nil {
		return "", err
	}

	// 安全检查
	if err := t.validatePath(fullPath); err != nil {
		return "", err
	}

	// 检查是否是目录
	info, err := os.Stat(fullPath)
	if err != nil {
		return "", fmt.Errorf("stat path: %w", err)
	}

	if !info.IsDir() {
		return "", fmt.Errorf("path is not a directory: %s", path)
	}

	// 列出目录
	var output strings.Builder
	output.WriteString(fmt.Sprintf("→ %s/\n\n", path))

	entries, err := t.readEntries(fullPath, ignorePatterns)
	if err != nil {
		return "", err
	}
	if offset > len(entries) {
		return "", fmt.Errorf("offset %d exceeds item count %d", offset, len(entries))
	}

	pageEnd := len(entries)
	if limit > 0 && offset+limit < pageEnd {
		pageEnd = offset + limit
	}
	pageEntries := entries[offset:pageEnd]
	pageCount := len(pageEntries)
	for i, entry := range pageEntries {
		isLast := i == pageCount-1
		if err := t.writeEntryTree(fullPath, "", entry, recursive, ignorePatterns, isLast, &output, 1, t.maxFiles); err != nil {
			output.WriteString(fmt.Sprintf("[error: %v]\n", err))
		}
	}

	hasMore := pageEnd < len(entries)
	output.WriteString(fmt.Sprintf("\n[total=%d, offset=%d, limit=%d, returned=%d, has_more=%t]", len(entries), offset, limit, pageCount, hasMore))

	return output.String(), nil
}

func (t *FileListTool) readEntries(dir string, ignore []string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	var dirs, files []os.DirEntry
	for _, entry := range entries {
		if t.shouldIgnore(entry.Name(), ignore) {
			continue
		}
		if entry.IsDir() {
			dirs = append(dirs, entry)
		} else {
			files = append(files, entry)
		}
	}

	return append(dirs, files...), nil
}

func (t *FileListTool) writeEntryTree(rootDir, prefix string, entry os.DirEntry, recursive bool, ignore []string, isLast bool, output *strings.Builder, count, maxFiles int) error {
	connector := "├── "
	if isLast {
		connector = "└── "
	}

	name := entry.Name()
	if entry.IsDir() {
		name += "/"
	}
	output.WriteString(fmt.Sprintf("%s%s%s\n", prefix, connector, name))
	if count >= maxFiles {
		output.WriteString(fmt.Sprintf("\n... (truncated, max %d files)", maxFiles))
		return nil
	}
	if !recursive || !entry.IsDir() {
		return nil
	}

	newPrefix := prefix + "│   "
	if isLast {
		newPrefix = prefix + "    "
	}
	subDir := filepath.Join(rootDir, entry.Name())
	entries, err := t.readEntries(subDir, ignore)
	if err != nil {
		return err
	}
	for i, child := range entries {
		childLast := i == len(entries)-1
		if err := t.writeEntryTree(subDir, newPrefix, child, recursive, ignore, childLast, output, count+1, maxFiles); err != nil {
			output.WriteString(fmt.Sprintf("%s[error: %v]\n", newPrefix, err))
		}
	}
	return nil
}

// shouldIgnore 检查是否应该忽略
func (t *FileListTool) shouldIgnore(name string, patterns []string) bool {
	for _, pattern := range patterns {
		// 简单匹配：精确匹配或通配符
		if name == pattern {
			return true
		}
		// 后缀匹配
		if strings.HasPrefix(pattern, "*") && strings.HasSuffix(name, pattern[1:]) {
			return true
		}
	}
	return false
}

// resolvePath 解析路径
func (t *FileListTool) resolvePath(path string) (string, error) {
	return t.permissions.ResolvePath(path)
}

// validatePath 验证路径安全性
func (t *FileListTool) validatePath(path string) error {
	return t.permissions.CanAccessPath(path)
}

// SetWorkspace 设置工作目录
func (t *FileListTool) SetWorkspace(workspace string) {
	t.permissions.Workspace = workspace
}
