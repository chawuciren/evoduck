package tools

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

// FileReadTool 读取文件内容
type FileReadTool struct {
	permissions AgentPermissions
	maxSize     int64 // 最大文件大小 (bytes)
}

func NewFileReadTool(permissions AgentPermissions) *FileReadTool {
	return &FileReadTool{
		permissions: permissions,
		maxSize:     10 * 1024 * 1024, // 10MB 默认限制
	}
}

func (t *FileReadTool) Name() string {
	return "file_read"
}

func (t *FileReadTool) Description() string {
	return `Read the contents of a file from the agent's workspace.

**Features:**
- Text files: Returns content with line numbers
- Binary files: Returns base64-encoded content
- Supports relative paths (relative to workspace)
- Auto-detects file encoding
- Supports 1-indexed line ranges and raw text output

**Security:**
- Only files within the workspace can be read
- Maximum file size: 10MB

**Parameters:**
- path: File path (relative to workspace or absolute if within workspace)
- start_line: 1-indexed start line number (optional)
- end_line: 1-indexed end line number, inclusive (optional)
- offset: Legacy 0-indexed start line number (optional)
- limit: Maximum number of lines to read (optional)
- line_numbers: Include line numbers in text output (optional, default true)`
}

func (t *FileReadTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "File path relative to workspace",
			},
			"offset": map[string]interface{}{
				"type":        "integer",
				"description": "Start line number (0-indexed, optional)",
			},
			"start_line": map[string]interface{}{
				"type":        "integer",
				"description": "Start line number (1-indexed, optional). Takes precedence over offset.",
			},
			"end_line": map[string]interface{}{
				"type":        "integer",
				"description": "End line number (1-indexed, inclusive, optional)",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum lines to read (optional)",
			},
			"line_numbers": map[string]interface{}{
				"type":        "boolean",
				"description": "Include line numbers in text output (default true)",
			},
		},
		"required": []string{"path"},
	}
}

func (t *FileReadTool) Execute(args map[string]interface{}) (string, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return "", fmt.Errorf("path is required")
	}

	// 解析路径
	fullPath, err := t.resolvePath(path)
	if err != nil {
		return "", err
	}

	// 安全检查
	if err := t.validatePath(fullPath); err != nil {
		return "", err
	}

	// 检查文件信息
	info, err := os.Stat(fullPath)
	if err != nil {
		return "", fmt.Errorf("stat file: %w", err)
	}

	// 检查是否是目录
	if info.IsDir() {
		return "", fmt.Errorf("path is a directory, use file_list instead")
	}

	// 检查文件大小
	if info.Size() > t.maxSize {
		return "", fmt.Errorf("file too large: %d bytes (max %d)", info.Size(), t.maxSize)
	}

	// 读取文件内容
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	// 检查是否是二进制文件
	if isBinary(content) {
		return t.formatBinary(content, fullPath)
	}

	// 解析分页参数。start_line/end_line 是 1-indexed；offset 是旧的 0-indexed 兼容参数。
	offset := parseIntArg(args["offset"], 0)
	limit := parseIntArg(args["limit"], 0)
	startLine := parseIntArg(args["start_line"], 0)
	endLine := parseIntArg(args["end_line"], 0)
	lineNumbers := true
	if raw, ok := args["line_numbers"].(bool); ok {
		lineNumbers = raw
	}

	// 格式化文本输出
	return t.formatText(content, offset, limit, startLine, endLine, lineNumbers, fullPath)
}

// resolvePath 解析路径（支持相对和绝对路径）
func (t *FileReadTool) resolvePath(path string) (string, error) {
	return t.permissions.ResolvePath(path)
}

// validatePath 验证路径安全性（防止目录遍历）
func (t *FileReadTool) validatePath(path string) error {
	return t.permissions.CanAccessPath(path)
}

// isBinary 检查是否是二进制文件
func isBinary(content []byte) bool {
	// 空文件不算二进制
	if len(content) == 0 {
		return false
	}

	// 检查前 512 字节
	checkLen := len(content)
	if checkLen > 512 {
		checkLen = 512
	}

	// 如果包含 NULL 字节，认为是二进制
	for i := 0; i < checkLen; i++ {
		if content[i] == 0 {
			return true
		}
	}

	// 检查非打印字符比例
	nonPrintable := 0
	for i := 0; i < checkLen; i++ {
		if content[i] < 32 && content[i] != '\n' && content[i] != '\r' && content[i] != '\t' {
			nonPrintable++
		}
	}

	// 如果非打印字符超过 30%，认为是二进制
	return float64(nonPrintable)/float64(checkLen) > 0.3
}

// formatText 格式化文本输出
func (t *FileReadTool) formatText(content []byte, offset, limit, startLine, endLine int, lineNumbers bool, path string) (string, error) {
	allLines := strings.Split(string(content), "\n")
	totalLines := len(allLines)
	if startLine < 0 || endLine < 0 || offset < 0 || limit < 0 {
		return "", fmt.Errorf("line range values cannot be negative")
	}

	startIndex := offset
	if startLine > 0 {
		startIndex = startLine - 1
	}
	if startIndex >= totalLines {
		return "", fmt.Errorf("start line %d exceeds file length %d", startIndex+1, totalLines)
	}

	endIndex := totalLines
	if startLine > 0 && endLine > 0 {
		if endLine < startLine {
			return "", fmt.Errorf("end_line %d cannot be before start_line %d", endLine, startLine)
		}
		endIndex = endLine
	} else if limit > 0 {
		endIndex = startIndex + limit
		if endIndex > totalLines {
			endIndex = totalLines
		}
	}
	if endIndex > totalLines {
		endIndex = totalLines
	}

	lines := allLines[startIndex:endIndex]
	if !lineNumbers {
		return strings.Join(lines, "\n"), nil
	}

	// 格式化输出（带行号）
	var output strings.Builder
	output.WriteString(fmt.Sprintf("→ %s\n\n", path))

	for i, line := range lines {
		lineNum := i + startIndex + 1
		output.WriteString(fmt.Sprintf("%4d→%s\n", lineNum, line))
	}

	output.WriteString(fmt.Sprintf("\n[%d lines total", totalLines))
	if startIndex > 0 || endIndex < totalLines {
		output.WriteString(fmt.Sprintf(", showing lines %d-%d", startIndex+1, startIndex+len(lines)))
	}
	output.WriteString("]")

	return output.String(), nil
}

// formatBinary 格式化二进制输出
func (t *FileReadTool) formatBinary(content []byte, path string) (string, error) {
	// 对于二进制文件，返回 base64 编码
	encoded := base64.StdEncoding.EncodeToString(content)

	var output strings.Builder
	output.WriteString(fmt.Sprintf("→ %s (binary, %d bytes)\n\n", path, len(content)))
	output.WriteString("```\n")
	output.WriteString(encoded)
	output.WriteString("\n```\n")
	output.WriteString(fmt.Sprintf("\n[base64 encoded, original size: %d bytes]", len(content)))

	return output.String(), nil
}

// SetWorkspace 设置工作目录
func (t *FileReadTool) SetWorkspace(workspace string) {
	t.permissions.Workspace = workspace
}
