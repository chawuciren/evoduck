package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
)

func TestNormalizeCompatibleRoleMapsDeveloperToSystem(t *testing.T) {
	got := normalizeCompatibleRole("developer", "openai-compatible")
	if got != "system" {
		t.Fatalf("expected developer role to downgrade to system, got %q", got)
	}
}

func TestOpenAICompatibleConvertMovesMidStreamSystemToFront(t *testing.T) {
	provider, err := NewOpenAICompatibleProvider("test", config.ProviderConfig{
		Type:         "openai-compatible",
		BaseURL:      "https://example.com",
		DefaultModel: "test-model",
	}, nil)
	if err != nil {
		t.Fatalf("NewOpenAICompatibleProvider() error = %v", err)
	}

	// developer 角色提醒出现在对话中途，经 normalizeCompatibleRole 变为 system，
	// 若不上提会触发 Ollama "system message must be at the beginning" 500 错误。
	converted, err := provider.(*OpenAICompatibleProvider).convertMessages([]models.Message{
		{Role: "system", Content: "base prompt"},
		{Role: "user", Content: "hello"},
		{Role: "developer", Content: "[Task Planning Suggestion] consider task_plan"},
		{Role: "assistant", Content: "ok"},
	})
	if err != nil {
		t.Fatalf("convertMessages() error = %v", err)
	}
	if len(converted) != 3 {
		t.Fatalf("expected 3 messages after merge, got %d: %+v", len(converted), converted)
	}
	if converted[0].Role != "system" {
		t.Fatalf("expected first message to be system, got %q", converted[0].Role)
	}
	if text, _ := converted[0].Content.(string); !strings.Contains(text, "base prompt") || !strings.Contains(text, "Task Planning Suggestion") {
		t.Fatalf("expected merged system content, got %q", text)
	}
	for _, msg := range converted[1:] {
		if msg.Role == "system" {
			t.Fatalf("expected no mid-stream system messages, got %+v", converted)
		}
	}
}

func TestOpenAICompatibleConvertWithoutLeadingSystemKeepsInsertionOrder(t *testing.T) {
	provider, err := NewOpenAICompatibleProvider("test", config.ProviderConfig{
		Type:         "openai-compatible",
		BaseURL:      "https://example.com",
		DefaultModel: "test-model",
	}, nil)
	if err != nil {
		t.Fatalf("NewOpenAICompatibleProvider() error = %v", err)
	}

	converted, err := provider.(*OpenAICompatibleProvider).convertMessages([]models.Message{
		{Role: "user", Content: "hello"},
		{Role: "developer", Content: "reminder"},
		{Role: "assistant", Content: "ok"},
	})
	if err != nil {
		t.Fatalf("convertMessages() error = %v", err)
	}
	if len(converted) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(converted))
	}
	if converted[0].Role != "system" {
		t.Fatalf("expected system at front, got %q", converted[0].Role)
	}
	if converted[1].Role != "user" || converted[2].Role != "assistant" {
		t.Fatalf("expected user then assistant after system, got %+v", converted)
	}
}

func TestOpenAICompatibleConvertAppendsUserFallbackWhenMissing(t *testing.T) {
	provider, err := NewOpenAICompatibleProvider("test", config.ProviderConfig{
		Type:         "openai-compatible",
		BaseURL:      "https://example.com",
		DefaultModel: "test-model",
	}, nil)
	if err != nil {
		t.Fatalf("NewOpenAICompatibleProvider() error = %v", err)
	}

	// 无任何 user 消息的序列（curation/摘要等内部运行可能出现），
	// qwen chat template 会报 "no user query found in messages" 500。
	// 期望末尾追加一条 user 兜底。
	converted, err := provider.(*OpenAICompatibleProvider).convertMessages([]models.Message{
		{Role: "system", Content: "curator prompt"},
		{Role: "assistant", Content: "checking", ToolCalls: []models.ToolCall{{
			ID: "c1", Type: "function", Function: models.ToolCallFunction{Name: "memory_read", Arguments: "{}"},
		}}},
		{Role: "tool", ToolCallID: "c1", Content: "result"},
	})
	if err != nil {
		t.Fatalf("convertMessages() error = %v", err)
	}
	last := converted[len(converted)-1]
	if last.Role != "user" || strings.TrimSpace(last.Content.(string)) == "" {
		t.Fatalf("expected trailing user fallback, got %+v", last)
	}
	userCount := 0
	for _, msg := range converted {
		if msg.Role == "user" {
			userCount++
		}
	}
	if userCount != 1 {
		t.Fatalf("expected exactly one user message, got %d", userCount)
	}
}

func TestOpenAICompatibleConvertKeepsExistingUserMessages(t *testing.T) {
	provider, err := NewOpenAICompatibleProvider("test", config.ProviderConfig{
		Type:         "openai-compatible",
		BaseURL:      "https://example.com",
		DefaultModel: "test-model",
	}, nil)
	if err != nil {
		t.Fatalf("NewOpenAICompatibleProvider() error = %v", err)
	}

	converted, err := provider.(*OpenAICompatibleProvider).convertMessages([]models.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	})
	if err != nil {
		t.Fatalf("convertMessages() error = %v", err)
	}
	if len(converted) != 3 {
		t.Fatalf("expected no extra user message, got %d: %+v", len(converted), converted)
	}
}

func TestOpenAICompatibleChatStreamRetriesTransientStatus(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"upstream error"}}`))
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider, err := NewOpenAICompatibleProvider("test", config.ProviderConfig{
		Type:         "openai-compatible",
		BaseURL:      server.URL,
		APIKey:       "test-key",
		DefaultModel: "test-model",
	}, nil)
	if err != nil {
		t.Fatalf("NewOpenAICompatibleProvider() error = %v", err)
	}

	streamCh, err := provider.ChatStream(context.Background(), []models.Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}

	var events []models.StreamEvent
	for event := range streamCh {
		events = append(events, event)
	}

	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
	if len(events) != 1 || events[0].Type != "content" || events[0].Content != "hello" {
		t.Fatalf("unexpected events: %#v", events)
	}
}

func TestOpenAICompatibleChatStreamDoesNotRetryClientStatus(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request"}}`))
	}))
	defer server.Close()

	provider, err := NewOpenAICompatibleProvider("test", config.ProviderConfig{
		Type:         "openai-compatible",
		BaseURL:      server.URL,
		APIKey:       "test-key",
		DefaultModel: "test-model",
	}, nil)
	if err != nil {
		t.Fatalf("NewOpenAICompatibleProvider() error = %v", err)
	}

	_, err = provider.ChatStream(context.Background(), []models.Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected ChatStream() error")
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", attempts)
	}
}

func TestOpenAICompatibleConvertMessagesIncludesReasoningContent(t *testing.T) {
	provider, err := NewOpenAICompatibleProvider("test", config.ProviderConfig{
		Type:         "openai-compatible",
		BaseURL:      "https://example.com",
		DefaultModel: "test-model",
	}, nil)
	if err != nil {
		t.Fatalf("NewOpenAICompatibleProvider() error = %v", err)
	}

	compatible, ok := provider.(*OpenAICompatibleProvider)
	if !ok {
		t.Fatalf("expected *OpenAICompatibleProvider, got %T", provider)
	}

	messages, err := compatible.convertMessages([]models.Message{{
		Role:            "assistant",
		ThinkingContent: "step by step",
	}})
	if err != nil {
		t.Fatalf("convertMessages() error = %v", err)
	}
	// 末尾会追加 user 兜底（qwen 模板要求至少一条 user query），共 2 条。
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0].ReasoningContent != "step by step" {
		t.Fatalf("expected reasoning content to be preserved, got %q", messages[0].ReasoningContent)
	}
	if messages[0].Content == nil || messages[0].Content != " " {
		t.Fatalf("expected placeholder content for assistant message with only reasoning, got %#v", messages[0].Content)
	}

	payload, err := json.Marshal(messages[0])
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}
	expectedPayload := `{"role":"assistant","content":" ","reasoning_content":"step by step"}`
	if string(payload) != expectedPayload {
		t.Fatalf("unexpected json payload: %s, expected: %s", payload, expectedPayload)
	}
}

func TestOpenAICompatibleConvertMessagesIncludesImageParts(t *testing.T) {
	provider, err := NewOpenAICompatibleProvider("test", config.ProviderConfig{
		Type:         "openai-compatible",
		BaseURL:      "https://example.com",
		DefaultModel: "test-model",
	}, nil)
	if err != nil {
		t.Fatalf("NewOpenAICompatibleProvider() error = %v", err)
	}

	file := t.TempDir() + "/image.png"
	if err := os.WriteFile(file, []byte("png-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	converted, err := provider.(*OpenAICompatibleProvider).convertMessages([]models.Message{{
		Role:    "user",
		Content: "look",
		Media:   []models.OutgoingMedia{{Name: "image.png", MimeType: "image/png", Path: file}},
	}})
	if err != nil {
		t.Fatalf("convertMessages() error = %v", err)
	}
	parts, ok := converted[0].Content.([]map[string]any)
	if !ok {
		t.Fatalf("expected multimodal content parts, got %#v", converted[0].Content)
	}
	if len(parts) != 2 {
		t.Fatalf("expected text and image parts, got %#v", parts)
	}
	if parts[0]["type"] != "text" || parts[0]["text"] != "look" {
		t.Fatalf("unexpected text part: %#v", parts[0])
	}
	imagePart, ok := parts[1]["image_url"].(map[string]any)
	if !ok || parts[1]["type"] != "image_url" {
		t.Fatalf("unexpected image part: %#v", parts[1])
	}
	url, _ := imagePart["url"].(string)
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatalf("expected data url, got %#v", imagePart)
	}
}

func TestOpenAICompatibleConvertMessagesKeepsToolMessagesTextOnly(t *testing.T) {
	provider, err := NewOpenAICompatibleProvider("test", config.ProviderConfig{
		Type:         "openai-compatible",
		BaseURL:      "https://example.com",
		DefaultModel: "test-model",
	}, nil)
	if err != nil {
		t.Fatalf("NewOpenAICompatibleProvider() error = %v", err)
	}

	file := t.TempDir() + "/image.png"
	if err := os.WriteFile(file, []byte("png-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	converted, err := provider.(*OpenAICompatibleProvider).convertMessages([]models.Message{
		{Role: "assistant", ToolCalls: []models.ToolCall{{ID: "call_1", Type: "function", Function: models.ToolCallFunction{Name: "browser_screenshot", Arguments: `{}`}}}},
		{Role: "tool", ToolCallID: "call_1", Content: "Screenshot captured", Media: []models.OutgoingMedia{{Name: "image.png", MimeType: "image/png", Path: file}}},
	})
	if err != nil {
		t.Fatalf("convertMessages() error = %v", err)
	}
	// 末尾会追加 user 兜底（qwen 模板要求至少一条 user query），共 3 条。
	if len(converted) != 3 {
		t.Fatalf("expected assistant, tool and user fallback, got %d", len(converted))
	}
	if text, ok := converted[1].Content.(string); !ok || text != "Screenshot captured" {
		t.Fatalf("expected text-only tool content, got %#v", converted[1].Content)
	}
}

func TestAppendCompatibleMessageToResponseIncludesTopLevelReasoningContent(t *testing.T) {
	result := &models.Response{}
	appendCompatibleMessageToResponse(result, openAIChatMessage{
		Role:             "assistant",
		ReasoningContent: "hidden chain",
		ToolCalls: []openAIChatToolCall{{
			ID:   "call_1",
			Type: "function",
			Function: openAIChatToolFunction{
				Name:      "search",
				Arguments: `{"q":"hello"}`,
			},
		}},
	})

	if result.ReasoningContent != "hidden chain" {
		t.Fatalf("expected reasoning content to be preserved, got %q", result.ReasoningContent)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].ID != "call_1" {
		t.Fatalf("expected tool call to be preserved, got %+v", result.ToolCalls)
	}
}

func TestDeepSeekProviderOnlyIncludesReasoningContentForToolCalls(t *testing.T) {
	provider, err := NewDeepSeekProvider("deepseek", config.ProviderConfig{
		Type:         "deepseek",
		BaseURL:      "https://api.deepseek.com/v1",
		APIKey:       "test-key",
		DefaultModel: "deepseek-chat",
	}, nil)
	if err != nil {
		t.Fatalf("NewOpenAICompatibleProvider() error = %v", err)
	}

	msgs := []models.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "response", ThinkingContent: "deep thought"},
		{Role: "assistant", ThinkingContent: "tool thought", ToolCalls: []models.ToolCall{{
			ID: "call_1", Type: "function", Function: models.ToolCallFunction{Name: "search", Arguments: `{}`},
		}}},
		{Role: "tool", ToolCallID: "call_1", Content: "result"},
	}

	converted, err := provider.(*DeepSeekProvider).adapter.ConvertMessages(provider.(*DeepSeekProvider).OpenAICompatibleProvider, msgs)
	if err != nil {
		t.Fatalf("convertMessages() error = %v", err)
	}

	if len(converted) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(converted))
	}

	if converted[1].ReasoningContent != "" {
		t.Fatalf("expected non-tool reasoning content to be stripped for DeepSeek, got %q", converted[1].ReasoningContent)
	}
	if converted[1].Content != "response" {
		t.Fatalf("expected content to be preserved, got %q", converted[1].Content)
	}
	if converted[2].ReasoningContent != "tool thought" {
		t.Fatalf("expected tool-call reasoning content to be preserved for DeepSeek, got %q", converted[2].ReasoningContent)
	}
}

func TestOpenAICompatibleConvertMessagesDropsToolCallsWithoutResults(t *testing.T) {
	provider, err := NewOpenAICompatibleProvider("test", config.ProviderConfig{
		Type:         "openai-compatible",
		BaseURL:      "https://example.com",
		DefaultModel: "test-model",
	}, nil)
	if err != nil {
		t.Fatalf("NewOpenAICompatibleProvider() error = %v", err)
	}

	converted, err := provider.(*OpenAICompatibleProvider).convertMessages([]models.Message{
		{Role: "user", Content: "start"},
		{Role: "assistant", ToolCalls: []models.ToolCall{
			{ID: "call_a", Type: "function", Function: models.ToolCallFunction{Name: "search", Arguments: `{}`}},
			{ID: "call_b", Type: "function", Function: models.ToolCallFunction{Name: "read", Arguments: `{}`}},
		}},
		{Role: "tool", ToolCallID: "call_a", Content: "result a"},
		{Role: "user", Content: "continue"},
	})
	if err != nil {
		t.Fatalf("convertMessages() error = %v", err)
	}

	var assistant openAIChatMessage
	for _, msg := range converted {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			assistant = msg
			break
		}
	}
	if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].ID != "call_a" {
		t.Fatalf("expected only call_a to remain, got %+v", assistant.ToolCalls)
	}
	for _, msg := range converted {
		if msg.Role == "tool" && msg.ToolCallID == "call_b" {
			t.Fatalf("unexpected orphaned tool message for call_b")
		}
	}
}

func TestOpenAICompatibleConvertMessagesDropsOrphanToolMessages(t *testing.T) {
	provider, err := NewOpenAICompatibleProvider("test", config.ProviderConfig{
		Type:         "openai-compatible",
		BaseURL:      "https://example.com",
		DefaultModel: "test-model",
	}, nil)
	if err != nil {
		t.Fatalf("NewOpenAICompatibleProvider() error = %v", err)
	}

	converted, err := provider.(*OpenAICompatibleProvider).convertMessages([]models.Message{
		{Role: "user", Content: "start"},
		{Role: "tool", ToolCallID: "orphan", Content: "orphan result"},
		{Role: "assistant", Content: "done"},
	})
	if err != nil {
		t.Fatalf("convertMessages() error = %v", err)
	}
	for _, msg := range converted {
		if msg.Role == "tool" {
			t.Fatalf("expected orphaned tool message to be dropped, got %+v", msg)
		}
	}
}

func TestOpenAICompatibleConvertMessagesFiltersSyntheticToolCalls(t *testing.T) {
	provider, err := NewOpenAICompatibleProvider("test", config.ProviderConfig{
		Type:         "openai-compatible",
		BaseURL:      "https://example.com",
		DefaultModel: "test-model",
	}, nil)
	if err != nil {
		t.Fatalf("NewOpenAICompatibleProvider() error = %v", err)
	}

	converted, err := provider.(*OpenAICompatibleProvider).convertMessages([]models.Message{
		{Role: "user", Content: "start"},
		{Role: "assistant", ToolCalls: []models.ToolCall{
			{ID: "runtime_task_plan_reminder_1", Type: "function", Function: models.ToolCallFunction{Name: "task_plan", Arguments: `{}`}},
			{ID: "call_real", Type: "function", Function: models.ToolCallFunction{Name: "search", Arguments: `{}`}},
		}},
		{Role: "tool", ToolCallID: "runtime_task_plan_reminder_1", Content: "synthetic"},
		{Role: "tool", ToolCallID: "call_real", Content: "real result"},
	})
	if err != nil {
		t.Fatalf("convertMessages() error = %v", err)
	}

	var assistant openAIChatMessage
	for _, msg := range converted {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			assistant = msg
			break
		}
	}
	if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].ID != "call_real" {
		t.Fatalf("expected only real tool call to remain, got %+v", assistant.ToolCalls)
	}
	for _, msg := range converted {
		if msg.Role == "tool" && msg.ToolCallID == "runtime_task_plan_reminder_1" {
			t.Fatalf("expected synthetic tool message to be dropped")
		}
	}
}

func TestDeepSeekBuildRequestAppliesOnlyDeepSeekFields(t *testing.T) {
	parallel := true
	presence := 0.5
	frequency := 0.25
	provider, err := NewDeepSeekProvider("deepseek", config.ProviderConfig{
		Type:              "deepseek",
		BaseURL:           "https://api.deepseek.com",
		DefaultModel:      "deepseek-v4-pro",
		Thinking:          &config.ThinkingConfig{Type: "enabled"},
		ReasoningEffort:   "medium",
		User:              "legacy-user",
		UserID:            "deepseek-user",
		ParallelToolCalls: &parallel,
		PresencePenalty:   &presence,
		FrequencyPenalty:  &frequency,
	}, nil)
	if err != nil {
		t.Fatalf("NewOpenAICompatibleProvider() error = %v", err)
	}

	req, err := provider.(*DeepSeekProvider).buildRequest([]models.Message{{Role: "user", Content: "hi"}}, nil, ChatOptions{}, false)
	if err != nil {
		t.Fatalf("buildRequest() error = %v", err)
	}
	if req.Extra["thinking"] == nil {
		t.Fatalf("expected DeepSeek thinking field, got %#v", req.Extra)
	}
	if req.ReasoningEffort != "high" {
		t.Fatalf("expected DeepSeek reasoning_effort to normalize to high, got %q", req.ReasoningEffort)
	}
	if req.Extra["user_id"] != "deepseek-user" || req.User != "" {
		t.Fatalf("expected DeepSeek user_id only, got user=%q extra=%#v", req.User, req.Extra)
	}
	if req.ParallelToolCalls != nil || req.PresencePenalty != nil || req.FrequencyPenalty != nil {
		t.Fatalf("expected DeepSeek unsupported fields to be omitted, got parallel=%v presence=%v frequency=%v", req.ParallelToolCalls, req.PresencePenalty, req.FrequencyPenalty)
	}
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if !strings.Contains(string(payload), `"thinking":{"type":"enabled"}`) || strings.Contains(string(payload), `"user":`) || !strings.Contains(string(payload), `"user_id":"deepseek-user"`) {
		t.Fatalf("unexpected DeepSeek payload: %s", payload)
	}
}

func TestDeepSeekReasoningReplayPolicyCanDisableReplay(t *testing.T) {
	provider, err := NewDeepSeekProvider("deepseek", config.ProviderConfig{
		Type:            "deepseek",
		BaseURL:         "https://api.deepseek.com",
		DefaultModel:    "deepseek-v4-pro",
		ReasoningReplay: "none",
	}, nil)
	if err != nil {
		t.Fatalf("NewOpenAICompatibleProvider() error = %v", err)
	}

	converted, err := provider.(*DeepSeekProvider).adapter.ConvertMessages(provider.(*DeepSeekProvider).OpenAICompatibleProvider, []models.Message{
		{Role: "assistant", ThinkingContent: "hidden", ToolCalls: []models.ToolCall{{
			ID: "call_1", Type: "function", Function: models.ToolCallFunction{Name: "search", Arguments: `{}`},
		}}},
		{Role: "tool", ToolCallID: "call_1", Content: "result"},
	})
	if err != nil {
		t.Fatalf("convertMessages() error = %v", err)
	}
	if converted[0].ReasoningContent != "" {
		t.Fatalf("expected reasoning replay to be disabled, got %q", converted[0].ReasoningContent)
	}
}

func TestDeepSeekReasoningReplayPolicyCanReplayAll(t *testing.T) {
	provider, err := NewDeepSeekProvider("deepseek", config.ProviderConfig{
		Type:            "deepseek",
		BaseURL:         "https://api.deepseek.com",
		DefaultModel:    "deepseek-v4-pro",
		ReasoningReplay: "all",
	}, nil)
	if err != nil {
		t.Fatalf("NewOpenAICompatibleProvider() error = %v", err)
	}

	converted, err := provider.(*DeepSeekProvider).adapter.ConvertMessages(provider.(*DeepSeekProvider).OpenAICompatibleProvider, []models.Message{{Role: "assistant", Content: "done", ThinkingContent: "hidden"}})
	if err != nil {
		t.Fatalf("convertMessages() error = %v", err)
	}
	if converted[0].ReasoningContent != "hidden" {
		t.Fatalf("expected all reasoning to replay, got %q", converted[0].ReasoningContent)
	}
}

func TestOpenAICompatibleBuildRequestDoesNotApplyDeepSeekFields(t *testing.T) {
	parallel := true
	presence := 0.5
	provider, err := NewOpenAICompatibleProvider("test", config.ProviderConfig{
		Type:              "openai-compatible",
		BaseURL:           "https://api.deepseek.com",
		DefaultModel:      "deepseek-v4-pro",
		Thinking:          &config.ThinkingConfig{Type: "enabled"},
		ReasoningEffort:   "medium",
		UserID:            "plain-user",
		ParallelToolCalls: &parallel,
		PresencePenalty:   &presence,
	}, nil)
	if err != nil {
		t.Fatalf("NewOpenAICompatibleProvider() error = %v", err)
	}

	req, err := provider.(*OpenAICompatibleProvider).buildRequest([]models.Message{{Role: "user", Content: "hi"}}, nil, ChatOptions{}, false)
	if err != nil {
		t.Fatalf("buildRequest() error = %v", err)
	}
	if req.Extra != nil {
		t.Fatalf("expected generic provider not to send DeepSeek extra fields, got %#v", req.Extra)
	}
	if req.ReasoningEffort != "medium" {
		t.Fatalf("expected generic reasoning_effort to remain medium, got %q", req.ReasoningEffort)
	}
	if req.User != "plain-user" {
		t.Fatalf("expected generic provider to use user field, got user=%q", req.User)
	}
	if req.ParallelToolCalls == nil || !*req.ParallelToolCalls || req.PresencePenalty == nil || *req.PresencePenalty != presence {
		t.Fatalf("expected generic provider fields to remain, got parallel=%v presence=%v", req.ParallelToolCalls, req.PresencePenalty)
	}
}

func TestOpenRouterBuildRequestDoesNotApplyDeepSeekFields(t *testing.T) {
	parallel := true
	provider, err := NewOpenAICompatibleProvider("openrouter", config.ProviderConfig{
		Type:              "openrouter",
		BaseURL:           "https://openrouter.ai/api/v1",
		DefaultModel:      "deepseek/deepseek-v4-pro",
		ReasoningEffort:   "medium",
		User:              "openrouter-user",
		ParallelToolCalls: &parallel,
	}, nil)
	if err != nil {
		t.Fatalf("NewOpenAICompatibleProvider() error = %v", err)
	}

	req, err := provider.(*OpenAICompatibleProvider).buildRequest([]models.Message{{Role: "user", Content: "hi"}}, nil, ChatOptions{}, false)
	if err != nil {
		t.Fatalf("buildRequest() error = %v", err)
	}
	if req.Extra != nil || req.User != "openrouter-user" {
		t.Fatalf("expected OpenRouter not to use DeepSeek fields, got extra=%v user=%q", req.Extra, req.User)
	}
	if req.ReasoningEffort != "medium" {
		t.Fatalf("expected OpenRouter reasoning_effort unchanged, got %q", req.ReasoningEffort)
	}
	if req.ParallelToolCalls == nil || !*req.ParallelToolCalls {
		t.Fatalf("expected OpenRouter parallel_tool_calls to remain enabled, got %v", req.ParallelToolCalls)
	}
}

func TestDeepSeekPresetPreservesExplicitProviderType(t *testing.T) {
	cfg, ok := applyPreset("deepseek", config.ProviderConfig{Type: "deepseek"})
	if !ok {
		t.Fatal("expected deepseek preset")
	}
	if cfg.Type != "deepseek" {
		t.Fatalf("expected preset to preserve provider type, got %q", cfg.Type)
	}
	if cfg.DefaultModel != "deepseek-v4-pro" {
		t.Fatalf("expected V4 default model, got %q", cfg.DefaultModel)
	}
}

func TestSummarizeCompatibleMessagesForLogDoesNotExposeReasoningText(t *testing.T) {
	summary := summarizeCompatibleMessagesForLog([]openAIChatMessage{{
		Role:             "assistant",
		ReasoningContent: "private chain of thought",
		ToolCalls:        []openAIChatToolCall{{ID: "call_1", Type: "function"}},
	}})
	if len(summary) != 1 {
		t.Fatalf("expected one summary, got %d", len(summary))
	}
	if _, ok := summary[0]["thinking_preview"]; ok {
		t.Fatalf("reasoning preview should not be present in log summary: %+v", summary[0])
	}
	if summary[0]["thinking_chars"] != len("private chain of thought") {
		t.Fatalf("expected reasoning length only, got %+v", summary[0])
	}
}
