package mcp

import (
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// TestMCPToolWrapperCallTimeout 验证 wrapper 正确上报配置的调用超时
func TestMCPToolWrapperCallTimeout(t *testing.T) {
	client := &Client{name: "test_server"}
	tool := &mcp.Tool{Name: "search"}

	// 0 表示走全局默认
	w0 := NewMCPToolWrapper(client, tool)
	if d := w0.CallTimeout(); d != 0 {
		t.Fatalf("expected 0 for default wrapper, got %v", d)
	}

	// 显式配置 30s
	w30 := NewMCPToolWrapperWithTimeout(client, tool, 30*time.Second)
	if d := w30.CallTimeout(); d != 30*time.Second {
		t.Fatalf("expected 30s, got %v", d)
	}
}
