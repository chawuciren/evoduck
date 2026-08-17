package session

import (
	"testing"

	"github.com/chawuciren/evoduck/pkg/models"
)

// qwen 流式 tool_call id 每轮从 call_0 重新编号。
// 新一轮同 id 的 tool result 不能被当作重放丢弃。
func TestAppendKeepsToolResultWithReusedID(t *testing.T) {
	s := NewSession("k", "k", nil)
	s.Append(models.Message{Role: "user", Content: "hi"})
	s.Append(models.Message{Role: "assistant", Content: "a", ToolCalls: []models.ToolCall{
		{ID: "call_0", Type: "function", Function: models.ToolCallFunction{Name: "exec"}},
	}})
	s.Append(models.Message{Role: "tool", ToolCallID: "call_0", Content: "r1"})

	// 第二轮：assistant 再次发出 call_0（qwen 重置编号）
	s.Append(models.Message{Role: "assistant", Content: "b", ToolCalls: []models.ToolCall{
		{ID: "call_0", Type: "function", Function: models.ToolCallFunction{Name: "exec"}},
	}})
	s.Append(models.Message{Role: "tool", ToolCallID: "call_0", Content: "r2"})

	msgs := s.GetMessages()
	toolCount := 0
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolCallID == "call_0" {
			toolCount++
		}
	}
	if toolCount != 2 {
		t.Fatalf("expected 2 tool results for reused call_0, got %d", toolCount)
	}
}

// 同一条 assistant 的 tool result 真重放时应被去重。
func TestAppendDropsTrueReplay(t *testing.T) {
	s := NewSession("k", "k", nil)
	s.Append(models.Message{Role: "user", Content: "hi"})
	s.Append(models.Message{Role: "assistant", Content: "a", ToolCalls: []models.ToolCall{
		{ID: "call_9", Type: "function", Function: models.ToolCallFunction{Name: "exec"}},
	}})
	s.Append(models.Message{Role: "tool", ToolCallID: "call_9", Content: "r1"})
	// 重放同一 result
	s.Append(models.Message{Role: "tool", ToolCallID: "call_9", Content: "r1"})

	msgs := s.GetMessages()
	toolCount := 0
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolCallID == "call_9" {
			toolCount++
		}
	}
	if toolCount != 1 {
		t.Fatalf("expected dedup to keep 1 tool result, got %d", toolCount)
	}
}
