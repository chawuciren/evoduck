package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/chawuciren/evoduck/pkg/models"
)

// MemorySearchTool 记忆搜索工具
// 支持两层记忆系统（Longterm/Medium）的统一搜索
type MemorySearchTool struct {
	workspace string
	agentID   string
	dataDir   string
}

func NewMemorySearchTool(workspace string, agentID string, dataDir string) *MemorySearchTool {
	return &MemorySearchTool{
		workspace: workspace,
		agentID:   agentID,
		dataDir:   dataDir,
	}
}

func (t *MemorySearchTool) Name() string {
	return "memory_search"
}

func (t *MemorySearchTool) Description() string {
	return `Search through your two-layer memory system.

## Memory Layers

### Layer 1: Longterm Memory (MEMORY.md)
- **What**: Permanent, always-loaded memories
- **Content**: User-specific preferences, constraints, important user-specific decisions, durable facts
- **Storage**: 
	  - User-level: data/users/{agentID}_user_{userID}/MEMORY.md
- **Search**: Keyword matching

### Layer 2: Medium Memory (Daily Logs)
- **What**: Daily activity logs (memory/YYYY-MM-DD.md)
- **Content**: Session summaries, temporary context, recent activities
- **Storage**: memory/YYYY-MM-DD.md (user-level)
- **Search**: Keyword matching with time filtering

## Usage Examples

**Search all layers:**
` + "```json" + `
{"query": "database preferences"}
` + "```" + `

**Search only medium memory:**
` + "```json" + `
{"query": "meeting", "layers": ["medium"], "days": 3}
` + "```" + `

**Get memory statistics:**
` + "```json" + `
{"query": "", "stats_only": true}
` + "```" + `

## Memory Layers Explained

| Layer | Storage | Purpose |
|-------|---------|---------|
| **Longterm** | MEMORY.md files | Permanent memories that persist indefinitely |
| **Medium** | memory/YYYY-MM-DD.md | Daily logs for temporary context |

## vs knowledge_search

- **memory_search**: Searches user-specific remembered facts stored in Markdown memory files
- **knowledge_search**: Searches shared knowledge entries under the shared knowledge base

Use memory_search when you need to recall something about the current user.
Use knowledge_search when you need shared project or domain knowledge.`
}

func (t *MemorySearchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search query - what to look for (empty string for stats only)",
			},
			"user_id": map[string]interface{}{
				"type":        "string",
				"description": "Target user ID for user memory. Admin/system curator only; defaults to current user context.",
			},
			"layers": map[string]interface{}{
				"type":        "array",
				"description": "Memory layers to search: longterm, medium (default: all layers)",
				"items": map[string]interface{}{
					"type": "string",
					"enum": []string{"longterm", "medium"},
				},
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum results per layer (default: 5)",
			},
			"days": map[string]interface{}{
				"type":        "integer",
				"description": "Days to search in medium layer (default: 7)",
			},
			"min_score": map[string]interface{}{
				"type":        "number",
				"description": "Minimum similarity score for longterm layer 0.0-1.0 (default: 0.7)",
			},
			"stats_only": map[string]interface{}{
				"type":        "boolean",
				"description": "Only return statistics, no content (default: false)",
			},
		},
		"required": []string{}, // query is optional when stats_only=true
	}
}

func (t *MemorySearchTool) Execute(args map[string]interface{}) (string, error) {
	query, _ := args["query"].(string)

	// 解析参数
	days := parseIntArg(args["days"], 7)
	statsOnly, _ := args["stats_only"].(bool)

	// 只返回统计
	if statsOnly {
		return t.getStatistics(days), nil
	}

	// 空查询返回提示
	if query == "" {
		return "Empty query. Use stats_only=true to get memory statistics.", nil
	}

	return "User memory search requires user context.", nil
}

// ExecuteWithUserContext 带用户上下文的执行（支持用户隔离）
func (t *MemorySearchTool) ExecuteWithUserContext(ctx context.Context, args map[string]interface{}, role models.Role, userID string, userIsolationEnabled bool, workspace string) (string, error) {
	query, _ := args["query"].(string)

	// 解析参数
	limit := parseIntArg(args["limit"], 5)
	days := parseIntArg(args["days"], 7)
	statsOnly, _ := args["stats_only"].(bool)

	// 解析 layers，默认搜索所有层
	layers := parseLayers(args["layers"])

	// 只返回统计
	if statsOnly {
		return t.getStatisticsWithUser(days, userID, userIsolationEnabled), nil
	}

	// 空查询返回提示
	if query == "" {
		return "Empty query. Use stats_only=true to get memory statistics.", nil
	}

	var allResults []MemorySearchResult

	if !userIsolationEnabled {
		return "User memory search requires user isolation.", nil
	}
	targetUserID, err := memoryTargetUserID(args, role, userID, userIsolationEnabled)
	if err != nil {
		return "", err
	}
	userDir := t.getUserWorkspace(targetUserID)

	// 按层级搜索
	for _, layer := range layers {
		switch layer {
		case "longterm":
			// 搜索用户级 MEMORY.md
			results := t.searchLongtermMemory(query, limit, userDir)
			allResults = append(allResults, results...)
		case "medium":
			// 搜索用户级 daily memory
			results := t.searchMediumMemory(query, limit, days, userDir)
			allResults = append(allResults, results...)
		}
	}

	if len(allResults) == 0 {
		return fmt.Sprintf("No matching memories found for: %s\n\nTip: Use stats_only=true to see what memories you have.", query), nil
	}

	return t.formatResults(allResults, query), nil
}

// getUserWorkspace 获取用户工作空间路径
// 新路径格式: data/users/{agentID}_user_{userID}
func (t *MemorySearchTool) getUserWorkspace(userID string) string {
	safeUserID := sanitizeUserIDForSearch(userID)
	safeAgentID := sanitizeUserIDForSearch(t.agentID)
	return filepath.Join(t.dataDir, "users", safeAgentID+"_user_"+safeUserID)
}

// sanitizeUserIDForSearch 清理用户 ID
func sanitizeUserIDForSearch(id string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9_\-]`)
	return re.ReplaceAllString(id, "_")
}

// MemorySearchResult 搜索结果
type MemorySearchResult struct {
	Layer       string    // longterm/medium
	Type        string    // 类型
	Content     string    // 内容
	Score       float64   // 相似度/重要性
	Source      string    // 来源文件或 ID
	Path        string    // 可读相对路径
	FullPath    string    // 绝对路径
	StartLine   int       // 1-indexed 起始行
	EndLine     int       // 1-indexed 结束行
	Truncated   bool      // 片段是否截断
	CreatedAt   time.Time // 创建时间
	AccessCount int       // 访问次数
}

// parseLayers 解析 layers 参数
func parseLayers(layersRaw interface{}) []string {
	if layersRaw == nil {
		return []string{"longterm", "medium"}
	}

	layers, ok := layersRaw.([]interface{})
	if !ok {
		return []string{"longterm", "medium"}
	}

	result := make([]string, 0, len(layers))
	for _, l := range layers {
		if s, ok := l.(string); ok {
			result = append(result, s)
		}
	}

	if len(result) == 0 {
		return []string{"longterm", "medium"}
	}

	return result
}

// searchLongtermMemory 搜索 Longterm Memory (MEMORY.md)
// userDir: 用户目录路径（启用用户隔离时）
func (t *MemorySearchTool) searchLongtermMemory(query string, limit int, userDir string) []MemorySearchResult {
	var results []MemorySearchResult
	results = append(results, t.searchLongtermFile(query, limit, userDir, "MEMORY.md", "MEMORY.md", "longterm", "longterm")...)
	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

func (t *MemorySearchTool) searchLongtermFile(query string, limit int, userDir string, filename string, source string, layer string, resultType string) []MemorySearchResult {
	var filePath string
	if userDir == "" {
		return nil
	}
	filePath = filepath.Join(userDir, filename)

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}

	contentStr := string(content)
	queryLower := strings.ToLower(query)

	// 关键词匹配
	if !strings.Contains(strings.ToLower(contentStr), queryLower) {
		return nil
	}

	startLine, endLine, snippet, truncated := findSnippetLines(contentStr, query, 2)
	if startLine == 0 {
		return nil
	}
	var results []MemorySearchResult
	displayPath := source
	if userDir != "" {
		displayPath = filename
	}
	results = append(results, MemorySearchResult{
		Layer:     layer,
		Type:      resultType,
		Content:   snippet,
		Source:    displayPath,
		Path:      displayPath,
		FullPath:  filePath,
		StartLine: startLine,
		EndLine:   endLine,
		Truncated: truncated,
		Score:     1.0,
	})
	return results
}

// searchMediumMemory 搜索 Medium Memory (daily logs)
// userDir: 用户目录路径（启用用户隔离时）
func (t *MemorySearchTool) searchMediumMemory(query string, limit int, days int, userDir string) []MemorySearchResult {
	var memDir string
	if userDir == "" {
		return nil
	}
	memDir = filepath.Join(userDir, "memory")
	var results []MemorySearchResult
	queryLower := strings.ToLower(query)

	for i := 0; i < days; i++ {
		date := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		path := filepath.Join(memDir, date+".md")

		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		contentStr := string(content)
		if !strings.Contains(strings.ToLower(contentStr), queryLower) {
			continue
		}

		startLine, endLine, snippet, truncated := findSnippetLines(contentStr, query, 2)
		if startLine > 0 {
			displayPath := "memory/" + date + ".md"
			results = append(results, MemorySearchResult{
				Layer:     "medium",
				Type:      "daily",
				Content:   snippet,
				Source:    displayPath,
				Path:      displayPath,
				FullPath:  path,
				StartLine: startLine,
				EndLine:   endLine,
				Truncated: truncated,
			})
		}

		// 限制总数
		if len(results) >= limit {
			break
		}
	}

	return results
}

// extractMatchingSections 提取匹配的段落
func (t *MemorySearchTool) extractMatchingSections(content, query string, maxSections int) []string {
	lines := strings.Split(content, "\n")
	queryLower := strings.ToLower(query)

	var sections []string
	var currentSection strings.Builder
	inMatch := false
	matchCount := 0

	for _, line := range lines {
		currentSection.WriteString(line + "\n")

		// 检测匹配
		if strings.Contains(strings.ToLower(line), queryLower) {
			inMatch = true
		}

		// 段落结束（空行或标题）
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "---") {
			if inMatch && currentSection.Len() > 0 {
				section := strings.TrimSpace(currentSection.String())
				if section != "" {
					if len(section) <= 50 {
						section = t.extractMatchingLine(section, query)
					}
					sections = append(sections, section)
					matchCount++
					if matchCount >= maxSections {
						break
					}
				}
			}
			currentSection.Reset()
			inMatch = false
		}
	}

	// 处理最后一个段落
	if inMatch && currentSection.Len() > 0 && matchCount < maxSections {
		section := strings.TrimSpace(currentSection.String())
		if section != "" {
			if len(section) <= 50 {
				section = t.extractMatchingLine(section, query)
			}
			sections = append(sections, section)
		}
	}

	return sections
}

func (t *MemorySearchTool) extractMatchingLine(section, query string) string {
	queryLower := strings.ToLower(query)
	for _, line := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.Contains(strings.ToLower(trimmed), queryLower) {
			return trimmed
		}
	}
	return strings.TrimSpace(section)
}

// getStatistics 获取记忆统计信息
func (t *MemorySearchTool) getStatistics(days int) string {
	var output strings.Builder
	output.WriteString("# Memory Statistics\n\n")

	output.WriteString("User memory requires user context. No agent-level memory files exist.\n\n")

	return output.String()
}

// getStatisticsWithUser 获取记忆统计信息（支持用户隔离）
func (t *MemorySearchTool) getStatisticsWithUser(days int, userID string, userIsolationEnabled bool) string {
	var output strings.Builder
	output.WriteString("# Memory Statistics\n\n")

	// 确定要统计的目录
	var userDir string
	if userIsolationEnabled && userID != "" {
		userDir = t.getUserWorkspace(userID)
		output.WriteString(fmt.Sprintf("**User**: %s\n\n", userID))
	}

	// Longterm Memory 统计
	output.WriteString("## Layer 1: Longterm Memory (MEMORY.md)\n\n")

	// 用户级
	if userDir != "" {
		userCorePath := filepath.Join(userDir, "MEMORY.md")
		if _, err := os.Stat(userCorePath); err == nil {
			content, _ := os.ReadFile(userCorePath)
			output.WriteString(fmt.Sprintf("- **User Level**: %d chars\n", len(content)))
		} else {
			output.WriteString("- **User Level**: Not found\n")
		}
	} else {
		output.WriteString("- **User Level**: No user context\n")
	}
	output.WriteString("\n")

	// Medium Memory 统计
	output.WriteString("## Layer 2: Medium Memory (Daily Logs)\n\n")

	// 用户级
	if userDir != "" {
		memDir := filepath.Join(userDir, "memory")
		if entries, err := os.ReadDir(memDir); err == nil {
			var totalSize int64
			var fileCount int
			for _, entry := range entries {
				if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
					if info, err := entry.Info(); err == nil {
						totalSize += info.Size()
						fileCount++
					}
				}
			}
			output.WriteString(fmt.Sprintf("- **User Logs**: %d files, %d chars\n", fileCount, totalSize))
		}
	}

	return output.String()
}

// formatResults 格式化搜索结果
func (t *MemorySearchTool) formatResults(results []MemorySearchResult, query string) string {
	var output strings.Builder
	output.WriteString(fmt.Sprintf("# Search Results for \"%s\"\n\n", query))
	output.WriteString(fmt.Sprintf("Found %d matching memories\n\n", len(results)))

	// 按层级分组显示
	layerOrder := []string{"longterm", "medium"}
	layerNames := map[string]string{
		"longterm": "Longterm Memory",
		"medium":   "Medium Memory",
	}

	for _, layer := range layerOrder {
		var layerResults []MemorySearchResult
		for _, r := range results {
			if r.Layer == layer {
				layerResults = append(layerResults, r)
			}
		}

		if len(layerResults) == 0 {
			continue
		}

		output.WriteString(fmt.Sprintf("## %s (%d results)\n\n", layerNames[layer], len(layerResults)))

		for i, r := range layerResults {
			output.WriteString(fmt.Sprintf("### %d. [%s]\n", i+1, r.Type))

			if r.Score > 0 {
				output.WriteString(fmt.Sprintf("**Score**: %.2f\n", r.Score))
			}
			if r.Source != "" {
				output.WriteString(fmt.Sprintf("**Source**: %s\n", r.Source))
			}
			if r.Path != "" {
				output.WriteString(fmt.Sprintf("**Path**: %s\n", r.Path))
			}
			if r.FullPath != "" {
				output.WriteString(fmt.Sprintf("**Full path**: %s\n", r.FullPath))
			}
			if r.StartLine > 0 && r.EndLine > 0 {
				output.WriteString(fmt.Sprintf("**Lines**: %d-%d\n", r.StartLine, r.EndLine))
			}
			if r.Truncated {
				output.WriteString("**Truncated**: true\n")
			}
			if !r.CreatedAt.IsZero() {
				output.WriteString(fmt.Sprintf("**Created**: %s\n", r.CreatedAt.Format("2006-01-02")))
			}
			if r.AccessCount > 0 {
				output.WriteString(fmt.Sprintf("**Accessed**: %d times\n", r.AccessCount))
			}

			output.WriteString(fmt.Sprintf("\n%s\n", t.truncateContent(r.Content, 400)))
			output.WriteString("\n---\n\n")
		}
	}

	return output.String()
}

// truncateContent 截断内容
func (t *MemorySearchTool) truncateContent(content string, maxLen int) string {
	content = strings.TrimSpace(content)
	if len(content) <= maxLen {
		return content
	}
	return content[:maxLen] + "\n... (truncated)"
}

// parseIntArg 解析整数参数
func parseIntArg(arg interface{}, defaultValue int) int {
	switch v := arg.(type) {
	case int:
		return v
	case float64:
		return int(v)
	case int64:
		return int(v)
	default:
		return defaultValue
	}
}

// parseFloatArg 解析浮点参数
func parseFloatArg(arg interface{}, defaultValue float64) float64 {
	switch v := arg.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return defaultValue
	}
}

// SetWorkspace 设置工作目录
func (t *MemorySearchTool) SetWorkspace(workspace string) {
	t.workspace = workspace
}
