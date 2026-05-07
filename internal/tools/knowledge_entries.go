package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chawuciren/evoduck/internal/knowledge"
)

type KnowledgeTreeTool struct {
	dataDir string
}

func NewKnowledgeTreeTool(dataDir string) *KnowledgeTreeTool {
	return &KnowledgeTreeTool{dataDir: dataDir}
}

func (t *KnowledgeTreeTool) Name() string {
	return "knowledge_tree"
}

func (t *KnowledgeTreeTool) Description() string {
	return "Show the shared knowledge markdown file tree with frontmatter title and tags metadata. Use this to discover knowledge paths before knowledge_read or knowledge_search."
}

func (t *KnowledgeTreeTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Optional directory path under the shared knowledge root to list",
			},
		},
	}
}

func (t *KnowledgeTreeTool) Execute(args map[string]interface{}) (string, error) {
	root, err := knowledge.EnsureRoot(t.dataDir)
	if err != nil {
		return "", err
	}
	baseRel, _ := args["path"].(string)
	baseRel = strings.Trim(strings.ReplaceAll(baseRel, "\\", "/"), "/")
	base := root
	if baseRel != "" {
		base = filepath.Join(root, filepath.FromSlash(baseRel))
		if !isPathWithin(base, root) {
			return "", fmt.Errorf("invalid knowledge tree path: %s", baseRel)
		}
	}

	var entries []knowledge.Entry
	err = filepath.WalkDir(base, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() || strings.ToLower(filepath.Ext(d.Name())) != ".md" {
			return nil
		}
		entry, err := knowledge.ReadEntry(t.dataDir, knowledgeRelativePath(root, path))
		if err != nil {
			return nil
		}
		entries = append(entries, *entry)
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "No shared knowledge markdown files found.", nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })

	var b strings.Builder
	b.WriteString("Shared knowledge tree:\n\n")
	for _, entry := range entries {
		depth := strings.Count(entry.Path, "/")
		indent := strings.Repeat("  ", depth)
		b.WriteString(fmt.Sprintf("%s- %s", indent, entry.Path))
		if entry.Title != "" {
			b.WriteString(fmt.Sprintf(" | title=%q", entry.Title))
		}
		if len(entry.Tags) > 0 {
			b.WriteString(fmt.Sprintf(" | tags=%s", strings.Join(entry.Tags, ",")))
		}
		b.WriteString(fmt.Sprintf(" | full_path=%q\n", filepath.Join(root, filepath.FromSlash(entry.Path))))
	}
	return strings.TrimSpace(b.String()), nil
}

func knowledgeRelativePath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.Base(path)
	}
	return filepath.ToSlash(rel)
}

type KnowledgeReadTool struct {
	dataDir string
}

func NewKnowledgeReadTool(dataDir string) *KnowledgeReadTool {
	return &KnowledgeReadTool{dataDir: dataDir}
}

func (t *KnowledgeReadTool) Name() string {
	return "knowledge_read"
}

func (t *KnowledgeReadTool) Description() string {
	return "Read one shared knowledge entry with path, full_path, line range, line-numbered content, tags, and directory metadata. Use this after knowledge_tree or knowledge_search when you know the target path and need the actual shared content, not just a summary."
}

func (t *KnowledgeReadTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Knowledge entry path relative to the shared knowledge root, such as 'product/roadmap.md'",
			},
			"start_line": map[string]interface{}{
				"type":        "integer",
				"description": "1-indexed start line number (optional)",
			},
			"end_line": map[string]interface{}{
				"type":        "integer",
				"description": "1-indexed end line number, inclusive (optional)",
			},
		},
		"required": []string{"path"},
	}
}

func (t *KnowledgeReadTool) Execute(args map[string]interface{}) (string, error) {
	path, _ := args["path"].(string)
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("knowledge path is required")
	}
	entry, err := knowledge.ReadEntry(t.dataDir, path)
	if err != nil {
		return "", err
	}
	fullPath := filepath.Join(knowledge.RootDir(t.dataDir), filepath.FromSlash(entry.Path))
	startLine := parseIntArg(args["start_line"], 0)
	endLine := parseIntArg(args["end_line"], 0)
	content, start, end, total, err := readMarkdownLines(fullPath, startLine, endLine)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("# %s\n\n", entry.Title))
	b.WriteString(fmt.Sprintf("Path: %s\n", entry.Path))
	b.WriteString(fmt.Sprintf("Full path: %s\n", fullPath))
	b.WriteString(fmt.Sprintf("Lines: %d-%d of %d\n", start, end, total))
	if entry.Directory != "" {
		b.WriteString(fmt.Sprintf("Directory: %s\n", entry.Directory))
	}
	if len(entry.Tags) > 0 {
		b.WriteString(fmt.Sprintf("Tags: %s\n", strings.Join(entry.Tags, ", ")))
	}
	b.WriteString("\n")
	b.WriteString(content)
	return strings.TrimSpace(b.String()), nil
}

type KnowledgeSearchTool struct {
	dataDir string
}

func NewKnowledgeSearchTool(dataDir string) *KnowledgeSearchTool {
	return &KnowledgeSearchTool{dataDir: dataDir}
}

func (t *KnowledgeSearchTool) Name() string {
	return "knowledge_search"
}

func (t *KnowledgeSearchTool) Description() string {
	return "Search shared knowledge by title, path, directory, tags, or content and return matching summaries. Use this proactively when a task may depend on existing project docs, architecture decisions, operating notes, prior research, reusable checklists, or shared troubleshooting knowledge. Do not use this for remembered user facts; use memory_search for user-specific memory."
}

func (t *KnowledgeSearchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search text to match against knowledge titles, paths, tags, and content",
			},
		},
		"required": []string{"query"},
	}
}

func (t *KnowledgeSearchTool) Execute(args map[string]interface{}) (string, error) {
	query, _ := args["query"].(string)
	if strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("knowledge query is required")
	}
	entries, err := knowledge.SearchEntries(t.dataDir, query)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "No matching shared knowledge entries found.", nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d matching shared knowledge entries:\n\n", len(entries)))
	for _, entry := range entries {
		b.WriteString(fmt.Sprintf("- `%s`: %s\n", entry.Path, entry.Title))
		fullPath := filepath.Join(knowledge.RootDir(t.dataDir), filepath.FromSlash(entry.Path))
		b.WriteString(fmt.Sprintf("  Full path: %s\n", fullPath))
		if entry.Directory != "" {
			b.WriteString(fmt.Sprintf("  Directory: %s\n", entry.Directory))
		}
		if len(entry.Tags) > 0 {
			b.WriteString(fmt.Sprintf("  Tags: %s\n", strings.Join(entry.Tags, ", ")))
		}
		startLine, endLine, summary, truncated := findSnippetLines(entry.Content, query, 2)
		if summary == "" {
			summary = strings.TrimSpace(entry.Content)
			if len(summary) > 180 {
				summary = summary[:180]
				truncated = true
			}
		}
		if startLine > 0 && endLine > 0 {
			b.WriteString(fmt.Sprintf("  Lines: %d-%d\n", startLine, endLine))
		}
		b.WriteString(fmt.Sprintf("  Truncated: %t\n", truncated))
		if summary != "" {
			b.WriteString(fmt.Sprintf("  Preview: %s\n", summary))
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String()), nil
}
