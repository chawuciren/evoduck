package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/internal/skill"
	"github.com/chawuciren/evoduck/internal/tools"
	"github.com/chawuciren/evoduck/pkg/models"
)

type staticTool struct {
	name string
}

func (t *staticTool) Name() string { return t.name }

func (t *staticTool) Description() string { return "test tool" }

func (t *staticTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
	}
}

func (t *staticTool) Execute(args map[string]interface{}) (string, error) {
	return fmt.Sprintf("ok:%s", t.name), nil
}

type scriptedStreamProvider struct {
	mu    sync.Mutex
	calls [][]models.Message
	step  int
}

type taskPlanStreamProvider struct {
	step int
}

type summaryToolPairProvider struct {
	mu    sync.Mutex
	calls [][]models.Message
	step  int
}

type earlyCompletionProvider struct {
	mu    sync.Mutex
	calls [][]models.Message
	step  int
}

func (p *scriptedStreamProvider) Name() string { return "scripted" }

func (p *scriptedStreamProvider) Chat(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (*models.Response, error) {
	return nil, fmt.Errorf("unexpected Chat call")
}

func (p *scriptedStreamProvider) ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, _ llm.ChatOptions) (*models.Response, error) {
	return p.Chat(ctx, messages, tools)
}

func (p *scriptedStreamProvider) ChatStream(_ context.Context, messages []models.Message, _ []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	p.mu.Lock()
	p.calls = append(p.calls, append([]models.Message(nil), messages...))
	step := p.step
	p.step++
	p.mu.Unlock()

	ch := make(chan models.StreamEvent, 4)
	if step == 0 {
		ch <- models.StreamEvent{Type: "tool_calls", ToolCalls: []models.ToolCall{
			{ID: "call_a", Type: "function", Function: models.ToolCallFunction{Name: "alpha", Arguments: `{}`}},
			{ID: "call_b", Type: "function", Function: models.ToolCallFunction{Name: "beta", Arguments: `{}`}},
		}}
		close(ch)
		return ch, nil
	}
	ch <- models.StreamEvent{Type: "content", Content: "done"}
	ch <- models.StreamEvent{Type: "stop"}
	close(ch)
	return ch, nil
}

func (p *scriptedStreamProvider) SetDefaultOptions(_ llm.ChatOptions) {}
func (p *scriptedStreamProvider) GetMaxContextTokens() int            { return 8192 }
func (p *scriptedStreamProvider) BuiltinModels() []llm.ProviderModel  { return nil }
func (p *scriptedStreamProvider) FetchModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}
func (p *scriptedStreamProvider) ListModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}

func (p *taskPlanStreamProvider) Name() string { return "task-plan-scripted" }

func (p *taskPlanStreamProvider) Chat(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (*models.Response, error) {
	return nil, fmt.Errorf("unexpected Chat call")
}

func (p *taskPlanStreamProvider) ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, _ llm.ChatOptions) (*models.Response, error) {
	return p.Chat(ctx, messages, tools)
}

func (p *taskPlanStreamProvider) ChatStream(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	ch := make(chan models.StreamEvent, 4)
	if p.step == 0 {
		p.step++
		ch <- models.StreamEvent{Type: "tool_calls", ToolCalls: []models.ToolCall{{
			ID:   "call_plan",
			Type: "function",
			Function: models.ToolCallFunction{
				Name:      "task_plan",
				Arguments: `{"intent":"Test plan event","sub_tasks":[{"name":"Plan step","description":"Verify plan update event","type":"short","status":"running"}]}`,
			},
		}}}
		close(ch)
		return ch, nil
	}
	ch <- models.StreamEvent{Type: "content", Content: "done"}
	ch <- models.StreamEvent{Type: "stop"}
	close(ch)
	return ch, nil
}

func (p *taskPlanStreamProvider) SetDefaultOptions(_ llm.ChatOptions) {}
func (p *taskPlanStreamProvider) GetMaxContextTokens() int            { return 8192 }
func (p *taskPlanStreamProvider) BuiltinModels() []llm.ProviderModel  { return nil }
func (p *taskPlanStreamProvider) FetchModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}
func (p *taskPlanStreamProvider) ListModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}

func (p *summaryToolPairProvider) Name() string { return "summary-tool-pair" }

func (p *summaryToolPairProvider) Chat(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (*models.Response, error) {
	return nil, fmt.Errorf("unexpected Chat call")
}

func (p *summaryToolPairProvider) ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, _ llm.ChatOptions) (*models.Response, error) {
	return p.Chat(ctx, messages, tools)
}

func (p *summaryToolPairProvider) ChatStream(_ context.Context, messages []models.Message, _ []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	p.mu.Lock()
	p.calls = append(p.calls, append([]models.Message(nil), messages...))
	step := p.step
	p.step++
	p.mu.Unlock()

	ch := make(chan models.StreamEvent, 4)
	if step == 0 {
		ch <- models.StreamEvent{Type: "tool_calls", ToolCalls: []models.ToolCall{{
			ID:   "call_summary",
			Type: "function",
			Function: models.ToolCallFunction{
				Name:      "alpha",
				Arguments: `{}`,
			},
		}}}
		close(ch)
		return ch, nil
	}
	ch <- models.StreamEvent{Type: "content", Content: "summary done"}
	ch <- models.StreamEvent{Type: "stop"}
	close(ch)
	return ch, nil
}

func (p *summaryToolPairProvider) SetDefaultOptions(_ llm.ChatOptions) {}
func (p *summaryToolPairProvider) GetMaxContextTokens() int            { return 8192 }
func (p *summaryToolPairProvider) BuiltinModels() []llm.ProviderModel  { return nil }
func (p *summaryToolPairProvider) FetchModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}
func (p *summaryToolPairProvider) ListModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}

func (p *earlyCompletionProvider) Name() string { return "early-completion-provider" }

func (p *earlyCompletionProvider) Chat(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (*models.Response, error) {
	return nil, fmt.Errorf("unexpected Chat call")
}

func (p *earlyCompletionProvider) ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, _ llm.ChatOptions) (*models.Response, error) {
	return p.Chat(ctx, messages, tools)
}

func (p *earlyCompletionProvider) ChatStream(_ context.Context, messages []models.Message, tools []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	p.mu.Lock()
	p.calls = append(p.calls, append([]models.Message(nil), messages...))
	step := p.step
	p.step++
	p.mu.Unlock()

	ch := make(chan models.StreamEvent, 4)
	if step == 0 {
		ch <- models.StreamEvent{Type: "tool_calls", ToolCalls: []models.ToolCall{{
			ID:   "call_plan_done",
			Type: "function",
			Function: models.ToolCallFunction{
				Name:      "task_plan",
				Arguments: `{"intent":"Investigate and report","sub_tasks":[{"name":"Search code","description":"Scope the issue","type":"search","status":"done"},{"name":"Summarize findings","description":"Report the result to the user","type":"chat","status":"done"}]}`,
			},
		}}}
		close(ch)
		return ch, nil
	}

	if tools != nil {
		close(ch)
		return ch, nil
	}

	ch <- models.StreamEvent{Type: "content", Content: "final wrapped answer"}
	ch <- models.StreamEvent{Type: "stop"}
	close(ch)
	return ch, nil
}

func (p *earlyCompletionProvider) SetDefaultOptions(_ llm.ChatOptions) {}
func (p *earlyCompletionProvider) GetMaxContextTokens() int            { return 8192 }
func (p *earlyCompletionProvider) BuiltinModels() []llm.ProviderModel  { return nil }
func (p *earlyCompletionProvider) FetchModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}
func (p *earlyCompletionProvider) ListModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}

func TestRunStreamWithLoopKeepsToolCallsGroupedPerAssistantTurn(t *testing.T) {
	tempDir := t.TempDir()
	toolReg := tools.NewRegistry()
	toolReg.Register(&staticTool{name: "alpha"})
	toolReg.Register(&staticTool{name: "beta"})

	provider := &scriptedStreamProvider{}
	promptBuilder := NewPromptBuilder(tempDir, "agent-stream-test", tempDir, toolReg, skill.NewLoader(tempDir, tempDir))
	runtime := NewRuntime("agent-stream-test", tempDir, provider, toolReg, promptBuilder, models.RoleAdmin, nil, true, nil)
	sess := session.NewSession("webchat:test-user", "stream-tool-session", nil)

	stream, err := runtime.RunStreamWithLoop(context.Background(), sess, "run tools", models.StreamConfig{MaxIterations: 3})
	if err != nil {
		t.Fatalf("run stream with loop: %v", err)
	}
	for range stream {
	}

	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.calls) != 2 {
		t.Fatalf("expected 2 provider calls, got %d", len(provider.calls))
	}

	secondCall := provider.calls[1]
	assistantIndex := -1
	toolIndexes := make([]int, 0, 2)
	for i, msg := range secondCall {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			assistantIndex = i
		}
		if msg.Role == "tool" {
			toolIndexes = append(toolIndexes, i)
		}
	}

	if assistantIndex == -1 {
		t.Fatal("expected second provider call to include assistant tool call message")
	}
	if len(secondCall[assistantIndex].ToolCalls) != 2 {
		t.Fatalf("expected grouped assistant message to contain 2 tool calls, got %d", len(secondCall[assistantIndex].ToolCalls))
	}
	if len(toolIndexes) != 2 {
		t.Fatalf("expected 2 tool result messages, got %d", len(toolIndexes))
	}
	if !(assistantIndex < toolIndexes[0] && toolIndexes[0] < toolIndexes[1]) {
		t.Fatalf("expected assistant tool call message before tool results, got assistant=%d toolIndexes=%v", assistantIndex, toolIndexes)
	}
	if secondCall[toolIndexes[0]].ToolCallID != "call_a" || secondCall[toolIndexes[1]].ToolCallID != "call_b" {
		t.Fatalf("unexpected tool call ids in follow-up request: %q, %q", secondCall[toolIndexes[0]].ToolCallID, secondCall[toolIndexes[1]].ToolCallID)
	}
}

func TestRunStreamWithLoopEmitsPlanUpdateForTaskPlanTool(t *testing.T) {
	tempDir := t.TempDir()
	toolReg := tools.NewRegistry()
	promptBuilder := NewPromptBuilder(tempDir, "agent-stream-plan-test", tempDir, toolReg, skill.NewLoader(tempDir, tempDir))
	runtime := NewRuntime("agent-stream-plan-test", tempDir, &taskPlanStreamProvider{}, toolReg, promptBuilder, models.RoleAdmin, nil, true, nil)
	toolReg.Register(tools.NewTaskPlanTool(runtime))
	sess := session.NewSession("webchat:test-user", "stream-plan-session", nil)

	stream, err := runtime.RunStreamWithLoop(context.Background(), sess, "plan something", models.StreamConfig{MaxIterations: 3})
	if err != nil {
		t.Fatalf("run stream with loop: %v", err)
	}

	var sawPlanUpdate bool
	for event := range stream {
		if event.Type == "plan_update" {
			sawPlanUpdate = true
			if event.Plan == nil || event.Plan.Intent != "Test plan event" {
				t.Fatalf("unexpected plan update payload: %+v", event.Plan)
			}
		}
	}

	if !sawPlanUpdate {
		t.Fatal("expected stream to emit plan_update event")
	}
}

func TestRunStreamWithLoopSummaryPreservesAssistantToolCallPairs(t *testing.T) {
	tempDir := t.TempDir()
	toolReg := tools.NewRegistry()
	toolReg.Register(&staticTool{name: "alpha"})
	provider := &summaryToolPairProvider{}
	promptBuilder := NewPromptBuilder(tempDir, "agent-stream-summary-test", tempDir, toolReg, skill.NewLoader(tempDir, tempDir))
	runtime := NewRuntime("agent-stream-summary-test", tempDir, provider, toolReg, promptBuilder, models.RoleAdmin, nil, true, nil)
	sess := session.NewSession("webchat:test-user", "stream-summary-session", nil)

	stream, err := runtime.RunStreamWithLoop(context.Background(), sess, "summarize after tool", models.StreamConfig{MaxIterations: 1})
	if err != nil {
		t.Fatalf("run stream with loop: %v", err)
	}
	for range stream {
	}

	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.calls) != 2 {
		t.Fatalf("expected 2 provider calls, got %d", len(provider.calls))
	}

	summaryCall := provider.calls[1]
	assistantIndex := -1
	toolIndex := -1
	for i, msg := range summaryCall {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			assistantIndex = i
		}
		if msg.Role == "tool" && msg.ToolCallID == "call_summary" {
			toolIndex = i
		}
	}

	if assistantIndex == -1 {
		t.Fatal("expected summary provider call to preserve assistant tool call message")
	}
	if toolIndex == -1 {
		t.Fatal("expected summary provider call to preserve tool result message")
	}
	if assistantIndex >= toolIndex {
		t.Fatalf("expected assistant tool call message before tool result in summary call, got assistant=%d tool=%d", assistantIndex, toolIndex)
	}
	if len(summaryCall[assistantIndex].ToolCalls) != 1 || summaryCall[assistantIndex].ToolCalls[0].ID != "call_summary" {
		t.Fatalf("unexpected tool calls in summary assistant message: %+v", summaryCall[assistantIndex].ToolCalls)
	}
}

func TestRunStreamWithLoopEarlyCompletionTriggersFinalResponse(t *testing.T) {
	tempDir := t.TempDir()
	toolReg := tools.NewRegistry()
	provider := &earlyCompletionProvider{}
	promptBuilder := NewPromptBuilder(tempDir, "agent-stream-early-stop-test", tempDir, toolReg, skill.NewLoader(tempDir, tempDir))
	runtime := NewRuntime("agent-stream-early-stop-test", tempDir, provider, toolReg, promptBuilder, models.RoleAdmin, nil, true, nil)
	toolReg.Register(tools.NewTaskPlanTool(runtime))
	sess := session.NewSession("webchat:test-user", "stream-early-stop-session", nil)

	stream, err := runtime.RunStreamWithLoop(context.Background(), sess, "finish and reply", models.StreamConfig{MaxIterations: 3})
	if err != nil {
		t.Fatalf("run stream with loop: %v", err)
	}

	var contentChunks []string
	for event := range stream {
		if event.Type == "content" {
			contentChunks = append(contentChunks, event.Content)
		}
	}

	if len(contentChunks) == 0 || contentChunks[len(contentChunks)-1] != "final wrapped answer" {
		t.Fatalf("expected final wrapped answer content, got %v", contentChunks)
	}

	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.calls) != 2 {
		t.Fatalf("expected 2 provider calls, got %d", len(provider.calls))
	}

	finalCall := provider.calls[1]
	last := finalCall[len(finalCall)-1]
	if last.Role != "user" || !strings.Contains(last.Content, "你已经完成了当前任务计划") {
		t.Fatalf("expected early completion finalization instruction, got role=%q content=%q", last.Role, last.Content)
	}

	messages := sess.GetMessages()
	if len(messages) == 0 || messages[len(messages)-1].Content != "final wrapped answer" {
		t.Fatalf("expected session to persist final wrapped answer, got last=%+v", messages[len(messages)-1])
	}
}

type thinkingStreamProvider struct{}

func (p *thinkingStreamProvider) Name() string { return "thinking-stream-provider" }

func (p *thinkingStreamProvider) Chat(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (*models.Response, error) {
	return nil, fmt.Errorf("unexpected Chat call")
}

func (p *thinkingStreamProvider) ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, _ llm.ChatOptions) (*models.Response, error) {
	return p.Chat(ctx, messages, tools)
}

func (p *thinkingStreamProvider) ChatStream(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	ch := make(chan models.StreamEvent, 4)
	ch <- models.StreamEvent{Type: "thinking", ThinkingContent: "internal reasoning"}
	ch <- models.StreamEvent{Type: "content", Content: "visible answer"}
	ch <- models.StreamEvent{Type: "stop"}
	close(ch)
	return ch, nil
}

func (p *thinkingStreamProvider) SetDefaultOptions(_ llm.ChatOptions) {}
func (p *thinkingStreamProvider) GetMaxContextTokens() int            { return 8192 }
func (p *thinkingStreamProvider) BuiltinModels() []llm.ProviderModel  { return nil }
func (p *thinkingStreamProvider) FetchModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}
func (p *thinkingStreamProvider) ListModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}

func TestRunStreamWithLoopPersistsThinkingContent(t *testing.T) {
	tempDir := t.TempDir()
	toolReg := tools.NewRegistry()
	promptBuilder := NewPromptBuilder(tempDir, "agent-stream-thinking-test", tempDir, toolReg, skill.NewLoader(tempDir, tempDir))
	runtime := NewRuntime("agent-stream-thinking-test", tempDir, &thinkingStreamProvider{}, toolReg, promptBuilder, models.RoleAdmin, nil, true, nil)
	sess := session.NewSession("webchat:test-user", "stream-thinking-session", nil)

	stream, err := runtime.RunStreamWithLoop(context.Background(), sess, "think then answer", models.StreamConfig{MaxIterations: 1})
	if err != nil {
		t.Fatalf("run stream with loop: %v", err)
	}
	for range stream {
	}

	messages := sess.GetMessages()
	if len(messages) == 0 {
		t.Fatal("expected persisted assistant message")
	}
	last := messages[len(messages)-1]
	if last.Role != "assistant" {
		t.Fatalf("expected last message role assistant, got %q", last.Role)
	}
	if last.Content != "visible answer" {
		t.Fatalf("expected persisted content, got %q", last.Content)
	}
	if last.ThinkingContent != "internal reasoning" {
		t.Fatalf("expected persisted thinking content, got %q", last.ThinkingContent)
	}
}

func TestBuildFinalizationMessagesPreservesReasoningReplay(t *testing.T) {
	sess := session.NewSession("webchat:test-user", "summary-reasoning-session", nil)
	sess.Append(models.Message{Role: "user", Content: "hello"})
	sess.Append(models.Message{
		Role:            "assistant",
		Content:         "let me think and call tools",
		ThinkingContent: "hidden chain",
		ReasoningMetadata: &models.ReasoningReplay{
			Provider: "openai_responses",
			OpenAIResponses: &models.OpenAIResponsesReasoningReplay{ItemID: "rs_123", Summary: []string{"hidden chain"}},
		},
		ToolCalls: []models.ToolCall{{
			ID:   "call_1",
			Type: "function",
			Function: models.ToolCallFunction{Name: "search", Arguments: `{"q":"x"}`},
		}},
	})

	msgs := buildFinalizationMessages(sess, "finalize")
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	assistant := msgs[1]
	if assistant.Role != "assistant" {
		t.Fatalf("expected assistant message, got %+v", assistant)
	}
	if assistant.ThinkingContent != "hidden chain" {
		t.Fatalf("expected thinking content to be preserved, got %q", assistant.ThinkingContent)
	}
	if assistant.ReasoningMetadata == nil || assistant.ReasoningMetadata.Provider != "openai_responses" {
		t.Fatalf("expected reasoning metadata to be preserved, got %+v", assistant.ReasoningMetadata)
	}
	if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].ID != "call_1" {
		t.Fatalf("expected tool calls to be preserved, got %+v", assistant.ToolCalls)
	}
}
