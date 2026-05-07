package textedit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Operation string

const (
	OpCreate         Operation = "create"
	OpWrite          Operation = "write"
	OpAppend         Operation = "append"
	OpPrepend        Operation = "prepend"
	OpReplaceText    Operation = "replace_text"
	OpReplaceLines   Operation = "replace_lines"
	OpInsertAtLine   Operation = "insert_at_line"
	OpAppendAtLine   Operation = "append_at_line"
	OpDeleteLines    Operation = "delete_lines"
	OpInsertBefore   Operation = "insert_before"
	OpInsertAfter    Operation = "insert_after"
	OpReplaceBetween Operation = "replace_between"
)

type Edit struct {
	Operation        Operation
	Content          string
	OldText          string
	NewText          string
	StartLine        int
	EndLine          int
	Line             int
	Expected         string
	Anchor           string
	StartMarker      string
	EndMarker        string
	ReplaceAll       bool
	Occurrence       int
	CreateIfMissing  bool
	IncludeMarkers   bool
}

type Result struct {
	Operation     Operation
	Additions     int
	Deletions     int
	Replacements  int
	LinesInserted int
	Message       string
}

func Apply(edit Edit, source string) (result string, res *Result, err error) {
	switch edit.Operation {
	case OpCreate, OpWrite:
		return edit.Content, &Result{Operation: edit.Operation, Message: fmt.Sprintf("Successfully %s", edit.Operation)}, nil
	case OpAppend:
		if source == "" {
			return edit.Content, &Result{Operation: edit.Operation, Additions: len(edit.Content), Message: "Successfully appended to empty source"}, nil
		}
		return source + edit.Content, &Result{Operation: edit.Operation, Additions: len(edit.Content), Message: "Successfully appended"}, nil
	case OpPrepend:
		if source == "" {
			return edit.Content, &Result{Operation: edit.Operation, Additions: len(edit.Content), Message: "Successfully prepended to empty source"}, nil
		}
		return edit.Content + source, &Result{Operation: edit.Operation, Additions: len(edit.Content), Message: "Successfully prepended"}, nil
	case OpReplaceText:
		return applyReplaceText(edit, source)
	case OpReplaceLines, OpDeleteLines:
		return applyLineRange(edit, source)
	case OpInsertAtLine, OpAppendAtLine:
		return applyInsertAtLine(edit, source)
	case OpInsertBefore, OpInsertAfter:
		return applyAnchor(edit, source)
	case OpReplaceBetween:
		return applyReplaceBetween(edit, source)
	default:
		return "", nil, fmt.Errorf("unsupported operation: %s", edit.Operation)
	}
}

func ApplyFile(edit Edit, path string, mode os.FileMode) (*Result, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("path is required")
	}

	switch edit.Operation {
	case OpCreate:
		if _, err := os.Stat(path); err == nil {
			return nil, fmt.Errorf("file already exists: %s", path)
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("stat file: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return nil, fmt.Errorf("create directories: %w", err)
		}
		if err := os.WriteFile(path, []byte(edit.Content), mode); err != nil {
			return nil, fmt.Errorf("write file: %w", err)
		}
		return &Result{Operation: edit.Operation, Message: "Successfully created file"}, nil

	case OpWrite:
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return nil, fmt.Errorf("create directories: %w", err)
		}
		if err := os.WriteFile(path, []byte(edit.Content), mode); err != nil {
			return nil, fmt.Errorf("write file: %w", err)
		}
		return &Result{Operation: edit.Operation, Message: "Successfully wrote file"}, nil

	case OpAppend, OpPrepend:
		return applyAppendPrependFile(edit, path, mode)

	default:
		source, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("read file: %w", err)
		}
		if err != nil && os.IsNotExist(err) && !edit.CreateIfMissing {
			return nil, fmt.Errorf("file does not exist: %s", path)
		}

		// binary check
		if len(source) > 0 && isBinary(source) {
			return nil, errors.New("cannot text-edit binary file")
		}

		result, res, err := Apply(edit, string(source))
		if err != nil {
			return nil, err
		}

		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return nil, fmt.Errorf("create directories: %w", err)
		}
		if err := os.WriteFile(path, []byte(result), mode); err != nil {
			return nil, fmt.Errorf("write file: %w", err)
		}
		return res, nil
	}
}

func applyAppendPrependFile(edit Edit, path string, mode os.FileMode) (*Result, error) {
	source, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if err != nil && os.IsNotExist(err) && !edit.CreateIfMissing {
		return nil, fmt.Errorf("file does not exist: %s", path)
	}

	if len(source) > 0 && isBinary(source) {
		return nil, errors.New("cannot text-edit binary file")
	}

	result, res, err := Apply(edit, string(source))
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create directories: %w", err)
	}
	if err := os.WriteFile(path, []byte(result), mode); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}
	return res, nil
}

func applyReplaceText(edit Edit, source string) (string, *Result, error) {
	if edit.OldText == "" {
		return "", nil, errors.New("old_text is required for replace_text")
	}
	if edit.NewText == "" {
		edit.NewText = ""
	}

	occurrences := strings.Count(source, edit.OldText)
	if occurrences == 0 {
		return "", nil, errors.New("old_text not found in source")
	}

	if edit.ReplaceAll {
		result := strings.ReplaceAll(source, edit.OldText, edit.NewText)
		return result, &Result{Operation: edit.Operation, Replacements: occurrences, Message: fmt.Sprintf("Successfully replaced %d occurrences", occurrences)}, nil
	}

	occurrence := edit.Occurrence
	if occurrence == 0 {
		if occurrences > 1 {
			return "", nil, fmt.Errorf("old_text appears %d times. Use replace_all=true, occurrence, or make old_text more specific", occurrences)
		}
		occurrence = 1
	}

	result, err := replaceNth(source, edit.OldText, edit.NewText, occurrence)
	if err != nil {
		return "", nil, err
	}
	return result, &Result{Operation: edit.Operation, Replacements: 1, Message: fmt.Sprintf("Successfully replaced occurrence %d", occurrence)}, nil
}

func replaceNth(source, oldText, newText string, occurrence int) (string, error) {
	if occurrence <= 0 {
		return "", errors.New("occurrence must be greater than 0")
	}
	searchFrom := 0
	for i := 1; i <= occurrence; i++ {
		idx := strings.Index(source[searchFrom:], oldText)
		if idx < 0 {
			return "", fmt.Errorf("occurrence %d not found", occurrence)
		}
		idx += searchFrom
		if i == occurrence {
			return source[:idx] + newText + source[idx+len(oldText):], nil
		}
		searchFrom = idx + len(oldText)
	}
	return source, nil
}

func applyLineRange(edit Edit, source string) (string, *Result, error) {
	lines, trailingNewline := splitLines(source)
	total := len(lines)

	startLine := edit.StartLine
	endLine := edit.EndLine
	if endLine == 0 {
		endLine = startLine
	}

	if startLine <= 0 || endLine <= 0 {
		return "", nil, errors.New("start_line and end_line must be greater than 0")
	}
	if endLine < startLine {
		return "", nil, fmt.Errorf("end_line %d cannot be before start_line %d", endLine, startLine)
	}
	if startLine > total || endLine > total {
		return "", nil, fmt.Errorf("line range %d-%d outside file length %d", startLine, endLine, total)
	}

	replaced := strings.Join(lines[startLine-1:endLine], "\n")
	if edit.Expected != "" && edit.Expected != replaced {
		return "", nil, fmt.Errorf("expected content does not match lines %d-%d", startLine, endLine)
	}

	replacement := []string{}
	if edit.Operation == OpReplaceLines {
		replacementLines, _ := splitLines(edit.Content)
		replacement = replacementLines
	}

	newLines := append([]string{}, lines[:startLine-1]...)
	newLines = append(newLines, replacement...)
	newLines = append(newLines, lines[endLine:]...)

	result := joinLines(newLines, trailingNewline)
	if edit.Operation == OpDeleteLines {
		return result, &Result{Operation: edit.Operation, Deletions: endLine - startLine + 1, Message: fmt.Sprintf("Successfully deleted lines %d-%d", startLine, endLine)}, nil
	}
	return result, &Result{Operation: edit.Operation, Replacements: 1, Message: fmt.Sprintf("Successfully replaced lines %d-%d", startLine, endLine)}, nil
}

func applyInsertAtLine(edit Edit, source string) (string, *Result, error) {
	line := edit.Line
	if line <= 0 {
		return "", nil, errors.New("line must be greater than 0")
	}

	lines, trailingNewline := splitLines(source)
	total := len(lines)

	if edit.Operation == OpAppendAtLine {
		line++
	}

	if line < 1 || line > total+1 {
		return "", nil, fmt.Errorf("line %d outside valid range 1-%d", line, total+1)
	}

	insertLines, _ := splitLines(edit.Content)
	idx := line - 1

	newLines := append([]string{}, lines[:idx]...)
	newLines = append(newLines, insertLines...)
	newLines = append(newLines, lines[idx:]...)

	result := joinLines(newLines, trailingNewline)
	return result, &Result{Operation: edit.Operation, LinesInserted: len(insertLines), Message: fmt.Sprintf("Successfully inserted %d lines", len(insertLines))}, nil
}

func applyAnchor(edit Edit, source string) (string, *Result, error) {
	if edit.Anchor == "" {
		return "", nil, errors.New("anchor is required")
	}

	occurrences := strings.Count(source, edit.Anchor)
	if occurrences == 0 {
		return "", nil, errors.New("anchor not found in source")
	}

	occurrence := edit.Occurrence
	if occurrence == 0 {
		occurrence = 1
	}

	idx, err := nthIndex(source, edit.Anchor, occurrence)
	if err != nil {
		return "", nil, err
	}

	insertAt := idx
	if edit.Operation == OpInsertAfter {
		insertAt = idx + len(edit.Anchor)
	}

	result := source[:insertAt] + edit.Content + source[insertAt:]
	return result, &Result{Operation: edit.Operation, LinesInserted: len(edit.Content), Message: fmt.Sprintf("Successfully inserted content %s occurrence %d", strings.TrimPrefix(edit.Operation.String(), "insert_"), occurrence)}, nil
}

func applyReplaceBetween(edit Edit, source string) (string, *Result, error) {
	if edit.StartMarker == "" || edit.EndMarker == "" {
		return "", nil, errors.New("start_marker and end_marker are required")
	}

	occurrence := edit.Occurrence
	if occurrence == 0 {
		occurrence = 1
	}

	startIdx, err := nthIndex(source, edit.StartMarker, occurrence)
	if err != nil {
		return "", nil, err
	}

	searchFrom := startIdx + len(edit.StartMarker)
	endRel := strings.Index(source[searchFrom:], edit.EndMarker)
	if endRel < 0 {
		return "", nil, fmt.Errorf("end_marker not found after start_marker occurrence %d", occurrence)
	}
	endIdx := searchFrom + endRel

	replaceStart := searchFrom
	replaceEnd := endIdx
	if edit.IncludeMarkers {
		replaceStart = startIdx
		replaceEnd = endIdx + len(edit.EndMarker)
	}

	result := source[:replaceStart] + edit.Content + source[replaceEnd:]
	return result, &Result{Operation: edit.Operation, Replacements: 1, Message: "Successfully replaced content between markers"}, nil
}

func nthIndex(source, needle string, occurrence int) (int, error) {
	if occurrence <= 0 {
		return 0, errors.New("occurrence must be greater than 0")
	}
	searchFrom := 0
	for i := 1; i <= occurrence; i++ {
		idx := strings.Index(source[searchFrom:], needle)
		if idx < 0 {
			return 0, fmt.Errorf("occurrence %d not found", occurrence)
		}
		idx += searchFrom
		if i == occurrence {
			return idx, nil
		}
		searchFrom = idx + len(needle)
	}
	return 0, fmt.Errorf("occurrence %d not found", occurrence)
}

func splitLines(content string) ([]string, bool) {
	trailingNewline := strings.HasSuffix(content, "\n")
	content = strings.TrimSuffix(content, "\n")
	content = strings.TrimSuffix(content, "\r")
	if content == "" {
		return []string{}, trailingNewline
	}
	return strings.Split(content, "\n"), trailingNewline
}

func joinLines(lines []string, trailingNewline bool) string {
	content := strings.Join(lines, "\n")
	if trailingNewline && content != "" {
		content += "\n"
	}
	return content
}

func isBinary(content []byte) bool {
	if len(content) == 0 {
		return false
	}
	checkLen := len(content)
	if checkLen > 512 {
		checkLen = 512
	}
	for i := 0; i < checkLen; i++ {
		if content[i] == 0 {
			return true
		}
	}
	nonPrintable := 0
	for i := 0; i < checkLen; i++ {
		if content[i] < 32 && content[i] != '\n' && content[i] != '\r' && content[i] != '\t' {
			nonPrintable++
		}
	}
	return float64(nonPrintable)/float64(checkLen) > 0.3
}

func (op Operation) String() string {
	return string(op)
}

func ParseFileMode(raw interface{}) os.FileMode {
	mode := os.FileMode(0644)
	modeStr, _ := raw.(string)
	if strings.TrimSpace(modeStr) == "" {
		return mode
	}
	parsed, err := strconv.ParseUint(strings.TrimSpace(modeStr), 8, 32)
	if err == nil {
		mode = os.FileMode(parsed)
	}
	return mode
}