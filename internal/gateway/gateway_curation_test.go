package gateway

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chawuciren/evoduck/internal/agent"
	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
)

type sessionResetSourceContextProvider struct{}

func (p *sessionResetSourceContextProvider) Name() string { return "source-curation" }
func (p *sessionResetSourceContextProvider) Chat(_ context.Context, messages []models.Message, _ []models.ToolDefinition) (*models.Response, error) {
	toolMessages := 0
	for _, msg := range messages {
		if msg.Role == "tool" {
			toolMessages++
		}
	}
	switch toolMessages {
	case 0:
		return &models.Response{ToolCalls: []models.ToolCall{{
			ID: "call-user-memory-write",
			Function: models.ToolCallFunction{
				Name:      "memory_write",
				Arguments: `{"path":"MEMORY.md","content":"Target user durable fact"}`,
			},
		}}}, nil
	case 1:
		return &models.Response{ToolCalls: []models.ToolCall{{
			ID: "call-agent-bootstrap-write",
			Function: models.ToolCallFunction{
				Name:      "memory_write",
				Arguments: `{"path":"AGENTS.md","content":"Target agent operating rule"}`,
			},
		}}}, nil
	default:
		return &models.Response{Content: "done"}, nil
	}
}
func (p *sessionResetSourceContextProvider) ChatStream(_ context.Context, messages []models.Message, _ []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	toolMessages := 0
	for _, msg := range messages {
		if msg.Role == "tool" {
			toolMessages++
		}
	}
	ch := make(chan models.StreamEvent, 2)
	go func() {
		defer close(ch)
		switch toolMessages {
		case 0:
			ch <- models.StreamEvent{Type: "tool_calls", ToolCalls: []models.ToolCall{{
				ID: "call-user-memory-write",
				Function: models.ToolCallFunction{
					Name:      "memory_write",
					Arguments: `{"path":"MEMORY.md","content":"Target user durable fact"}`,
				},
			}}}
		case 1:
			ch <- models.StreamEvent{Type: "tool_calls", ToolCalls: []models.ToolCall{{
				ID: "call-agent-bootstrap-write",
				Function: models.ToolCallFunction{
					Name:      "memory_write",
					Arguments: `{"path":"AGENTS.md","content":"Target agent operating rule"}`,
				},
			}}}
		default:
			ch <- models.StreamEvent{Type: "content", Content: "done"}
			ch <- models.StreamEvent{Type: "stop"}
		}
	}()
	return ch, nil
}
func (p *sessionResetSourceContextProvider) ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, _ llm.ChatOptions) (*models.Response, error) {
	return p.Chat(ctx, messages, tools)
}
func (p *sessionResetSourceContextProvider) SetDefaultOptions(_ llm.ChatOptions) {}
func (p *sessionResetSourceContextProvider) GetMaxContextTokens() int            { return 8192 }
func (p *sessionResetSourceContextProvider) BuiltinModels() []llm.ProviderModel  { return nil }
func (p *sessionResetSourceContextProvider) FetchModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}
func (p *sessionResetSourceContextProvider) ListModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}

func TestBuildSessionResetCurationPromptPrefersMemoryTools(t *testing.T) {
	sess := session.NewSession("agent:source:user:alice:ws", "session-1", nil)
	sess.SetUserID("alice")
	sess.SetMetadataValue("actor_user_id", "alice")
	sess.Append(models.Message{Role: "user", Content: "Remember that I prefer concise replies."})

	prompt := buildSessionResetCurationPrompt("source", sess, "alice")
	for _, want := range []string{
		"Prefer memory_search, memory_read, memory_write, and memory_edit before any file tool",
		"memory/YYYY-MM-DD.md",
		"MEMORY.md only for stable durable facts",
		"Do not stop at an internal summary",
		"target_user_id: alice",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected prompt to contain %q\nfull prompt:\n%s", want, prompt)
		}
	}
}

func TestFlushSessionMemoryRoutesToSourceAgentNamespace(t *testing.T) {
	root := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "source-curation",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("source-curation", &sessionResetSourceContextProvider{}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	memoryCfg := config.MemoryConfig{}
	memoryCfg.ShortTerm.SessionTTL = 24
	mgr := agent.NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, memoryCfg, nil, nil, nil)
	if err := mgr.Register("source-agent", config.AgentConfig{
		Workspace: filepath.Join(root, "agents", "source-agent"),
		Provider:  "source-curation",
		Model:     "stub-model",
		Role:      string(models.RoleAdmin),
	}); err != nil {
		t.Fatalf("register source agent: %v", err)
	}
	if err := mgr.Register(agent.ExperienceCuratorID, agent.ExperienceCuratorConfig(root, config.AgentConfig{Provider: "source-curation", Model: "stub-model"})); err != nil {
		t.Fatalf("register curator: %v", err)
	}
	curatorAgentPath := filepath.Join(root, "agents", agent.ExperienceCuratorID, "AGENTS.md")
	curatorBefore, err := os.ReadFile(curatorAgentPath)
	if err != nil {
		t.Fatalf("read curator AGENTS.md before run: %v", err)
	}

	cfg := &config.Config{
		DataDir:      root,
		DefaultAgent: "source-agent",
		LLM:          config.LLMConfig{DefaultProvider: "source-curation", DefaultModel: "stub-model", Providers: map[string]config.ProviderConfig{}},
		Agents:       map[string]config.AgentConfig{"source-agent": {Workspace: filepath.Join(root, "agents", "source-agent"), Provider: "source-curation", Model: "stub-model", Role: string(models.RoleAdmin)}},
		Channels:     map[string]config.ChannelConfig{},
		Memory:       memoryCfg,
		Scheduler:    config.SchedulerConfig{},
		Plugins:      config.PluginConfig{},
		MCP:          config.MCPConfig{Servers: map[string]config.MCPServerConfig{}},
	}
	gw := New(cfg, filepath.Join(root, "config.yaml"), llmReg, mgr, nil, nil)

	sess := session.NewSession("agent:source-agent:user:session-owner:ws", "session-1", nil)
	sess.SetUserID("session-owner")
	sess.SetMetadataValue("actor_user_id", "alice")
	sess.SetMetadataValue("agent_id", "source-agent")
	sess.Append(models.Message{Role: "user", Content: "Please remember my durable preference."})

	result, err := gw.FlushSessionMemory("source-agent", sess, models.RoleAdmin, "")
	if err != nil {
		t.Fatalf("FlushSessionMemory: %v", err)
	}
	if result == nil || !result.Flushed {
		t.Fatalf("expected flushed result, got %+v", result)
	}

	targetUserPath := filepath.Join(root, "users", "source-agent_user_alice", "MEMORY.md")
	if data, err := os.ReadFile(targetUserPath); err != nil || string(data) != "Target user durable fact" {
		t.Fatalf("expected target user memory at %s, data=%q err=%v", targetUserPath, string(data), err)
	}
	targetAgentPath := filepath.Join(root, "agents", "source-agent", "AGENTS.md")
	if data, err := os.ReadFile(targetAgentPath); err != nil || string(data) != "Target agent operating rule" {
		t.Fatalf("expected target agent bootstrap at %s, data=%q err=%v", targetAgentPath, string(data), err)
	}
	curatorUserPath := filepath.Join(root, "users", "experience-curator_user_alice", "MEMORY.md")
	if _, err := os.Stat(curatorUserPath); !os.IsNotExist(err) {
		t.Fatalf("did not expect curator user memory namespace write, stat err=%v", err)
	}
	curatorAfter, err := os.ReadFile(curatorAgentPath)
	if err != nil {
		t.Fatalf("read curator AGENTS.md after run: %v", err)
	}
	if string(curatorAfter) != string(curatorBefore) {
		t.Fatalf("did not expect curator AGENTS.md to change\nbefore:\n%s\nafter:\n%s", string(curatorBefore), string(curatorAfter))
	}
}
