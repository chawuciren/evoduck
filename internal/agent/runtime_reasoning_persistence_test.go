package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/internal/skill"
	"github.com/chawuciren/evoduck/internal/tools"
	"github.com/chawuciren/evoduck/pkg/models"
)

type reasoningProvider struct{}

func (p *reasoningProvider) Name() string { return "reasoning-provider" }

func (p *reasoningProvider) Chat(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (*models.Response, error) {
	return &models.Response{Content: "visible answer", ReasoningContent: "hidden chain"}, nil
}

func (p *reasoningProvider) ChatStream(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	return nil, fmt.Errorf("unexpected ChatStream call")
}

func (p *reasoningProvider) ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, _ llm.ChatOptions) (*models.Response, error) {
	return p.Chat(ctx, messages, tools)
}

func (p *reasoningProvider) SetDefaultOptions(_ llm.ChatOptions) {}
func (p *reasoningProvider) GetMaxContextTokens() int            { return 8192 }
func (p *reasoningProvider) BuiltinModels() []llm.ProviderModel  { return nil }
func (p *reasoningProvider) FetchModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}
func (p *reasoningProvider) ListModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}

func TestRuntimeRunPersistsReasoningContent(t *testing.T) {
	tempDir := t.TempDir()
	toolReg := tools.NewRegistry()
	promptBuilder := NewPromptBuilder(tempDir, "agent-reasoning-test", tempDir, toolReg, skill.NewLoader(tempDir, tempDir))
	runtime := NewRuntime("agent-reasoning-test", tempDir, &reasoningProvider{}, toolReg, promptBuilder, models.RoleAdmin, nil, true, nil)
	sess := session.NewSession("webchat:test-user", "reasoning-session", nil)

	if err := runtime.Run(context.Background(), sess, "answer with reasoning"); err != nil {
		t.Fatalf("runtime run: %v", err)
	}

	messages := sess.GetMessages()
	if len(messages) == 0 {
		t.Fatal("expected persisted assistant message")
	}
	last := messages[len(messages)-1]
	if last.Role != "assistant" {
		t.Fatalf("expected assistant message, got %q", last.Role)
	}
	if last.Content != "visible answer" {
		t.Fatalf("expected content to persist, got %q", last.Content)
	}
	if last.ThinkingContent != "hidden chain" {
		t.Fatalf("expected reasoning to persist, got %q", last.ThinkingContent)
	}
}
