package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/internal/tools"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
)

func TestActiveMemoryUsesActorUserOnly(t *testing.T) {
	workspace := t.TempDir()
	dataDir := t.TempDir()
	aliceDir := filepath.Join(dataDir, "users", "agent-test_user_alice")
	bobDir := filepath.Join(dataDir, "users", "agent-test_user_bob")
	if err := os.MkdirAll(aliceDir, 0o755); err != nil {
		t.Fatalf("mkdir alice dir: %v", err)
	}
	if err := os.MkdirAll(bobDir, 0o755); err != nil {
		t.Fatalf("mkdir bob dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(aliceDir, "MEMORY.md"), []byte("Alice database preference is Postgres."), 0o644); err != nil {
		t.Fatalf("write alice memory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bobDir, "MEMORY.md"), []byte("Bob database preference is SQLite."), 0o644); err != nil {
		t.Fatalf("write bob memory: %v", err)
	}

	registry := tools.NewRegistry()
	registry.Register(tools.NewMemorySearchTool(workspace, "agent-test", dataDir))
	sess := session.NewSession("wecom:group-thread", "session-1", nil)
	sess.SetMetadataValue("chat_type", "group")
	sess.SetMetadataValue("actor_user_id", "alice")

	runtime := &Runtime{agentID: "agent-test", workspace: workspace, toolRegistry: registry, userIsolation: true, role: models.RoleAdmin}
	contextText := runtime.buildActiveMemoryContext(context.Background(), sess, "database preference")
	if !strings.Contains(contextText, "Alice database preference") {
		t.Fatalf("expected actor user's memory in active context: %s", contextText)
	}
	if strings.Contains(contextText, "Bob database preference") || strings.Contains(contextText, "group-thread") {
		t.Fatalf("active memory should not include other users or group-thread memory: %s", contextText)
	}
	if !strings.Contains(contextText, "Untrusted memory context") {
		t.Fatalf("expected untrusted context wrapper: %s", contextText)
	}
}

func TestRuntimeToolCallsUseActorUserForMemoryTools(t *testing.T) {
	workspace := t.TempDir()
	dataDir := t.TempDir()
	registry := tools.NewRegistry()
	registry.Register(tools.NewMemoryWriteTool(workspace, "agent-test", dataDir))
	runtime := &Runtime{agentID: "agent-test", workspace: workspace, toolRegistry: registry, userIsolation: true, role: models.RoleAdmin}
	sess := session.NewSession("wecom:group-thread", "session-1", nil)
	sess.SetMetadataValue("actor_user_id", "alice")

	_, err := runtime.executeToolCall(context.Background(), models.ToolCall{
		ID: "call-memory-write",
		Function: models.ToolCallFunction{
			Name:      "memory_write",
			Arguments: `{"path":"MEMORY.md","content":"Alice durable fact"}`,
		},
	}, toolUserIDFromSession(sess), sess.Key)
	if err != nil {
		t.Fatalf("execute memory_write: %v", err)
	}

	alicePath := filepath.Join(dataDir, "users", "agent-test_user_alice", "MEMORY.md")
	if data, err := os.ReadFile(alicePath); err != nil || string(data) != "Alice durable fact" {
		t.Fatalf("expected alice memory write at %s, data=%q err=%v", alicePath, string(data), err)
	}
	groupPath := filepath.Join(dataDir, "users", "agent-test_user_group-thread", "MEMORY.md")
	if _, err := os.Stat(groupPath); !os.IsNotExist(err) {
		t.Fatalf("did not expect memory under group session user, stat err=%v", err)
	}
}

func TestPreCompactPromptWarnsAgainstCuratorMemoryWrite(t *testing.T) {
	sess := session.NewSession("agent:source:user:alice:ws", "session-1", nil)
	sess.SetMetadataValue("agent_id", "source")
	prompt := buildPreCompactCurationPrompt(sess, []models.Message{{Role: "user", Content: "remember this"}})
	for _, want := range []string{
		"Prefer memory_search, memory_read, memory_write, and memory_edit",
		"Use file tools only as a fallback",
		"target_user_id: alice",
		"source_agent_id: source",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected prompt to contain %q, got:\n%s", want, prompt)
		}
	}
}

type memoryToolCallProvider struct {
	stream bool
}

type sourceContextCurationProvider struct{}

func (p *memoryToolCallProvider) Name() string { return "memory-tool-call" }
func (p *memoryToolCallProvider) Chat(_ context.Context, messages []models.Message, _ []models.ToolDefinition) (*models.Response, error) {
	for _, msg := range messages {
		if msg.Role == "tool" {
			return &models.Response{Content: "done"}, nil
		}
	}
	return &models.Response{ToolCalls: []models.ToolCall{{
		ID: "call-memory-write",
		Function: models.ToolCallFunction{
			Name:      "memory_write",
			Arguments: `{"path":"MEMORY.md","content":"Alice durable fact"}`,
		},
	}}}, nil
}
func (p *memoryToolCallProvider) ChatStream(_ context.Context, messages []models.Message, _ []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	ch := make(chan models.StreamEvent, 2)
	go func() {
		defer close(ch)
		for _, msg := range messages {
			if msg.Role == "tool" {
				ch <- models.StreamEvent{Type: "content", Content: "done"}
				ch <- models.StreamEvent{Type: "stop"}
				return
			}
		}
		ch <- models.StreamEvent{Type: "tool_calls", ToolCalls: []models.ToolCall{{
			ID: "call-memory-write",
			Function: models.ToolCallFunction{
				Name:      "memory_write",
				Arguments: `{"path":"MEMORY.md","content":"Alice durable fact"}`,
			},
		}}}
	}()
	return ch, nil
}

func (p *sourceContextCurationProvider) Name() string { return "source-curation" }
func (p *sourceContextCurationProvider) Chat(_ context.Context, messages []models.Message, _ []models.ToolDefinition) (*models.Response, error) {
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
func (p *sourceContextCurationProvider) ChatStream(_ context.Context, messages []models.Message, _ []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	ch := make(chan models.StreamEvent)
	close(ch)
	return ch, nil
}
func (p *memoryToolCallProvider) ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, _ llm.ChatOptions) (*models.Response, error) {
	return p.Chat(ctx, messages, tools)
}
func (p *memoryToolCallProvider) SetDefaultOptions(_ llm.ChatOptions) {}
func (p *memoryToolCallProvider) GetMaxContextTokens() int            { return 8192 }
func (p *memoryToolCallProvider) BuiltinModels() []llm.ProviderModel  { return nil }
func (p *memoryToolCallProvider) FetchModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}
func (p *memoryToolCallProvider) ListModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}
func (p *sourceContextCurationProvider) ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, _ llm.ChatOptions) (*models.Response, error) {
	return p.Chat(ctx, messages, tools)
}
func (p *sourceContextCurationProvider) SetDefaultOptions(_ llm.ChatOptions) {}
func (p *sourceContextCurationProvider) GetMaxContextTokens() int            { return 8192 }
func (p *sourceContextCurationProvider) BuiltinModels() []llm.ProviderModel  { return nil }
func (p *sourceContextCurationProvider) FetchModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}
func (p *sourceContextCurationProvider) ListModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}

func TestRunUsesActorUserForMemoryTools(t *testing.T) {
	workspace := t.TempDir()
	dataDir := t.TempDir()
	registry := tools.NewRegistry()
	registry.Register(tools.NewMemoryWriteTool(workspace, "agent-test", dataDir))
	promptBuilder := NewPromptBuilder(workspace, "agent-test", dataDir, registry, nil)
	promptBuilder.SetUserIsolation(config.UserIsolationConfig{AutoCreate: true})
	runtime := NewRuntime("agent-test", workspace, &memoryToolCallProvider{}, registry, promptBuilder, models.RoleAdmin, nil, true, nil)
	sess := session.NewSession("wecom:group-thread", "session-1", nil)
	sess.SetMetadataValue("actor_user_id", "alice")

	if err := runtime.Run(context.Background(), sess, "remember this"); err != nil {
		t.Fatalf("runtime run: %v", err)
	}
	assertAliceMemoryOnly(t, dataDir)
}

func TestRunStreamUsesActorUserForMemoryTools(t *testing.T) {
	workspace := t.TempDir()
	dataDir := t.TempDir()
	registry := tools.NewRegistry()
	registry.Register(tools.NewMemoryWriteTool(workspace, "agent-test", dataDir))
	promptBuilder := NewPromptBuilder(workspace, "agent-test", dataDir, registry, nil)
	promptBuilder.SetUserIsolation(config.UserIsolationConfig{AutoCreate: true})
	runtime := NewRuntime("agent-test", workspace, &memoryToolCallProvider{}, registry, promptBuilder, models.RoleAdmin, nil, true, nil)
	sess := session.NewSession("wecom:group-thread", "session-1", nil)
	sess.SetMetadataValue("actor_user_id", "alice")

	stream, err := runtime.RunStreamWithLoop(context.Background(), sess, "remember this", models.StreamConfig{MaxIterations: 3})
	if err != nil {
		t.Fatalf("run stream: %v", err)
	}
	for event := range stream {
		if event.Type == "error" {
			t.Fatalf("stream error: %v", event.Error)
		}
	}
	assertAliceMemoryOnly(t, dataDir)
}

func TestRunSourceContextCurationEphemeralRoutesMemoryToTargetNamespace(t *testing.T) {
	root := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "source-curation",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("source-curation", &sourceContextCurationProvider{}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	mgr := NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := mgr.Register("source-agent", config.AgentConfig{
		Workspace: filepath.Join(root, "agents", "source-agent"),
		Provider:  "source-curation",
		Model:     "stub-model",
		Role:      string(models.RoleAdmin),
	}); err != nil {
		t.Fatalf("register source agent: %v", err)
	}
	if err := mgr.Register(ExperienceCuratorID, ExperienceCuratorConfig(root, config.AgentConfig{Provider: "source-curation", Model: "stub-model"})); err != nil {
		t.Fatalf("register curator: %v", err)
	}
	curatorAgentPath := filepath.Join(root, "agents", ExperienceCuratorID, "AGENTS.md")
	curatorBefore, err := os.ReadFile(curatorAgentPath)
	if err != nil {
		t.Fatalf("read curator AGENTS.md before run: %v", err)
	}

	sess := session.NewSession("agent:source-agent:user:alice:ws", "sess-1", nil)
	sess.SetUserID("alice")
	sess.SetMetadataValue("actor_user_id", "alice")
	sess.Append(models.Message{Role: "user", Content: "please keep this preference"})

	report, err := mgr.RunSourceContextCurationEphemeral(context.Background(), "source-agent", "alice", "curate this target context", SourceContextCurationOptions{
		TaskKind: "memory_curation",
		Sessions: []*session.Session{sess},
	})
	if err != nil {
		t.Fatalf("RunSourceContextCurationEphemeral: %v", err)
	}
	if strings.TrimSpace(report) != "done" {
		t.Fatalf("expected done report, got %q", report)
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
	if string(curatorAfter) == "Target agent operating rule" {
		t.Fatalf("did not expect target agent bootstrap content to land in curator namespace")
	}
}

func assertAliceMemoryOnly(t *testing.T, dataDir string) {
	t.Helper()
	alicePath := filepath.Join(dataDir, "users", "agent-test_user_alice", "MEMORY.md")
	data, err := os.ReadFile(alicePath)
	if err != nil || string(data) != "Alice durable fact" {
		t.Fatalf("expected alice memory write at %s, data=%q err=%v", alicePath, string(data), err)
	}
	groupPath := filepath.Join(dataDir, "users", "agent-test_user_group-thread", "MEMORY.md")
	if _, err := os.Stat(groupPath); !os.IsNotExist(err) {
		t.Fatalf("did not expect memory under group session user, stat err=%v", err)
	}
	if entries, err := os.ReadDir(filepath.Join(dataDir, "users")); err == nil && len(entries) != 1 {
		var names []string
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("expected only alice user dir, got %s", fmt.Sprint(names))
	}
}

func TestActiveMemorySkippedForScheduleSession(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(tools.NewMemorySearchTool(t.TempDir(), "agent-test", t.TempDir()))
	sess := session.NewSession("agent:agent-test:user:alice:schedule:task-1", "session-1", nil)
	sess.SetMetadataValue("session_kind", "schedule")
	sess.SetMetadataValue("actor_user_id", "alice")
	runtime := &Runtime{agentID: "agent-test", workspace: t.TempDir(), toolRegistry: registry, userIsolation: true, role: models.RoleAdmin}
	if contextText := runtime.buildActiveMemoryContext(context.Background(), sess, "anything"); contextText != "" {
		t.Fatalf("expected schedule active memory to be skipped, got: %s", contextText)
	}
}

func TestInjectActiveMemoryContextAddsToSystemMessage(t *testing.T) {
	messages := []models.Message{{Role: "system", Content: "base"}, {Role: "user", Content: "hi"}}
	out := injectActiveMemoryContext(messages, "active")
	if !strings.Contains(out[0].Content, "base\n\nactive") {
		t.Fatalf("expected active context appended to system message: %#v", out[0].Content)
	}
	if messages[0].Content != "base" {
		t.Fatalf("inject should not mutate original messages")
	}
}
