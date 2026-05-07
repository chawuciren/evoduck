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

	count := 0
	err = t.listDir(fullPath, "", recursive, ignorePatterns, &output, &count)
	if err != nil {
		return "", err
	}

	output.WriteString(fmt.Sprintf("\n[%d items]", count))

	return output.String(), nil
}

// listDir 递归列出目录
func (t *FileListTool) listDir(dir, prefix string, recursive bool, ignore []string, output *strings.Builder, count *int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read directory: %w", err)
	}

	// 排序：目录在前，然后按名称排序
	var dirs, files []os.DirEntry
	for _, entry := range entries {
		// 检查是否忽略
		if t.shouldIgnore(entry.Name(), ignore) {
			continue
		}

		if entry.IsDir() {
			dirs = append(dirs, entry)
		} else {
			files = append(files, entry)
		}
	}

	// 合并
	entries = append(dirs, files...)

	for i, entry := range entries {
		*count++
		if *count > t.maxFiles {
			output.WriteString(fmt.Sprintf("\n... (truncated, max %d files)", t.maxFiles))
			return nil
		}

		// 判断是否是最后一个
		isLast := i == len(entries)-1

		// 绘制树形结构
		connector := "├── "
		if isLast {
			connector = "└── "
		}

		// 文件/目录标记
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}

		output.WriteString(fmt.Sprintf("%s%s%s\n", prefix, connector, name))

		// 递归处理子目录
		if recursive && entry.IsDir() {
			newPrefix := prefix + "│   "
			if isLast {
				newPrefix = prefix + "    "
			}

			subDir := filepath.Join(dir, entry.Name())
			if err := t.listDir(subDir, newPrefix, recursive, ignore, output, count); err != nil {
				// 忽略权限错误等
				output.WriteString(fmt.Sprintf("%s[error: %v]\n", newPrefix, err))
			}
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
