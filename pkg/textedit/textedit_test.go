package textedit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyCreateWriteAppendPrepend(t *testing.T) {
	edit := Edit{Operation: OpCreate, Content: "hello"}
	result, _, err := Apply(edit, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if result != "hello" {
		t.Fatalf("unexpected create result: %q", result)
	}

	edit = Edit{Operation: OpWrite, Content: "world"}
	result, _, err = Apply(edit, "old")
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if result != "world" {
		t.Fatalf("unexpected write result: %q", result)
	}

	edit = Edit{Operation: OpAppend, Content: " end"}
	result, _, err = Apply(edit, "middle")
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if result != "middle end" {
		t.Fatalf("unexpected append result: %q", result)
	}

	edit = Edit{Operation: OpPrepend, Content: "start "}
	result, _, err = Apply(edit, "middle")
	if err != nil {
		t.Fatalf("prepend: %v", err)
	}
	if result != "start middle" {
		t.Fatalf("unexpected prepend result: %q", result)
	}
}

func TestApplyReplaceText(t *testing.T) {
	source := "alpha beta beta"

	edit := Edit{Operation: OpReplaceText, OldText: "beta", NewText: "gamma", Occurrence: 2}
	result, _, err := Apply(edit, source)
	if err != nil {
		t.Fatalf("replace_text occurrence: %v", err)
	}
	if result != "alpha beta gamma" {
		t.Fatalf("unexpected result: %q", result)
	}

	edit = Edit{Operation: OpReplaceText, OldText: "alpha", NewText: "omega"}
	result, _, err = Apply(edit, result)
	if err != nil {
		t.Fatalf("replace_text first: %v", err)
	}
	if result != "omega beta gamma" {
		t.Fatalf("unexpected result: %q", result)
	}

	edit = Edit{Operation: OpReplaceText, OldText: "beta", NewText: "BETA", ReplaceAll: true}
	result, _, err = Apply(edit, "beta beta beta")
	if err != nil {
		t.Fatalf("replace_text all: %v", err)
	}
	if result != "BETA BETA BETA" {
		t.Fatalf("unexpected result: %q", result)
	}

	edit = Edit{Operation: OpReplaceText, OldText: "missing", NewText: "x"}
	_, _, err = Apply(edit, source)
	if err == nil {
		t.Fatal("expected error for missing old_text")
	}
}

func TestApplyLineRange(t *testing.T) {
	source := "one\ntwo\nthree\nfour\n"

	edit := Edit{Operation: OpReplaceLines, StartLine: 2, EndLine: 3, Expected: "two\nthree", Content: "TWO\nTHREE"}
	result, _, err := Apply(edit, source)
	if err != nil {
		t.Fatalf("replace_lines: %v", err)
	}
	if result != "one\nTWO\nTHREE\nfour\n" {
		t.Fatalf("unexpected result: %q", result)
	}

	edit = Edit{Operation: OpDeleteLines, StartLine: 4, EndLine: 4}
	result, _, err = Apply(edit, result)
	if err != nil {
		t.Fatalf("delete_lines: %v", err)
	}
	if result != "one\nTWO\nTHREE\n" {
		t.Fatalf("unexpected result: %q", result)
	}

	edit = Edit{Operation: OpReplaceLines, StartLine: 2, EndLine: 2, Expected: "wrong", Content: "X"}
	_, _, err = Apply(edit, source)
	if err == nil || !strings.Contains(err.Error(), "expected content does not match") {
		t.Fatalf("expected mismatch error: %v", err)
	}
}

func TestApplyInsertAtLine(t *testing.T) {
	source := "one\ntwo\nthree\n"

	edit := Edit{Operation: OpInsertAtLine, Line: 2, Content: "inserted\n"}
	result, _, err := Apply(edit, source)
	if err != nil {
		t.Fatalf("insert_at_line: %v", err)
	}
	if result != "one\ninserted\ntwo\nthree\n" {
		t.Fatalf("unexpected result: %q", result)
	}

	edit = Edit{Operation: OpAppendAtLine, Line: 3, Content: "extra"}
	result, _, err = Apply(edit, source)
	if err != nil {
		t.Fatalf("append_at_line: %v", err)
	}
	if result != "one\ntwo\nthree\nextra\n" {
		t.Fatalf("unexpected result: %q", result)
	}
}

func TestApplyAnchor(t *testing.T) {
	source := "# Title\n\nContent here\n"

	edit := Edit{Operation: OpInsertAfter, Anchor: "# Title", Content: "\nintro"}
	result, _, err := Apply(edit, source)
	if err != nil {
		t.Fatalf("insert_after: %v", err)
	}
	if result != "# Title\nintro\n\nContent here\n" {
		t.Fatalf("unexpected result: %q", result)
	}

	edit = Edit{Operation: OpInsertBefore, Anchor: "Content here", Content: "Before content. "}
	result, _, err = Apply(edit, source)
	if err != nil {
		t.Fatalf("insert_before: %v", err)
	}
	if result != "# Title\n\nBefore content. Content here\n" {
		t.Fatalf("unexpected result: %q", result)
	}
}

func TestApplyReplaceBetween(t *testing.T) {
	source := "# Title\n<!-- start -->\nold\n<!-- end -->\n"

	edit := Edit{Operation: OpReplaceBetween, StartMarker: "<!-- start -->", EndMarker: "<!-- end -->", Content: "\nnew\n"}
	result, _, err := Apply(edit, source)
	if err != nil {
		t.Fatalf("replace_between: %v", err)
	}
	if result != "# Title\n<!-- start -->\nnew\n<!-- end -->\n" {
		t.Fatalf("unexpected result: %q", result)
	}

	edit = Edit{Operation: OpReplaceBetween, StartMarker: "<!-- start -->", EndMarker: "<!-- end -->", Content: "full replacement", IncludeMarkers: true}
	result, _, err = Apply(edit, source)
	if err != nil {
		t.Fatalf("replace_between include_markers: %v", err)
	}
	if result != "# Title\nfull replacement\n" {
		t.Fatalf("unexpected result: %q", result)
	}
}

func TestApplyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	edit := Edit{Operation: OpCreate, Content: "initial\n"}
	_, err := ApplyFile(edit, path, 0644)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}

	edit = Edit{Operation: OpAppend, Content: " appended"}
	_, err = ApplyFile(edit, path, 0644)
	if err != nil {
		t.Fatalf("append file: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "initial\n appended" {
		t.Fatalf("unexpected file content: %q", string(data))
	}
}