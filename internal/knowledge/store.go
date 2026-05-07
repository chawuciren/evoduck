package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chawuciren/evoduck/pkg/textedit"
	"gopkg.in/yaml.v3"
)

type Entry struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Path      string    `json:"path"`
	Directory string    `json:"directory"`
	Tags      []string  `json:"tags"`
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updated_at"`
}

type WriteInput struct {
	Path    string
	Title   string
	Tags    []string
	Content string
}

type frontmatter struct {
	Title string   `yaml:"title"`
	Tags  []string `yaml:"tags"`
}

func RootDir(dataDir string) string {
	return filepath.Join(dataDir, "knowledge")
}

func EnsureRoot(dataDir string) (string, error) {
	root := RootDir(dataDir)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create knowledge root: %w", err)
	}
	return root, nil
}

func ListEntries(dataDir, query string) ([]Entry, error) {
	root, err := EnsureRoot(dataDir)
	if err != nil {
		return nil, err
	}

	entries := make([]Entry, 0)
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(d.Name())) != ".md" {
			return nil
		}

		entry, err := ReadEntry(dataDir, relativePathFromRoot(root, path))
		if err != nil {
			return nil
		}
		if matchesQuery(*entry, query) {
			entries = append(entries, *entry)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Directory == entries[j].Directory {
			return entries[i].Path < entries[j].Path
		}
		return entries[i].Directory < entries[j].Directory
	})
	return entries, nil
}

func ReadEntry(dataDir, relPath string) (*Entry, error) {
	root, err := EnsureRoot(dataDir)
	if err != nil {
		return nil, err
	}

	normalized, err := normalizeEntryPath(relPath)
	if err != nil {
		return nil, err
	}
	absPath := filepath.Join(root, filepath.FromSlash(normalized))
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read knowledge entry: %w", err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("stat knowledge entry: %w", err)
	}

	fm, body := parseContent(string(data))
	title := strings.TrimSpace(fm.Title)
	if title == "" {
		title = titleFromPath(normalized)
	}

	directory := filepath.ToSlash(filepath.Dir(normalized))
	if directory == "." {
		directory = ""
	}

	return &Entry{
		ID:        entryID(normalized),
		Title:     title,
		Path:      normalized,
		Directory: directory,
		Tags:      append([]string(nil), fm.Tags...),
		Content:   body,
		UpdatedAt: info.ModTime(),
	}, nil
}

func WriteEntry(dataDir string, input WriteInput) (*Entry, error) {
	root, err := EnsureRoot(dataDir)
	if err != nil {
		return nil, err
	}

	normalized, err := normalizeEntryPath(input.Path)
	if err != nil {
		return nil, err
	}
	absPath := filepath.Join(root, filepath.FromSlash(normalized))

	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = titleFromPath(normalized)
	}
	body := strings.TrimSpace(input.Content)
	if body == "" {
		return nil, fmt.Errorf("knowledge content is required")
	}

	payload, err := marshalContent(frontmatter{Title: title, Tags: cleanTags(input.Tags)}, body)
	if err != nil {
		return nil, err
	}

	edit := textedit.Edit{Operation: textedit.OpWrite, Content: payload, CreateIfMissing: true}
	if _, err := textedit.ApplyFile(edit, absPath, 0644); err != nil {
		return nil, fmt.Errorf("write knowledge entry: %w", err)
	}

	return ReadEntry(dataDir, normalized)
}

func DeleteEntry(dataDir, relPath string) error {
	root, err := EnsureRoot(dataDir)
	if err != nil {
		return err
	}

	normalized, err := normalizeEntryPath(relPath)
	if err != nil {
		return err
	}
	absPath := filepath.Join(root, filepath.FromSlash(normalized))
	if err := os.Remove(absPath); err != nil {
		return fmt.Errorf("delete knowledge entry: %w", err)
	}
	cleanupEmptyParents(filepath.Dir(absPath), root)
	return nil
}

func MoveEntry(dataDir, fromPath, toPath string) (*Entry, error) {
	root, err := EnsureRoot(dataDir)
	if err != nil {
		return nil, err
	}

	fromNormalized, err := normalizeEntryPath(fromPath)
	if err != nil {
		return nil, err
	}
	toNormalized, err := normalizeEntryPath(toPath)
	if err != nil {
		return nil, err
	}
	if fromNormalized == toNormalized {
		return ReadEntry(dataDir, fromNormalized)
	}

	fromAbs := filepath.Join(root, filepath.FromSlash(fromNormalized))
	toAbs := filepath.Join(root, filepath.FromSlash(toNormalized))
	if err := os.MkdirAll(filepath.Dir(toAbs), 0o755); err != nil {
		return nil, fmt.Errorf("create target knowledge directory: %w", err)
	}
	if err := os.Rename(fromAbs, toAbs); err != nil {
		return nil, fmt.Errorf("move knowledge entry: %w", err)
	}
	cleanupEmptyParents(filepath.Dir(fromAbs), root)
	return ReadEntry(dataDir, toNormalized)
}

func CreateDirectory(dataDir, dirPath string) (string, error) {
	root, err := EnsureRoot(dataDir)
	if err != nil {
		return "", err
	}

	normalized, err := normalizeDirectoryPath(dirPath)
	if err != nil {
		return "", err
	}
	absPath := filepath.Join(root, filepath.FromSlash(normalized))
	if err := os.MkdirAll(absPath, 0o755); err != nil {
		return "", fmt.Errorf("create knowledge directory: %w", err)
	}
	return normalized, nil
}

func DeleteDirectory(dataDir, dirPath string) (string, error) {
	root, err := EnsureRoot(dataDir)
	if err != nil {
		return "", err
	}

	normalized, err := normalizeDirectoryPath(dirPath)
	if err != nil {
		return "", err
	}
	absPath := filepath.Join(root, filepath.FromSlash(normalized))
	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("stat knowledge directory: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("knowledge path is not a directory: %s", normalized)
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return "", fmt.Errorf("read knowledge directory: %w", err)
	}
	if len(entries) > 0 {
		return "", fmt.Errorf("knowledge directory is not empty: %s", normalized)
	}
	if err := os.Remove(absPath); err != nil {
		return "", fmt.Errorf("delete knowledge directory: %w", err)
	}
	cleanupEmptyParents(filepath.Dir(absPath), root)
	return normalized, nil
}

func ListDirectories(dataDir string) ([]string, error) {
	root, err := EnsureRoot(dataDir)
	if err != nil {
		return nil, err
	}

	var directories []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if filepath.Clean(path) == filepath.Clean(root) {
			return nil
		}
		directories = append(directories, relativePathFromRoot(root, path))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(directories)
	return directories, nil
}

func SearchEntries(dataDir, query string) ([]Entry, error) {
	return ListEntries(dataDir, query)
}

func NormalizeEntryPath(relPath string) (string, error) {
	return normalizeEntryPath(relPath)
}

func normalizeEntryPath(relPath string) (string, error) {
	path := strings.TrimSpace(relPath)
	path = strings.ReplaceAll(path, "\\", "/")
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("knowledge path is required")
	}
	if !strings.HasSuffix(strings.ToLower(path), ".md") {
		path += ".md"
	}
	cleaned := filepath.ToSlash(filepath.Clean(path))
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		return "", fmt.Errorf("invalid knowledge path: %s", relPath)
	}
	return cleaned, nil
}

func normalizeDirectoryPath(relPath string) (string, error) {
	path := strings.TrimSpace(relPath)
	path = strings.ReplaceAll(path, "\\", "/")
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, "/")
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("knowledge directory is required")
	}
	cleaned := filepath.ToSlash(filepath.Clean(path))
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		return "", fmt.Errorf("invalid knowledge directory: %s", relPath)
	}
	return cleaned, nil
}

func parseContent(raw string) (frontmatter, string) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	parts := strings.SplitN(raw, "---", 3)
	if len(parts) >= 3 && strings.TrimSpace(parts[0]) == "" {
		var fm frontmatter
		if err := yaml.Unmarshal([]byte(parts[1]), &fm); err == nil {
			return fm, strings.TrimSpace(parts[2])
		}
	}
	return frontmatter{}, strings.TrimSpace(raw)
}

func marshalContent(fm frontmatter, body string) (string, error) {
	data, err := yaml.Marshal(fm)
	if err != nil {
		return "", fmt.Errorf("marshal knowledge frontmatter: %w", err)
	}
	return fmt.Sprintf("---\n%s---\n\n%s\n", string(data), strings.TrimSpace(body)), nil
}

func cleanTags(tags []string) []string {
	seen := make(map[string]struct{})
	cleaned := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		cleaned = append(cleaned, tag)
	}
	sort.Strings(cleaned)
	return cleaned
}

func relativePathFromRoot(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.Base(path)
	}
	return filepath.ToSlash(rel)
}

func titleFromPath(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	return strings.TrimSpace(base)
}

func entryID(path string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_")
	return replacer.Replace(path)
}

func matchesQuery(entry Entry, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	if strings.Contains(strings.ToLower(entry.Title), query) ||
		strings.Contains(strings.ToLower(entry.Path), query) ||
		strings.Contains(strings.ToLower(entry.Directory), query) ||
		strings.Contains(strings.ToLower(entry.Content), query) {
		return true
	}
	for _, tag := range entry.Tags {
		if strings.Contains(strings.ToLower(tag), query) {
			return true
		}
	}
	return false
}

func cleanupEmptyParents(dir, stop string) {
	stop = filepath.Clean(stop)
	for {
		dir = filepath.Clean(dir)
		if dir == stop || dir == "." || dir == string(filepath.Separator) {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}
