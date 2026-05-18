package tools

import (
	"testing"
)

func TestFindLineEnds(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		expected []int
	}{
		{
			name:     "empty",
			data:     "",
			expected: nil,
		},
		{
			name:     "single line no newline",
			data:     "hello",
			expected: nil,
		},
		{
			name:     "single line with newline",
			data:     "hello\n",
			expected: []int{5},
		},
		{
			name:     "multiple lines",
			data:     "line1\nline2\nline3\n",
			expected: []int{5, 11, 17},
		},
		{
			name:     "no final newline",
			data:     "line1\nline2\nline3",
			expected: []int{5, 11},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findLineEndsFromEnd([]byte(tt.data))
			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d line ends, got %d: %v", len(tt.expected), len(result), result)
			}
			for i, pos := range result {
				if pos != tt.expected[i] {
					t.Fatalf("expected line end %d at position %d, got %d", i, tt.expected[i], pos)
				}
			}
		})
	}
}

func TestCountLines(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		expected int
	}{
		{"empty", "", 0},
		{"single no newline", "hello", 1},
		{"single with newline", "hello\n", 1},
		{"multiple with final newline", "line1\nline2\nline3\n", 3},
		{"multiple no final newline", "line1\nline2\nline3", 3},
		{"two lines", "a\nb", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := countLines([]byte(tt.data))
			if result != tt.expected {
				t.Fatalf("expected %d lines, got %d", tt.expected, result)
			}
		})
	}
}

func TestLogPaginationLogic(t *testing.T) {
	// Simulate output buffer content with multiple lines
	// Lines are numbered from oldest at top to newest at bottom
	// line1 (oldest) -> line10 (newest)
	data := []byte("line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n")
	lineEnds := findLineEndsFromEnd(data)
	totalLines := len(lineEnds) // 10 lines

	if totalLines != 10 {
		t.Fatalf("expected 10 lines, got %d", totalLines)
	}

	// Test: offset=0, lines=3 -> should get last 3 lines (line8, line9, line10)
	t.Run("offset0_lines3", func(t *testing.T) {
		offset := 0
		linesToTake := 3

		startLineIdx := totalLines - offset - linesToTake // 7
		endLineIdx := totalLines - offset                 // 10

		// Verify line indices
		if startLineIdx != 7 {
			t.Fatalf("expected startLineIdx=7, got %d", startLineIdx)
		}
		if endLineIdx != 10 {
			t.Fatalf("expected endLineIdx=10, got %d", endLineIdx)
		}

		// Extract content using implementation logic
		startBytePos := 0
		if startLineIdx > 0 {
			startBytePos = lineEnds[startLineIdx-1] + 1 // after line7's newline
		}
		endBytePos := len(data)
		if endLineIdx > 0 && endLineIdx <= len(lineEnds) {
			endBytePos = lineEnds[endLineIdx-1] + 1 // after line10's newline
		}

		content := string(data[startBytePos:endBytePos])
		expected := "line8\nline9\nline10\n"
		if content != expected {
			t.Fatalf("expected content %q, got %q (startBytePos=%d, endBytePos=%d)", expected, content, startBytePos, endBytePos)
		}
	})

	// Test: offset=3, lines=3 -> should get lines 4-6 from end (line5, line6, line7)
	t.Run("offset3_lines3", func(t *testing.T) {
		offset := 3
		linesToTake := 3

		startLineIdx := totalLines - offset - linesToTake // 4
		endLineIdx := totalLines - offset                 // 7

		if startLineIdx != 4 {
			t.Fatalf("expected startLineIdx=4, got %d", startLineIdx)
		}
		if endLineIdx != 7 {
			t.Fatalf("expected endLineIdx=7, got %d", endLineIdx)
		}

		startBytePos := 0
		if startLineIdx > 0 {
			startBytePos = lineEnds[startLineIdx-1] + 1 // after line4's newline
		}
		endBytePos := len(data)
		if endLineIdx > 0 && endLineIdx <= len(lineEnds) {
			endBytePos = lineEnds[endLineIdx-1] + 1 // after line7's newline
		}

		content := string(data[startBytePos:endBytePos])
		expected := "line5\nline6\nline7\n"
		if content != expected {
			t.Fatalf("expected content %q, got %q", expected, content)
		}
	})

	// Test pagination flags
	t.Run("pagination_flags", func(t *testing.T) {
		// offset=0, lines=3 -> hasOlder=true, hasNewer=false
		offset := 0
		linesToTake := 3
		startLineIdx := totalLines - offset - linesToTake

		hasOlder := startLineIdx > 0
		hasNewer := offset > 0

		if !hasOlder {
			t.Fatal("expected hasOlder=true for offset=0, lines=3")
		}
		if hasNewer {
			t.Fatal("expected hasNewer=false for offset=0")
		}

		// offset=3, lines=3 -> hasOlder=true, hasNewer=true
		offset = 3
		startLineIdx = totalLines - offset - linesToTake
		hasOlder = startLineIdx > 0
		hasNewer = offset > 0

		if !hasOlder {
			t.Fatal("expected hasOlder=true for offset=3, lines=3")
		}
		if !hasNewer {
			t.Fatal("expected hasNewer=true for offset=3")
		}

		// offset=7, lines=3 -> hasOlder=false, hasNewer=true
		offset = 7
		startLineIdx = totalLines - offset - linesToTake // 10 - 7 - 3 = 0
		hasOlder = startLineIdx > 0
		hasNewer = offset > 0

		if hasOlder {
			t.Fatal("expected hasOlder=false for offset=7, lines=3")
		}
		if !hasNewer {
			t.Fatal("expected hasNewer=true for offset=7")
		}
	})
}