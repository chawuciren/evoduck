package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func readMarkdownLines(fullPath string, startLine, endLine int) (string, int, int, int, error) {
	if strings.ToLower(filepath.Ext(fullPath)) != ".md" {
		return "", 0, 0, 0, fmt.Errorf("only markdown files can be read: %s", fullPath)
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", 0, 0, 0, fmt.Errorf("read markdown file: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	total := len(lines)
	if startLine < 0 || endLine < 0 {
		return "", 0, 0, 0, fmt.Errorf("line range values cannot be negative")
	}
	if startLine == 0 {
		startLine = 1
	}
	if endLine == 0 || endLine > total {
		endLine = total
	}
	if startLine > total {
		return "", 0, 0, 0, fmt.Errorf("start line %d exceeds file length %d", startLine, total)
	}
	if endLine < startLine {
		return "", 0, 0, 0, fmt.Errorf("end_line %d cannot be before start_line %d", endLine, startLine)
	}

	var b strings.Builder
	for i := startLine; i <= endLine; i++ {
		b.WriteString(fmt.Sprintf("%4d→%s", i, lines[i-1]))
		if i < endLine {
			b.WriteString("\n")
		}
	}
	return b.String(), startLine, endLine, total, nil
}

func isPathWithin(child, parent string) bool {
	childClean, err := filepath.Abs(filepath.Clean(child))
	if err != nil {
		return false
	}
	parentClean, err := filepath.Abs(filepath.Clean(parent))
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(parentClean, childClean)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func findSnippetLines(content, query string, context int) (int, int, string, bool) {
	lines := strings.Split(content, "\n")
	queryLower := strings.ToLower(query)
	match := -1
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), queryLower) {
			match = i
			break
		}
	}
	if match < 0 {
		return 0, 0, "", false
	}
	start := match - context
	if start < 0 {
		start = 0
	}
	end := match + context
	if end >= len(lines) {
		end = len(lines) - 1
	}
	snippet := strings.Join(lines[start:end+1], "\n")
	truncated := start > 0 || end < len(lines)-1
	return start + 1, end + 1, snippet, truncated
}
