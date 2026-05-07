package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/internal/mediautil"
	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/internal/skill"
	"github.com/chawuciren/evoduck/internal/tools"
	"github.com/chawuciren/evoduck/pkg/models"
)

type deferredReplayProvider struct {
	chatResponses []*models.Response
	chatErrs      []error
	chatCalls     [][]models.Message
	streamSteps   []deferredReplayStreamStep
	streamCalls   [][]models.Message
}

type deferredReplayStreamStep struct {
	events []models.StreamEvent
	err    error
}

func (p *deferredReplayProvider) Name() string { return "deferred-replay-provider" }

func (p *deferredReplayProvider) RequiresDeferredToolImageReplay() bool { return true }

func (p *deferredReplayProvider) Chat(_ context.Context, messages []models.Message, _ []models.ToolDefinition) (*models.Response, error) {
	p.chatCalls = append(p.chatCalls, cloneMessageSlice(messages))
	idx := len(p.chatCalls) - 1
	if idx < len(p.chatErrs) && p.chatErrs[idx] != nil {
		return nil, p.chatErrs[idx]
	}
	if idx < len(p.chatResponses) && p.chatResponses[idx] != nil {
		return p.chatResponses[idx], nil
	}
	return &models.Response{}, nil
}

func (p *deferredReplayProvider) ChatStream(_ context.Context, messages []models.Message, _ []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	p.streamCalls = append(p.streamCalls, cloneMessageSlice(messages))
	idx := len(p.streamCalls) - 1
	if idx < len(p.streamSteps) && p.streamSteps[idx].err != nil {
		return nil, p.streamSteps[idx].err
	}
	ch := make(chan models.StreamEvent, 8)
	var events []models.StreamEvent
	if idx < len(p.streamSteps) {
		events = p.streamSteps[idx].events
	}
	for _, event := range events {
		ch <- event
	}
	close(ch)
	return ch, nil
}

func (p *deferredReplayProvider) ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, _ llm.ChatOptions) (*models.Response, error) {
	return p.Chat(ctx, messages, tools)
}

func (p *deferredReplayProvider) SetDefaultOptions(_ llm.ChatOptions) {}
func (p *deferredReplayProvider) GetMaxContextTokens() int            { return 8192 }
func (p *deferredReplayProvider) BuiltinModels() []llm.ProviderModel  { return nil }
func (p *deferredReplayProvider) FetchModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}
func (p *deferredReplayProvider) ListModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}

type browserScreenshotStaticTool struct{}

func (t *browserScreenshotStaticTool) Name() string { return "browser_screenshot" }

func (t *browserScreenshotStaticTool) Description() string { return "test screenshot tool" }

func (t *browserScreenshotStaticTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}

func (t *browserScreenshotStaticTool) Execute(args map[string]interface{}) (string, error) {
	return testBrowserScreenshotResult(), nil
}

func TestPromptBuilderInjectsDeferredToolReplayMessage(t *testing.T) {
	workspace := t.TempDir()
	pb := NewPromptBuilder(workspace, "agent-test", workspace, tools.NewRegistry(), skill.NewLoader(workspace, workspace))
	pb.SetLLMProvider(&deferredReplayProvider{})
	sess := session.NewSession("webchat:test-user", "prompt-replay-session", nil)
	sess.Append(models.Message{Role: "user", Content: "show me the page"})
	sess.SetPendingToolReplay(&models.Message{
		Role:       "tool",
		Content:    "Screenshot captured",
		ToolCallID: "call_1",
		Media: []models.OutgoingMedia{{
			Type:     "image",
			Name:     "screen.png",
			MimeType: "image/png",
			Path:     "/tmp/screen.png",
		}},
	})

	messages, err := pb.Build(context.Background(), sess, "show me the page")
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	if len(messages) < 3 {
		t.Fatalf("expected replay message to be appended, got %d messages", len(messages))
	}
	last := messages[len(messages)-1]
	if last.Role != "user" {
		t.Fatalf("expected replay role user, got %q", last.Role)
	}
	if last.Content != "Tool result image replay. Use this screenshot together with the immediately preceding tool result." {
		t.Fatalf("unexpected replay content: %q", last.Content)
	}
	if len(last.Media) != 1 || last.Media[0].Path != "/tmp/screen.png" {
		t.Fatalf("expected replay media to be preserved, got %+v", last.Media)
	}
	if len(sess.GetMessages()) != 1 {
		t.Fatalf("expected replay message to stay out of session history, got %d messages", len(sess.GetMessages()))
	}
}

func TestRunDeferredReplayInjectsNextPromptAndClearsAfterSuccess(t *testing.T) {
	toolReg := tools.NewRegistry()
	toolReg.Register(&browserScreenshotStaticTool{})
	workspace := t.TempDir()
	provider := &deferredReplayProvider{chatResponses: []*models.Response{
		{ToolCalls: []models.ToolCall{{ID: "call_1", Type: "function", Function: models.ToolCallFunction{Name: "browser_screenshot", Arguments: `{}`}}}},
		{Content: "done"},
	}}
	pb := NewPromptBuilder(workspace, "agent-test", workspace, toolReg, skill.NewLoader(workspace, workspace))
	pb.SetLLMProvider(provider)
	runtime := NewRuntime("agent-test", workspace, provider, toolReg, pb, models.RoleAdmin, nil, true, nil)
	store, err := mediautil.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new media store: %v", err)
	}
	runtime.SetMediaStore(store)
	sess := session.NewSession("webchat:test-user", "run-replay-session", nil)

	if err := runtime.Run(context.Background(), sess, "take a screenshot"); err != nil {
		t.Fatalf("runtime run: %v", err)
	}
	if sess.PendingToolReplay() != nil {
		t.Fatal("expected pending replay to be cleared after successful follow-up request")
	}
	if len(provider.chatCalls) != 2 {
		t.Fatalf("expected 2 provider calls, got %d", len(provider.chatCalls))
	}
	if replay := findDeferredReplayMessage(provider.chatCalls[0]); replay != nil {
		t.Fatalf("did not expect replay on initial request, got %+v", replay)
	}
	replay := findDeferredReplayMessage(provider.chatCalls[1])
	if replay == nil {
		t.Fatal("expected replay message on follow-up request")
	}
	if len(replay.Media) != 1 || replay.Media[0].Path == "" {
		t.Fatalf("expected replay media path to be populated, got %+v", replay.Media)
	}
	messages := sess.GetMessages()
	if len(messages) != 4 {
		t.Fatalf("expected user, assistant tool call, tool result, final assistant; got %d", len(messages))
	}
	if messages[2].Role != "tool" || len(messages[2].Media) != 1 {
		t.Fatalf("expected persisted tool result with media, got %+v", messages[2])
	}
	if messages[3].Content != "done" {
		t.Fatalf("expected final assistant content, got %+v", messages[3])
	}
}

func TestRunDeferredReplayPreservesPendingReplayOnFollowupError(t *testing.T) {
	toolReg := tools.NewRegistry()
	toolReg.Register(&browserScreenshotStaticTool{})
	workspace := t.TempDir()
	provider := &deferredReplayProvider{
		chatResponses: []*models.Response{{ToolCalls: []models.ToolCall{{ID: "call_1", Type: "function", Function: models.ToolCallFunction{Name: "browser_screenshot", Arguments: `{}`}}}}},
		chatErrs:      []error{nil, fmt.Errorf("boom")},
	}
	pb := NewPromptBuilder(workspace, "agent-test", workspace, toolReg, skill.NewLoader(workspace, workspace))
	pb.SetLLMProvider(provider)
	runtime := NewRuntime("agent-test", workspace, provider, toolReg, pb, models.RoleAdmin, nil, true, nil)
	store, err := mediautil.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new media store: %v", err)
	}
	runtime.SetMediaStore(store)
	sess := session.NewSession("webchat:test-user", "run-replay-error-session", nil)

	err = runtime.Run(context.Background(), sess, "take a screenshot")
	if err == nil || err.Error() != "LLM chat after tool: boom" {
		t.Fatalf("expected follow-up error, got %v", err)
	}
	if sess.PendingToolReplay() == nil {
		t.Fatal("expected pending replay to remain available after failed follow-up request")
	}
	if len(provider.chatCalls) != 2 {
		t.Fatalf("expected 2 provider calls, got %d", len(provider.chatCalls))
	}
	if findDeferredReplayMessage(provider.chatCalls[1]) == nil {
		t.Fatal("expected failed follow-up request to still receive replay message")
	}
}

func TestRunStreamWithLoopDeferredReplayInjectsNextPromptAndClearsAfterSuccess(t *testing.T) {
	toolReg := tools.NewRegistry()
	toolReg.Register(&browserScreenshotStaticTool{})
	workspace := t.TempDir()
	provider := &deferredReplayProvider{streamSteps: []deferredReplayStreamStep{
		{events: []models.StreamEvent{{Type: "tool_calls", ToolCalls: []models.ToolCall{{ID: "call_1", Type: "function", Function: models.ToolCallFunction{Name: "browser_screenshot", Arguments: `{}`}}}}}},
		{events: []models.StreamEvent{{Type: "content", Content: "done"}, {Type: "stop"}}},
	}}
	pb := NewPromptBuilder(workspace, "agent-stream-test", workspace, toolReg, skill.NewLoader(workspace, workspace))
	pb.SetLLMProvider(provider)
	runtime := NewRuntime("agent-stream-test", workspace, provider, toolReg, pb, models.RoleAdmin, nil, true, nil)
	store, err := mediautil.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new media store: %v", err)
	}
	runtime.SetMediaStore(store)
	sess := session.NewSession("webchat:test-user", "stream-replay-session", nil)

	stream, err := runtime.RunStreamWithLoop(context.Background(), sess, "take a screenshot", models.StreamConfig{MaxIterations: 3})
	if err != nil {
		t.Fatalf("run stream with loop: %v", err)
	}
	for range stream {
	}
	if sess.PendingToolReplay() != nil {
		t.Fatal("expected pending replay to be cleared after successful stream follow-up request")
	}
	if len(provider.streamCalls) != 2 {
		t.Fatalf("expected 2 stream provider calls, got %d", len(provider.streamCalls))
	}
	if findDeferredReplayMessage(provider.streamCalls[1]) == nil {
		t.Fatal("expected replay message on second stream request")
	}
}

func cloneMessageSlice(messages []models.Message) []models.Message {
	cloned := make([]models.Message, len(messages))
	for i, msg := range messages {
		cloned[i] = msg
		cloned[i].Media = append([]models.OutgoingMedia(nil), msg.Media...)
		cloned[i].ToolCalls = append([]models.ToolCall(nil), msg.ToolCalls...)
		cloned[i].ReasoningMetadata = models.CloneReasoningReplay(msg.ReasoningMetadata)
	}
	return cloned
}

func findDeferredReplayMessage(messages []models.Message) *models.Message {
	for i := range messages {
		msg := messages[i]
		if msg.Role == "user" && msg.Content == "Tool result image replay. Use this screenshot together with the immediately preceding tool result." {
			return &msg
		}
	}
	return nil
}

func testBrowserScreenshotResult() string {
	payload, err := json.Marshal(browserScreenshotToolResult{
		Summary: "Screenshot captured (9 bytes)",
		Media: []models.OutgoingMedia{{
			Type:     "image",
			Name:     "browser-screenshot.png",
			MimeType: "image/png",
			Data:     base64.StdEncoding.EncodeToString([]byte("png-data")),
			FileSize: int64(len("png-data")),
		}},
	})
	if err != nil {
		panic(err)
	}
	return string(payload)
}
