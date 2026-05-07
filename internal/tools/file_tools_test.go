package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
)

func testFilePermissions(t *testing.T) (AgentPermissions, string) {
	t.Helper()
	workspace := t.TempDir()
	return NewAgentPermissions(models.RoleEmployee, workspace, config.AgentPermissionConfig{}), workspace
}

func TestFileReadLineRangeAndRawOutput(t *testing.T) {
	perms, workspace := testFilePermissions(t)
	path := filepath.Join(workspace, "note.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tool := NewFileReadTool(perms)
	out, err := tool.Execute(map[string]interface{}{
		"path":       "note.txt",
		"start_line": 2,
		"end_line":   3,
	})
	if err != nil {
		t.Fatalf("read line range: %v", err)
	}
	if !strings.Contains(out, "   2→two") || !strings.Contains(out, "   3→three") || strings.Contains(out, "   1→one") {
		t.Fatalf("unexpected numbered output:\n%s", out)
	}

	raw, err := tool.Execute(map[string]interface{}{
		"path":         "note.txt",
		"start_line":   2,
		"limit":        2,
		"line_numbers": false,
	})
	if err != nil {
		t.Fatalf("read raw range: %v", err)
	}
	if raw != "two\nthree" {
		t.Fatalf("unexpected raw output: %q", raw)
	}
}

func TestFileWriteCreateAndOverwrite(t *testing.T) {
	perms, workspace := testFilePermissions(t)
	tool := NewFileWriteTool(perms)

	if _, err := tool.Execute(map[string]interface{}{"path": "a.txt", "operation": "create", "content": "middle"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := tool.Execute(map[string]interface{}{"path": "a.txt", "operation": "create", "content": "again"}); err == nil {
		t.Fatal("expected duplicate create to fail")
	}
	if _, err := tool.Execute(map[string]interface{}{"path": "a.txt", "operation": "write", "content": "replaced"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := tool.Execute(map[string]interface{}{"path": "a.txt", "operation": "append", "content": " end"}); err == nil {
		t.Fatal("expected file_write append to fail")
	}

	data, err := os.ReadFile(filepath.Join(workspace, "a.txt"))
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	if string(data) != "replaced" {
		t.Fatalf("unexpected content: %q", string(data))
	}
}

func TestFileEditAppendPrependAndReplaceText(t *testing.T) {
	perms, workspace := testFilePermissions(t)
	path := filepath.Join(workspace, "a.txt")
	if err := os.WriteFile(path, []byte("middle alpha beta beta"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tool := NewFileEditTool(perms)
	if _, err := tool.Execute(map[string]interface{}{"path": "a.txt", "operation": "append", "content": " end"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := tool.Execute(map[string]interface{}{"path": "a.txt", "operation": "prepend", "content": "start "}); err != nil {
		t.Fatalf("prepend: %v", err)
	}

	// Replace second occurrence
	if _, err := tool.Execute(map[string]interface{}{
		"path":       "a.txt",
		"operation":  "replace_text",
		"old_text":   "beta",
		"new_text":   "gamma",
		"occurrence": 2,
	}); err != nil {
		t.Fatalf("replace_text occurrence: %v", err)
	}

	// Replace first occurrence
	if _, err := tool.Execute(map[string]interface{}{
		"path":      "a.txt",
		"operation": "replace_text",
		"old_text":  "alpha",
		"new_text":  "omega",
	}); err != nil {
		t.Fatalf("replace_text first occurrence: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	if string(data) != "start middle omega beta gamma end" {
		t.Fatalf("unexpected content: %q", string(data))
	}
}

func TestFileEditRejectsLineOperations(t *testing.T) {
	perms, workspace := testFilePermissions(t)
	tool := NewFileEditTool(perms)
	path := filepath.Join(workspace, "lines.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if _, err := tool.Execute(map[string]interface{}{
		"path":       "lines.txt",
		"operation":  "replace_lines",
		"start_line": 2,
		"end_line":   3,
		"expected":   "two\nthree",
		"content":    "TWO\nTHREE",
	}); err == nil {
		t.Fatal("expected replace_lines to be rejected")
	}
}

func TestFileEditAnchorAndMarkerOperations(t *testing.T) {
	perms, workspace := testFilePermissions(t)
	tool := NewFileEditTool(perms)
	path := filepath.Join(workspace, "doc.md")
	if err := os.WriteFile(path, []byte("# Title\n\n<!-- start -->\nold\n<!-- end -->\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if _, err := tool.Execute(map[string]interface{}{
		"path":      "doc.md",
		"operation": "insert_after",
		"anchor":    "# Title",
		"content":   "\nintro",
	}); err != nil {
		t.Fatalf("insert_after: %v", err)
	}
	if _, err := tool.Execute(map[string]interface{}{
		"path":         "doc.md",
		"operation":    "replace_between",
		"start_marker": "<!-- start -->",
		"end_marker":   "<!-- end -->",
		"content":      "\nnew\n",
	}); err != nil {
		t.Fatalf("replace_between: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	want := "# Title\nintro\n\n<!-- start -->\nnew\n<!-- end -->\n"
	if string(data) != want {
		t.Fatalf("unexpected content:\nwant %q\n got %q", want, string(data))
	}
}

func TestFilePatchSingleFileApply(t *testing.T) {
	perms, workspace := testFilePermissions(t)
	tool := NewFilePatchTool(perms)

	src := filepath.Join(workspace, "src.go")
	if err := os.WriteFile(src, []byte("package main\n\nfunc old() {}\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	patch := `--- a/src.go
+++ b/src.go
@@ -3,1 +3,1 @@
-func old() {}
+func new() {}
`
	if _, err := tool.Execute(map[string]interface{}{"patch": patch}); err != nil {
		t.Fatalf("apply patch: %v", err)
	}

	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	want := "package main\n\nfunc new() {}\n"
	if string(data) != want {
		t.Fatalf("unexpected content:\nwant %q\n got %q", want, string(data))
	}
}

func TestFilePatchMultiFileApply(t *testing.T) {
	perms, workspace := testFilePermissions(t)
	tool := NewFilePatchTool(perms)

	file1 := filepath.Join(workspace, "a.txt")
	file2 := filepath.Join(workspace, "b.txt")
	if err := os.WriteFile(file1, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	if err := os.WriteFile(file2, []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatalf("write b.txt: %v", err)
	}

	patch := `--- a/a.txt
+++ b/a.txt
@@ -1,2 +1,2 @@
 one
-two
+TWO
--- a/b.txt
+++ b/b.txt
@@ -1,2 +1,2 @@
 alpha
-beta
+BETA
`
	if _, err := tool.Execute(map[string]interface{}{"patch": patch}); err != nil {
		t.Fatalf("apply multi-file patch: %v", err)
	}

	data1, err := os.ReadFile(file1)
	if err != nil {
		t.Fatalf("read a.txt: %v", err)
	}
	if string(data1) != "one\nTWO\n" {
		t.Fatalf("a.txt unexpected: %q", string(data1))
	}

	data2, err := os.ReadFile(file2)
	if err != nil {
		t.Fatalf("read b.txt: %v", err)
	}
	if string(data2) != "alpha\nBETA\n" {
		t.Fatalf("b.txt unexpected: %q", string(data2))
	}
}

func TestFilePatchDryRunPreview(t *testing.T) {
	perms, workspace := testFilePermissions(t)
	tool := NewFilePatchTool(perms)

	src := filepath.Join(workspace, "note.txt")
	if err := os.WriteFile(src, []byte("line1\nline2\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	patch := `--- a/note.txt
+++ b/note.txt
@@ -1,2 +1,3 @@
 line1
 line2
+line3
`
	out, err := tool.Execute(map[string]interface{}{
		"patch":   patch,
		"dry_run": true,
	})
	if err != nil {
		t.Fatalf("dry_run preview: %v", err)
	}
	if !strings.Contains(out, "previewed") || !strings.Contains(out, "Additions: 1") {
		t.Fatalf("dry_run output missing preview stats:\n%s", out)
	}

	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read source after dry_run: %v", err)
	}
	if string(data) != "line1\nline2\n" {
		t.Fatalf("dry_run should not modify file: %q", string(data))
	}
}

func TestFilePatchConflictError(t *testing.T) {
	perms, workspace := testFilePermissions(t)
	tool := NewFilePatchTool(perms)

	src := filepath.Join(workspace, "conflict.txt")
	if err := os.WriteFile(src, []byte("actual line\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	patch := `--- a/conflict.txt
+++ b/conflict.txt
@@ -1,1 +1,1 @@
-expected line
+new line
`
	_, err := tool.Execute(map[string]interface{}{"patch": patch})
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("expected conflict in error message: %v", err)
	}

	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read source after conflict: %v", err)
	}
	if string(data) != "actual line\n" {
		t.Fatalf("conflict patch should not modify file: %q", string(data))
	}
}
