package tools

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/chawuciren/evoduck/internal/knowledge"
)

type KnowledgeWriteTool struct {
	dataDir string
}

func NewKnowledgeWriteTool(dataDir string) *KnowledgeWriteTool {
	return &KnowledgeWriteTool{dataDir: dataDir}
}

func (t *KnowledgeWriteTool) Name() string {
	return "knowledge_write"
}

func (t *KnowledgeWriteTool) Description() string {
	return "Create or overwrite a shared knowledge entry.\n\n" +
		"Knowledge entries are stored in data/knowledge/ and support frontmatter (title, tags).\n\n" +
		"## Usage\n\n" +
		"{\"path\": \"product/roadmap.md\", \"content\": \"...\", \"title\": \"Product Roadmap\", \"tags\": [\"planning\"]}\n" +
		"{\"path\": \"tech/architecture.md\", \"content\": \"...\"}\n\n" +
		"## Features\n" +
		"- Creates parent directories if needed\n" +
		"- Adds YAML frontmatter with title and tags\n" +
		"- Content must be non-empty markdown\n\n" +
		"## Security\n" +
		"- Only writes to data/knowledge/ directory\n" +
		"- Cannot escape knowledge root"
}

func (t *KnowledgeWriteTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Knowledge entry path relative to knowledge root, e.g. product/roadmap.md",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Markdown content to write",
			},
			"title": map[string]interface{}{
				"type":        "string",
				"description": "Optional title for frontmatter",
			},
			"tags": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Optional tags for frontmatter",
			},
		},
		"required": []string{"path", "content"},
	}
}

func (t *KnowledgeWriteTool) Execute(args map[string]interface{}) (string, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	title, _ := args["title"].(string)
	tagsRaw, _ := args["tags"].([]interface{})

	path = strings.TrimSpace(path)
	content = strings.TrimSpace(content)

	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if content == "" {
		return "", fmt.Errorf("content is required")
	}

	tags := parseTagsFromInterface(tagsRaw)

	entry, err := knowledge.WriteEntry(t.dataDir, knowledge.WriteInput{
		Path:    path,
		Title:   title,
		Tags:    tags,
		Content: content,
	})
	if err != nil {
		return "", fmt.Errorf("write knowledge entry: %w", err)
	}

	fullPath := filepath.Join(knowledge.RootDir(t.dataDir), filepath.FromSlash(entry.Path))

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Successfully wrote knowledge entry\n"))
	b.WriteString(fmt.Sprintf("Path: %s\n", entry.Path))
	b.WriteString(fmt.Sprintf("Full path: %s\n", fullPath))
	if entry.Title != "" {
		b.WriteString(fmt.Sprintf("Title: %s\n", entry.Title))
	}
	if len(entry.Tags) > 0 {
		b.WriteString(fmt.Sprintf("Tags: %s\n", strings.Join(entry.Tags, ", ")))
	}

	return strings.TrimSpace(b.String()), nil
}

func parseTagsFromInterface(tagsRaw []interface{}) []string {
	if tagsRaw == nil {
		return nil
	}
	tags := make([]string, 0, len(tagsRaw))
	for _, t := range tagsRaw {
		if s, ok := t.(string); ok && strings.TrimSpace(s) != "" {
			tags = append(tags, strings.TrimSpace(s))
		}
	}
	return tags
}

func (t *KnowledgeWriteTool) SetDataDir(dataDir string) {
	t.dataDir = dataDir
}