package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCodeExecutionTimeoutStopsCommand(t *testing.T) {
	tool := NewCodeExecutionTool()

	language := "python"
	code := "import time\ntime.sleep(20)"
	if isWindowsShellDefault() {
		language = "javascript"
		code = "setTimeout(() => {}, 20000)"
	}

	started := time.Now()
	result, err := tool.ExecuteWithContext(context.Background(), map[string]interface{}{
		"language": language,
		"code":     code,
		"timeout":  float64(1),
	})
	if err != nil {
		t.Fatalf("code_execution returned unexpected error: %v", err)
	}

	elapsed := time.Since(started)
	if elapsed > 5*time.Second {
		t.Fatalf("expected timeout to stop quickly, took %v", elapsed)
	}
	if !strings.Contains(result, "Timeout") {
		t.Fatalf("expected timeout result, got: %s", result)
	}
}
