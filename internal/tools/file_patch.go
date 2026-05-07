package tools

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

type FilePatchTool struct {
	permissions AgentPermissions
}

func NewFilePatchTool(permissions AgentPermissions) *FilePatchTool {
	return &FilePatchTool{
		permissions: permissions,
	}
}

func (t *FilePatchTool) Name() string {
	return "file_patch"
}

func (t *FilePatchTool) Description() string {
	return `Apply a multi-file patch using git/unified diff format.

**What is a patch?**
A patch is a diff format that describes changes to files. It can:
- Add new lines (prefixed with +)
- Remove lines (prefixed with -)
- Modify lines (combination of - and +)
- Apply to multiple files in one call
- Create or delete files
- Handle binary patches

**Use Cases:**
- Apply changes from git diff
- Apply suggested code changes
- Multi-file modifications

**Patch Format (unified diff):**
` + "```" + `
--- a/path/to/file.go
+++ b/path/to/file.go
@@ -10,5 +10,6 @@
 context line
-old line
+new line
+another new line
 context line
` + "```" + `

**Behavior:**
- Uses a reliable git/unified diff engine that validates hunk positions and context lines.
- Fails with a conflict error if the patch does not match the current file content.
- Supports multiple files and multiple hunks per file.
- Honors the order of hunks; overlapping hunks will fail.

**Parameters:**
- patch: The patch content in unified diff format (required)
- dry_run: Preview changes without applying (optional, default false)

**Returns:**
- For each file: additions, deletions, and applied status.
- On conflict: an error describing the mismatched hunk or line.

**Security:**
- Only files within the workspace can be modified.
- Cannot patch sensitive files (.env, credentials, etc.)`
}

func (t *FilePatchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"patch": map[string]interface{}{
				"type":        "string",
				"description": "Patch content in unified diff format",
			},
			"dry_run": map[string]interface{}{
				"type":        "boolean",
				"description": "Preview changes without applying (default: false)",
			},
		},
		"required": []string{"patch"},
	}
}

func (t *FilePatchTool) Execute(args map[string]interface{}) (string, error) {
	patch, ok := args["patch"].(string)
	if !ok || strings.TrimSpace(patch) == "" {
		return "", fmt.Errorf("patch is required")
	}

	dryRun, _ := args["dry_run"].(bool)

	files, preamble, err := gitdiff.Parse(strings.NewReader(patch))
	if err != nil {
		return "", fmt.Errorf("parse patch: %w", err)
	}
	if preamble != "" {
		// 忽略 patch 前的邮件头或 commit message，不影响应用。
	}

	if len(files) == 0 {
		return "No files found in patch", nil
	}

	var results []patchFileResult
	for _, file := range files {
		result, err := t.applyFile(file, dryRun)
		if err != nil {
			return "", fmt.Errorf("apply patch to %s: %w", patchFilePath(file), err)
		}
		results = append(results, result)
	}

	return formatPatchResults(results, dryRun), nil
}

type patchFileResult struct {
	Path      string
	Additions int64
	Deletions int64
	Applied   bool
	IsNew     bool
	IsDelete  bool
}

func (t *FilePatchTool) applyFile(file *gitdiff.File, dryRun bool) (patchFileResult, error) {
	result := patchFileResult{
		Path:     patchFilePath(file),
		IsNew:    file.IsNew,
		IsDelete: file.IsDelete,
	}

	fullPath, err := t.resolvePathForPatch(file)
	if err != nil {
		return result, err
	}

	if err := t.validatePath(fullPath); err != nil {
		return result, err
	}

	if t.isSensitivePatchFile(fullPath) {
		return result, fmt.Errorf("cannot patch sensitive file: %s", result.Path)
	}

	if file.IsBinary && file.BinaryFragment != nil {
		result.Additions = file.BinaryFragment.Size
		if dryRun {
			result.Applied = false
			return result, nil
		}
		if err := t.applyBinaryPatch(fullPath, file, &result); err != nil {
			return result, err
		}
		return result, nil
	}

	for _, frag := range file.TextFragments {
		result.Additions += frag.LinesAdded
		result.Deletions += frag.LinesDeleted
	}

	if dryRun {
		result.Applied = false
		return result, nil
	}

	if file.IsDelete {
		if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
			return result, fmt.Errorf("delete file: %w", err)
		}
		result.Applied = true
		return result, nil
	}

	if file.IsNew {
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return result, fmt.Errorf("create parent directory: %w", err)
		}
	}

	src, err := os.Open(fullPath)
	if err != nil && !os.IsNotExist(err) {
		return result, fmt.Errorf("open source file: %w", err)
	}
	if src != nil {
		defer src.Close()
	}

	var dst bytes.Buffer
	if src == nil {
		if file.IsNew {
			if err := gitdiff.Apply(&dst, nil, file); err != nil {
				return result, err
			}
			if err := os.WriteFile(fullPath, dst.Bytes(), 0644); err != nil {
				return result, fmt.Errorf("write new file: %w", err)
			}
			result.Applied = true
			return result, nil
		}
		return result, fmt.Errorf("source file does not exist: %s", result.Path)
	}

	if err := gitdiff.Apply(&dst, src, file); err != nil {
		if errors.Is(err, &gitdiff.Conflict{}) {
			return result, fmt.Errorf("patch conflict: %w", err)
		}
		return result, err
	}

	if err := os.WriteFile(fullPath, dst.Bytes(), 0644); err != nil {
		return result, fmt.Errorf("write patched file: %w", err)
	}
	result.Applied = true
	return result, nil
}

func (t *FilePatchTool) applyBinaryPatch(fullPath string, file *gitdiff.File, result *patchFileResult) error {
	if file.BinaryFragment == nil {
		return fmt.Errorf("binary file missing binary fragment")
	}

	src, err := os.Open(fullPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("open binary source: %w", err)
	}
	if src != nil {
		defer src.Close()
	}

	var dst bytes.Buffer
	applier := gitdiff.NewBinaryApplier(&dst, src)
	if err := applier.ApplyFragment(file.BinaryFragment); err != nil {
		if errors.Is(err, &gitdiff.Conflict{}) {
			return fmt.Errorf("binary patch conflict: %w", err)
		}
		return err
	}
	if err := applier.Close(); err != nil {
		return err
	}

	if err := os.WriteFile(fullPath, dst.Bytes(), 0644); err != nil {
		return fmt.Errorf("write binary file: %w", err)
	}
	result.Applied = true
	return nil
}

func (t *FilePatchTool) resolvePathForPatch(file *gitdiff.File) (string, error) {
	target := file.NewName
	if target == "" || target == "/dev/null" {
		target = file.OldName
	}
	if target == "" || target == "/dev/null" {
		return "", fmt.Errorf("patch file has no valid path")
	}
	target = cleanPatchPath(target)
	return t.permissions.ResolvePath(target)
}

func (t *FilePatchTool) validatePath(path string) error {
	return t.permissions.CanAccessPath(path)
}

func (t *FilePatchTool) isSensitivePatchFile(path string) bool {
	filename := strings.ToLower(filepath.Base(path))
	sensitiveFiles := []string{
		".env",
		".env.local",
		".env.production",
		".env.development",
		"credentials.json",
		"secrets.json",
		"id_rsa",
		"id_ed25519",
		".gitconfig",
		".npmrc",
		".pypirc",
	}
	for _, sensitive := range sensitiveFiles {
		if filename == sensitive {
			return true
		}
	}
	return false
}

func patchFilePath(file *gitdiff.File) string {
	if file.NewName != "" && file.NewName != "/dev/null" {
		return cleanPatchPath(file.NewName)
	}
	if file.OldName != "" && file.OldName != "/dev/null" {
		return cleanPatchPath(file.OldName)
	}
	return "(unknown)"
}

func cleanPatchPath(p string) string {
	p = strings.TrimPrefix(p, "a/")
	p = strings.TrimPrefix(p, "b/")
	p = strings.TrimSpace(p)
	return filepath.FromSlash(p)
}

func formatPatchResults(results []patchFileResult, dryRun bool) string {
	var output strings.Builder

	if dryRun {
		output.WriteString("# Dry Run Results\n\n")
	} else {
		output.WriteString("# Patch Applied\n\n")
	}

	var totalAdditions, totalDeletions int64
	for _, r := range results {
		status := "previewed"
		if r.Applied {
			status = "applied"
		}
		output.WriteString(fmt.Sprintf("## %s (%s)\n", r.Path, status))
		if r.IsNew {
			output.WriteString("- Created new file\n")
		}
		if r.IsDelete {
			output.WriteString("- Deleted file\n")
		}
		output.WriteString(fmt.Sprintf("- Additions: %d\n", r.Additions))
		output.WriteString(fmt.Sprintf("- Deletions: %d\n\n", r.Deletions))
		totalAdditions += r.Additions
		totalDeletions += r.Deletions
	}

	output.WriteString(fmt.Sprintf("**Total: +%d -%d**\n", totalAdditions, totalDeletions))
	return output.String()
}

func (t *FilePatchTool) SetWorkspace(workspace string) {
	t.permissions.Workspace = workspace
}
