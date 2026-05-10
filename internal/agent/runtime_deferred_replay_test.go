package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/internal/mediautil"
	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/internal/skill"
	"github.com/chawuciren/evoduck/internal/tools"
	"github.com/chawuciren/evoduck/pkg/models"
)

type toolMediaProvider struct {
	chatResponses []*models.Response
	chatCalls     [][]models.Message
}

func (p *toolMediaProvider) Name() string { return "tool-media-provider" }

func (p *toolMediaProvider) Chat(_ context.Context, messages []models.Message, _ []models.ToolDefinition) (*models.Response, error) {
	p.chatCalls = append(p.chatCalls, cloneMessageSlice(messages))
	idx := len(p.chatCalls) - 1
	if idx < len(p.chatResponses) && p.chatResponses[idx] != nil {
		return p.chatResponses[idx], nil
	}
	return &models.Response{}, nil
}

func (p *toolMediaProvider) ChatStream(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	return nil, nil
}

func (p *toolMediaProvider) ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, _ llm.ChatOptions) (*models.Response, error) {
	return p.Chat(ctx, messages, tools)
}

func (p *toolMediaProvider) SetDefaultOptions(_ llm.ChatOptions) {}
func (p *toolMediaProvider) GetMaxContextTokens() int           { return 8192 }
func (p *toolMediaProvider) BuiltinModels() []llm.ProviderModel { return nil }
func (p *toolMediaProvider) FetchModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}
func (p *toolMediaProvider) ListModels(_ context.Context) ([]llm.ProviderModel, error) {
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

func TestPromptBuilderDoesNotInjectDeferredToolReplayMessage(t *testing.T) {
	workspace := t.TempDir()
	pb := NewPromptBuilder(workspace, "agent-test", workspace, tools.NewRegistry(), skill.NewLoader(workspace, workspace))
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
	if len(messages) != 2 {
		t.Fatalf("expected only system and history messages, got %d", len(messages))
	}
	if len(sess.GetMessages()) != 1 {
		t.Fatalf("expected session history to stay unchanged, got %d messages", len(sess.GetMessages()))
	}
}

func TestRunPersistsToolMediaWithoutDeferredReplay(t *testing.T) {
	toolReg := tools.NewRegistry()
	toolReg.Register(&browserScreenshotStaticTool{})
	workspace := t.TempDir()
	provider := &toolMediaProvider{chatResponses: []*models.Response{
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
		t.Fatal("expected pending replay to remain unused")
	}
	if len(provider.chatCalls) != 2 {
		t.Fatalf("expected 2 provider calls, got %d", len(provider.chatCalls))
	}
	if replay := findDeferredReplayMessage(provider.chatCalls[1]); replay != nil {
		t.Fatalf("did not expect deferred replay message, got %+v", replay)
	}
	messages := sess.GetMessages()
	if len(messages) != 4 {
		t.Fatalf("expected user, assistant tool call, tool result, final assistant; got %d", len(messages))
	}
	if messages[2].Role != "tool" || len(messages[2].Media) != 1 || messages[2].Media[0].Path == "" {
		t.Fatalf("expected persisted tool result with media path, got %+v", messages[2])
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
		Summary:      "Screenshot captured (9 bytes)",
		OriginalSize: int64(len("png-data")),
		FinalSize:    int64(len("png-data")),
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
