package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chawuciren/evoduck/internal/agent"
	"github.com/chawuciren/evoduck/internal/channels"
	"github.com/chawuciren/evoduck/internal/command"
	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/internal/profile"
	"github.com/chawuciren/evoduck/internal/router"
	"github.com/chawuciren/evoduck/internal/scheduler"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/gorilla/websocket"
)

type capturingBridge struct {
	name         string
	last         *models.OutgoingMessage
	sent         []*models.OutgoingMessage
	typingStates []bool
	err          error
}

func (b *capturingBridge) Name() string                                { return b.name }
func (b *capturingBridge) Connect(_ context.Context) error             { return nil }
func (b *capturingBridge) Disconnect() error                           { return nil }
func (b *capturingBridge) OnMessage(_ func(*models.NormalizedMessage)) {}
func (b *capturingBridge) Send(_ context.Context, msg *models.OutgoingMessage) error {
	b.sent = append(b.sent, msg)
	if b.err != nil {
		err := b.err
		b.err = nil
		return err
	}
	b.last = msg
	return nil
}
func (b *capturingBridge) Broadcast(_ context.Context, _ string, _ string) error { return nil }
func (b *capturingBridge) SupportsProactiveSend() bool                           { return strings.HasPrefix(b.name, "wecom") }
func (b *capturingBridge) SetTyping(_ context.Context, _ *models.NormalizedMessage, active bool) error {
	b.typingStates = append(b.typingStates, active)
	return nil
}

type scheduleBindingProvider struct{}

type cancellableStreamProvider struct {
	started chan string
}

type curationReportProvider struct{}

func (p *scheduleBindingProvider) Name() string { return "stub" }
func (p *scheduleBindingProvider) Chat(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (*models.Response, error) {
	return &models.Response{Content: "ok"}, nil
}
func (p *scheduleBindingProvider) ChatStream(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	ch := make(chan models.StreamEvent)
	close(ch)
	return ch, nil
}
func (p *scheduleBindingProvider) ChatWithOptions(_ context.Context, _ []models.Message, _ []models.ToolDefinition, _ llm.ChatOptions) (*models.Response, error) {
	return &models.Response{Content: "ok"}, nil
}
func (p *scheduleBindingProvider) SetDefaultOptions(_ llm.ChatOptions) {}
func (p *scheduleBindingProvider) GetMaxContextTokens() int            { return 8192 }
func (p *scheduleBindingProvider) BuiltinModels() []llm.ProviderModel  { return nil }
func (p *scheduleBindingProvider) FetchModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}
func (p *scheduleBindingProvider) ListModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}

func (p *cancellableStreamProvider) Name() string { return "cancellable" }
func (p *cancellableStreamProvider) Chat(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (*models.Response, error) {
	return &models.Response{Content: "ok"}, nil
}
func (p *cancellableStreamProvider) ChatStream(ctx context.Context, messages []models.Message, _ []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	ch := make(chan models.StreamEvent)
	requestContent := "working"
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" && strings.TrimSpace(messages[i].Content) != "" {
			requestContent = strings.TrimSpace(messages[i].Content)
			break
		}
	}
	go func() {
		defer close(ch)
		select {
		case p.started <- requestContent:
		default:
		}
		ch <- models.StreamEvent{Type: "content", Content: requestContent}
		<-ctx.Done()
	}()
	return ch, nil
}
func (p *cancellableStreamProvider) ChatWithOptions(_ context.Context, _ []models.Message, _ []models.ToolDefinition, _ llm.ChatOptions) (*models.Response, error) {
	return &models.Response{Content: "ok"}, nil
}
func (p *cancellableStreamProvider) SetDefaultOptions(_ llm.ChatOptions) {}
func (p *cancellableStreamProvider) GetMaxContextTokens() int            { return 8192 }
func (p *cancellableStreamProvider) BuiltinModels() []llm.ProviderModel  { return nil }
func (p *cancellableStreamProvider) FetchModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}
func (p *cancellableStreamProvider) ListModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}

func (p *curationReportProvider) Name() string { return "curation-report" }
func (p *curationReportProvider) Chat(_ context.Context, messages []models.Message, _ []models.ToolDefinition) (*models.Response, error) {
	toolMessages := 0
	promptText := ""
	for _, msg := range messages {
		if msg.Role == "tool" {
			toolMessages++
		}
		if promptText == "" && msg.Role == "user" {
			promptText = msg.Content
		}
	}
	isDaily := strings.Contains(promptText, "task_kind: experience_curation") || strings.Contains(promptText, "daily-report")
	if !isDaily {
		switch toolMessages {
		case 0:
			return &models.Response{ToolCalls: []models.ToolCall{{
				ID: "call-hourly-daily-log",
				Function: models.ToolCallFunction{
					Name:      "memory_write",
					Arguments: `{"path":"memory/2026-05-08.md","content":"Hourly curation log\n- captured session preference: concise updates"}`,
				},
			}}}, nil
		default:
			return &models.Response{Content: "hourly-report: updated memory/2026-05-08.md with concise-updates note"}, nil
		}
	}
	switch toolMessages {
	case 0:
		return &models.Response{ToolCalls: []models.ToolCall{{
			ID: "call-daily-memory",
			Function: models.ToolCallFunction{
				Name:      "memory_write",
				Arguments: `{"path":"MEMORY.md","content":"Durable user preference: concise updates"}`,
			},
		}}}, nil
	case 1:
		return &models.Response{ToolCalls: []models.ToolCall{{
			ID: "call-daily-agents",
			Function: models.ToolCallFunction{
				Name:      "memory_write",
				Arguments: `{"path":"AGENTS.md","content":"Always keep curation summaries concise and target-bound."}`,
			},
		}}}, nil
	default:
		return &models.Response{Content: "daily-report: updated MEMORY.md and AGENTS.md for the target namespace"}, nil
	}
}
func (p *curationReportProvider) ChatStream(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	ch := make(chan models.StreamEvent)
	close(ch)
	return ch, nil
}
func (p *curationReportProvider) ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, _ llm.ChatOptions) (*models.Response, error) {
	return p.Chat(ctx, messages, tools)
}
func (p *curationReportProvider) SetDefaultOptions(_ llm.ChatOptions) {}
func (p *curationReportProvider) GetMaxContextTokens() int            { return 8192 }
func (p *curationReportProvider) BuiltinModels() []llm.ProviderModel  { return nil }
func (p *curationReportProvider) FetchModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}
func (p *curationReportProvider) ListModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}

func TestExecuteScheduledRunUsesExecutionSessionKey(t *testing.T) {
	root := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleBindingProvider{}); err != nil {
		t.Fatalf("register dynamic provider: %v", err)
	}

	agentMgr := agent.NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := agentMgr.Register("agent-test", config.AgentConfig{
		Workspace: filepath.Join(root, "agent-test"),
		Provider:  "stub",
		Model:     "stub-model",
		Role:      string(models.RoleAdmin),
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	gw := New(&config.Config{DataDir: root, DefaultAgent: "agent-test"}, filepath.Join(root, "config.yaml"), llmReg, agentMgr, nil, nil)
	originSessionKey := "agent:agent-test:user:admin:ws"
	originSession := gw.GetOrCreateSession(originSessionKey)
	originSession.Append(models.Message{Role: "user", Content: "existing session context"})
	executionSessionKey := "agent:agent-test:user:admin:schedule:task-1"

	err = gw.executeScheduledRun(schedulerRecordForTest("agent-test", "admin", "task-1", originSessionKey, executionSessionKey))
	if err != nil {
		t.Fatalf("execute scheduled run: %v", err)
	}

	if originSession.MessageCount() != 1 {
		t.Fatalf("expected origin session to remain unchanged, got %d messages", originSession.MessageCount())
	}
	if _, err := gw.GetSessionManagerRaw().Get(executionSessionKey); err == nil {
		t.Fatal("expected scheduled run to use an ephemeral execution session")
	}
	logPath := gw.backgroundRunLogPath("schedule", "task-1")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected scheduled run log: %v", err)
	}
	if !strings.Contains(string(data), "run in origin session") {
		t.Fatalf("expected scheduled run log to contain prompt, got %q", string(data))
	}
}

func TestSystemCurationTaskSpecUsesProfilePrompts(t *testing.T) {
	tests := []struct {
		scheduleID       string
		wantTaskKind     string
		wantPrompt       string
		wantErrSubstring string
	}{
		{scheduleID: "system:memory-curation", wantTaskKind: "memory_curation", wantPrompt: profile.DefaultHourlyMemoryCurationPrompt()},
		{scheduleID: "system:experience-curation", wantTaskKind: "experience_curation", wantPrompt: profile.DefaultDailyExperienceCurationPrompt()},
		{scheduleID: "system:unknown", wantErrSubstring: "unknown system schedule"},
	}

	for _, tt := range tests {
		t.Run(tt.scheduleID, func(t *testing.T) {
			taskKind, prompt, err := systemCurationTaskSpec(tt.scheduleID)
			if tt.wantErrSubstring != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrSubstring) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErrSubstring, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("systemCurationTaskSpec(%q): %v", tt.scheduleID, err)
			}
			if taskKind != tt.wantTaskKind {
				t.Fatalf("expected task kind %q, got %q", tt.wantTaskKind, taskKind)
			}
			if prompt != tt.wantPrompt {
				t.Fatalf("expected prompt to match profile helper for %s", tt.scheduleID)
			}
		})
	}
}

func TestSystemScheduleRecordsUseExperienceCurator(t *testing.T) {
	root := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleBindingProvider{}); err != nil {
		t.Fatalf("register dynamic provider: %v", err)
	}
	agentMgr := agent.NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := agentMgr.Register(agent.ExperienceCuratorID, agent.ExperienceCuratorConfig(root, config.AgentConfig{Provider: "stub", Model: "stub-model"})); err != nil {
		t.Fatalf("register curator: %v", err)
	}
	gw := New(&config.Config{
		DataDir: root,
		Scheduler: config.SchedulerConfig{SystemTasks: config.SystemSchedulerTasksConfig{
			MemoryCuration:     config.SystemTaskConfig{Schedule: "0 * * * *"},
			ExperienceCuration: config.SystemTaskConfig{Schedule: "0 3 * * *"},
		}},
	}, filepath.Join(root, "config.yaml"), llmReg, agentMgr, nil, nil)

	records := gw.systemScheduleRecords()
	if len(records) != 2 {
		t.Fatalf("expected 2 system schedule records, got %d", len(records))
	}
	for _, record := range records {
		if record.AgentID != agent.ExperienceCuratorID {
			t.Fatalf("expected curator agent, got %q", record.AgentID)
		}
		if record.Scope != scheduler.ScheduleScopeSystem {
			t.Fatalf("expected system scope, got %q", record.Scope)
		}
		if !record.Enabled {
			t.Fatalf("expected system record enabled")
		}
		if record.Metadata["memory_policy"] != "ignore" {
			t.Fatalf("expected memory_policy ignore, got %q", record.Metadata["memory_policy"])
		}
		if record.Metadata["task_kind"] == "" {
			t.Fatalf("expected task_kind metadata")
		}
	}
}

func TestSystemScheduleNoPersistentSessionFiles(t *testing.T) {
	root := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleBindingProvider{}); err != nil {
		t.Fatalf("register dynamic provider: %v", err)
	}
	agentMgr := agent.NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := agentMgr.Register(agent.ExperienceCuratorID, agent.ExperienceCuratorConfig(root, config.AgentConfig{Provider: "stub", Model: "stub-model"})); err != nil {
		t.Fatalf("register curator: %v", err)
	}
	gw := New(&config.Config{
		DataDir: root,
		Scheduler: config.SchedulerConfig{SystemTasks: config.SystemSchedulerTasksConfig{
			MemoryCuration:     config.SystemTaskConfig{Schedule: "0 * * * *"},
			ExperienceCuration: config.SystemTaskConfig{Schedule: "0 3 * * *"},
		}},
	}, filepath.Join(root, "config.yaml"), llmReg, agentMgr, nil, nil)
	_ = gw

	sessionDir := filepath.Join(root, "sessions")
	files, err := os.ReadDir(sessionDir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("read session dir: %v", err)
	}

	for _, file := range files {
		name := file.Name()
		if strings.Contains(name, "experience-curator") && strings.Contains(name, "schedule") {
			t.Fatalf("unexpected system schedule session file: %s", name)
		}
	}
}

func TestExecuteCuratorSystemTaskUsesEphemeralSession(t *testing.T) {
	root := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleBindingProvider{}); err != nil {
		t.Fatalf("register dynamic provider: %v", err)
	}
	agentMgr := agent.NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := agentMgr.Register(agent.ExperienceCuratorID, agent.ExperienceCuratorConfig(root, config.AgentConfig{Provider: "stub", Model: "stub-model"})); err != nil {
		t.Fatalf("register curator: %v", err)
	}
	gw := New(&config.Config{DataDir: root}, filepath.Join(root, "config.yaml"), llmReg, agentMgr, nil, nil)
	record := scheduler.ScheduleRecord{
		ID:                  "system:memory-curation",
		AgentID:             agent.ExperienceCuratorID,
		ExecutionSessionKey: "agent:experience-curator:schedule:system:memory-curation",
		Metadata:            map[string]string{"task_kind": "memory_curation"},
	}

	if err := gw.executeCuratorSystemTask(record, "memory_curation", "curate memory"); err != nil {
		t.Fatalf("execute curator task: %v", err)
	}
	if _, err := gw.GetSessionManagerRaw().Get(record.ExecutionSessionKey); err == nil {
		t.Fatalf("expected no persistent schedule session for curator system task")
	}
}

func TestDiscoverSystemCurationTargetsGroupsOrdinarySessionsByAgentAndUser(t *testing.T) {
	root := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleBindingProvider{}); err != nil {
		t.Fatalf("register dynamic provider: %v", err)
	}
	agentMgr := agent.NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	gw := New(&config.Config{DataDir: root, DefaultAgent: "agent-a"}, filepath.Join(root, "config.yaml"), llmReg, agentMgr, nil, nil)

	makeSession := func(key, agentID, userID, actorUserID, kind, memoryPolicy string, updatedAt time.Time, message string) {
		sess := gw.GetOrCreateSession(key)
		sess.SetUserID(userID)
		sess.SetMetadataValue("agent_id", agentID)
		sess.SetMetadataValue("session_kind", kind)
		sess.SetMetadataValue("memory_policy", memoryPolicy)
		sess.SetMetadataValue("actor_user_id", actorUserID)
		sess.Append(models.Message{Role: "user", Content: message})
		sess.UpdatedAt = updatedAt
	}

	now := time.Now()
	makeSession("agent:agent-a:user:u1:ws", "agent-a", "u1", "u1", "normal", "", now.Add(-20*time.Minute), "recent user session a1")
	makeSession("agent:agent-a:user:u1:web", "agent-a", "u1", "u1", "normal", "", now.Add(-5*time.Minute), "recent user session a2")
	makeSession("agent:agent-b:user:u2:ws", "agent-b", "u2", "u2", "", "", now.Add(-15*time.Minute), "recent user session b1")
	makeSession("agent:agent-b:user:u2:ws:ignored", "agent-b", "u2", "u2", "normal", "ignore", now.Add(-10*time.Minute), "ignored memory policy")
	makeSession("agent:agent-a:user:u1:schedule:task-1", "agent-a", "u1", "u1", "schedule", "", now.Add(-8*time.Minute), "scheduled session")
	makeSession("agent:experience-curator:user:u1:ws", agent.ExperienceCuratorID, "u1", "u1", "normal", "", now.Add(-7*time.Minute), "curator session")
	makeSession("agent:agent-c:user:u3:ws", "agent-c", "u3", "u3", "system_task", "", now.Add(-6*time.Minute), "system task session")
	makeSession("agent:agent-old:user:u4:ws", "agent-old", "u4", "u4", "normal", "", now.Add(-3*time.Hour), "stale session")
	gw.GetOrCreateSession("agent:agent-empty:user:u5:ws").SetMetadataValue("agent_id", "agent-empty")

	targets := gw.discoverSystemCurationTargets("memory_curation")
	if len(targets) != 2 {
		t.Fatalf("expected 2 curation targets, got %#v", targets)
	}
	if targets[0].SourceAgentID != "agent-a" || targets[0].TargetUserID != "u1" {
		t.Fatalf("unexpected first target: %#v", targets[0])
	}
	if len(targets[0].Sessions) != 2 {
		t.Fatalf("expected 2 sessions for agent-a/u1, got %d", len(targets[0].Sessions))
	}
	if targets[0].Sessions[0].Key != "agent:agent-a:user:u1:web" {
		t.Fatalf("expected newest session first, got %q", targets[0].Sessions[0].Key)
	}
	if targets[1].SourceAgentID != "agent-b" || targets[1].TargetUserID != "u2" {
		t.Fatalf("unexpected second target: %#v", targets[1])
	}
	if len(targets[1].Sessions) != 1 || targets[1].Sessions[0].Key != "agent:agent-b:user:u2:ws" {
		t.Fatalf("unexpected sessions for agent-b/u2: %#v", targets[1].Sessions)
	}
}

func TestExperienceCuratorHiddenFromAgentList(t *testing.T) {
	root := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleBindingProvider{}); err != nil {
		t.Fatalf("register dynamic provider: %v", err)
	}
	agentMgr := agent.NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := agentMgr.Register("admin-bot", config.AgentConfig{Workspace: filepath.Join(root, "agents", "admin-bot"), Provider: "stub", Model: "stub-model", Role: string(models.RoleAdmin)}); err != nil {
		t.Fatalf("register admin: %v", err)
	}
	if err := agentMgr.Register(agent.ExperienceCuratorID, agent.ExperienceCuratorConfig(root, config.AgentConfig{Provider: "stub", Model: "stub-model"})); err != nil {
		t.Fatalf("register curator: %v", err)
	}
	visible := agentMgr.List()
	if len(visible) != 1 || visible[0].ID != "admin-bot" {
		t.Fatalf("expected only admin-bot visible, got %#v", visible)
	}
	all := agentMgr.ListAll()
	if len(all) != 2 {
		t.Fatalf("expected all agents to include curator, got %d", len(all))
	}
}

func TestExperienceCuratorUsesRestrictedPermissions(t *testing.T) {
	root := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleBindingProvider{}); err != nil {
		t.Fatalf("register dynamic provider: %v", err)
	}
	agentMgr := agent.NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{Enabled: true}, config.MemoryConfig{}, nil, nil, nil)
	if err := agentMgr.Register(agent.ExperienceCuratorID, agent.ExperienceCuratorConfig(root, config.AgentConfig{Provider: "stub", Model: "stub-model"})); err != nil {
		t.Fatalf("register curator: %v", err)
	}
	curator, err := agentMgr.Get(agent.ExperienceCuratorID)
	if err != nil {
		t.Fatalf("get curator: %v", err)
	}
	for _, name := range []string{"file_read", "file_write", "file_edit", "sessions_list", "sessions_history", "memory_search", "memory_read", "memory_write", "memory_edit", "knowledge_tree", "knowledge_search", "knowledge_read", "skill_use", "system_reload"} {
		if _, err := curator.Tools.Get(name); err != nil {
			t.Fatalf("expected curator tool %s: %v", name, err)
		}
	}
	for _, name := range []string{"exec", "process", "http_call", "code_execution", "schedule_create", "schedule_enable", "schedule_disable", "schedule_delete", "schedule_trigger", "knowledge_list", "knowledge_move", "knowledge_delete", "knowledge_create_directory", "knowledge_delete_directory", "knowledge_write", "sessions_send", "sessions_run"} {
		if _, err := curator.Tools.Get(name); err == nil {
			t.Fatalf("did not expect curator tool %s", name)
		}
	}
	if len(curator.Config.Permissions.AuthorizedDirectories) != 1 || curator.Config.Permissions.AuthorizedDirectories[0] != root {
		t.Fatalf("expected curator directory restricted to data dir, got %#v", curator.Config.Permissions.AuthorizedDirectories)
	}
}

func TestConfigApplyEnsuresExperienceCuratorScaffold(t *testing.T) {
	root := filepath.Join(os.TempDir(), "evoduck-curator-apply-test")
	_ = os.RemoveAll(root)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleBindingProvider{}); err != nil {
		t.Fatalf("register dynamic provider: %v", err)
	}
	agentMgr := agent.NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	gw := New(&config.Config{DataDir: root, DefaultAgent: "admin-bot"}, filepath.Join(root, "config.yaml"), llmReg, agentMgr, nil, nil)
	cfg := &config.Config{
		DataDir:      root,
		DefaultAgent: "admin-bot",
		Agents:       map[string]config.AgentConfig{"admin-bot": {Workspace: filepath.Join(root, "agents", "admin-bot"), Provider: "stub", Model: "stub-model", Role: string(models.RoleAdmin)}},
		LLM:          config.LLMConfig{DefaultProvider: "stub", DefaultModel: "stub-model"},
		Scheduler:    config.SchedulerConfig{SystemTasks: config.SystemSchedulerTasksConfig{MemoryCuration: config.SystemTaskConfig{Schedule: "0 * * * *"}, ExperienceCuration: config.SystemTaskConfig{Schedule: "0 3 * * *"}}},
		Channels:     config.ChannelsConfig{},
	}

	if _, err := gw.applyConfig(cfg); err != nil {
		t.Fatalf("apply config: %v", err)
	}
	curator, err := agentMgr.Get(agent.ExperienceCuratorID)
	if err != nil {
		t.Fatalf("expected curator registered: %v", err)
	}
	if _, err := os.Stat(filepath.Join(curator.Config.Workspace, "AGENTS.md")); err != nil {
		t.Fatalf("expected curator AGENTS.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(curator.Config.Workspace, "SOUL.md")); err != nil {
		t.Fatalf("expected curator SOUL.md: %v", err)
	}
}

func TestUserCannotManageSystemSchedules(t *testing.T) {
	root := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{DefaultProvider: "stub", DefaultModel: "stub-model", Providers: map[string]config.ProviderConfig{}}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleBindingProvider{}); err != nil {
		t.Fatalf("register dynamic provider: %v", err)
	}
	agentMgr := agent.NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := agentMgr.Register(agent.ExperienceCuratorID, agent.ExperienceCuratorConfig(root, config.AgentConfig{Provider: "stub", Model: "stub-model"})); err != nil {
		t.Fatalf("register curator: %v", err)
	}
	gw := New(&config.Config{DataDir: root, Scheduler: config.SchedulerConfig{SystemTasks: config.SystemSchedulerTasksConfig{MemoryCuration: config.SystemTaskConfig{Schedule: "0 * * * *"}, ExperienceCuration: config.SystemTaskConfig{Schedule: "0 3 * * *"}}}}, filepath.Join(root, "config.yaml"), llmReg, agentMgr, nil, nil)
	if err := gw.registerSystemScheduledTasks(); err != nil {
		t.Fatalf("register system tasks: %v", err)
	}
	if err := gw.SetScheduleEnabled(agent.ExperienceCuratorID, "admin", "system:memory-curation", false); err == nil || !strings.Contains(err.Error(), "not user scoped") {
		t.Fatalf("expected not user scoped error disabling system task, got %v", err)
	}
	if err := gw.DeleteSchedule(agent.ExperienceCuratorID, "admin", "system:memory-curation"); err == nil || !strings.Contains(err.Error(), "not user scoped") {
		t.Fatalf("expected not user scoped error deleting system task, got %v", err)
	}
}

func TestSystemReloadConfigUsesGatewayConfigReload(t *testing.T) {
	root := filepath.Join(os.TempDir(), "evoduck-system-reload-config-test")
	_ = os.RemoveAll(root)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	configPath := filepath.Join(root, "config.yaml")
	llmReg, err := llm.NewRegistry(config.LLMConfig{DefaultProvider: "stub", DefaultModel: "stub-model", Providers: map[string]config.ProviderConfig{}}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleBindingProvider{}); err != nil {
		t.Fatalf("register dynamic provider: %v", err)
	}
	agentMgr := agent.NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	gw := New(&config.Config{DataDir: root, DefaultAgent: "admin-bot"}, configPath, llmReg, agentMgr, nil, nil)
	if gw == nil {
		t.Fatal("expected gateway")
	}
	configYAML := `data_dir: "` + filepath.ToSlash(root) + `"
default_agent: admin-bot
llm:
  default_provider: stub
  default_model: stub-model
  providers:
    stub:
      type: stub
      default_model: stub-model
      models:
        - id: stub-model
          name: stub-model
          type: chat
          capabilities:
            vision: false
            reasoning: false
            tool_use: false
          context_window: 0
          max_output_tokens: 0
agents:
  admin-bot:
    workspace: "` + filepath.ToSlash(filepath.Join(root, "agents", "admin-bot")) + `"
    role: admin
    provider: stub
    model: stub-model
channels: {}
shared:
  skills_dir: "` + filepath.ToSlash(filepath.Join(root, "shared", "skills")) + `"
scheduler:
  system_tasks:
    memory_curation:
      schedule: "0 * * * *"
    experience_curation:
      schedule: "0 3 * * *"
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	result, err := agentMgr.ReloadSystem(context.Background(), "config")
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if !strings.Contains(result, "Reloaded config") {
		t.Fatalf("unexpected reload result: %q", result)
	}
	if _, err := agentMgr.Get(agent.ExperienceCuratorID); err != nil {
		t.Fatalf("expected curator after config reload: %v", err)
	}
}

func TestRunSessionInputRequiresExistingIdentifiers(t *testing.T) {
	root := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleBindingProvider{}); err != nil {
		t.Fatalf("register dynamic provider: %v", err)
	}
	agentMgr := agent.NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := agentMgr.Register("agent-test", config.AgentConfig{
		Workspace: filepath.Join(root, "agent-test"),
		Provider:  "stub",
		Model:     "stub-model",
		Role:      string(models.RoleAdmin),
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	gw := New(&config.Config{DataDir: root, DefaultAgent: "agent-test"}, filepath.Join(root, "config.yaml"), llmReg, agentMgr, nil, nil)
	if _, err := gw.runSessionInput(context.Background(), "", "agent:agent-test:user:admin:schedule:task-1", "hello", models.StreamConfig{}); err == nil {
		t.Fatal("expected missing agent id to fail")
	}
	if _, err := gw.runSessionInput(context.Background(), "agent-test", "", "hello", models.StreamConfig{}); err == nil {
		t.Fatal("expected missing session key to fail")
	}
	if _, err := gw.runSessionInput(context.Background(), "agent-test", "agent:agent-test:user:admin:schedule:task-1", "", models.StreamConfig{}); err == nil {
		t.Fatal("expected empty input to fail")
	}
}

func TestCreateScheduleInitializesExecutionSessionKey(t *testing.T) {
	root := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleBindingProvider{}); err != nil {
		t.Fatalf("register dynamic provider: %v", err)
	}

	agentMgr := agent.NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := agentMgr.Register("agent-test", config.AgentConfig{
		Workspace: filepath.Join(root, "agent-test"),
		Provider:  "stub",
		Model:     "stub-model",
		Role:      string(models.RoleAdmin),
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	gw := New(&config.Config{DataDir: root, DefaultAgent: "agent-test"}, filepath.Join(root, "config.yaml"), llmReg, agentMgr, nil, nil)
	created, err := gw.CreateSchedule("agent-test", "admin", models.RoleAdmin, command.CreateScheduleRequest{
		Name:             "task",
		Schedule:         "* * * * *",
		Prompt:           "run in schedule session",
		OriginSessionKey: "agent:agent-test:user:admin:ws",
	})
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	if created.ExecutionSessionKey == "" {
		t.Fatal("expected execution session key to be set")
	}
	expected := "agent:agent-test:user:admin:schedule:" + created.ID
	if created.ExecutionSessionKey != expected {
		t.Fatalf("expected execution session key %q, got %q", expected, created.ExecutionSessionKey)
	}
	sess, err := gw.GetSessionManagerRaw().Get(created.ExecutionSessionKey)
	if err != nil {
		t.Fatalf("expected execution session to be initialized: %v", err)
	}
	if sess.Key != created.ExecutionSessionKey {
		t.Fatalf("expected session key %q, got %q", created.ExecutionSessionKey, sess.Key)
	}
	if got := sess.GetMetadataValue("session_kind"); got != "schedule" {
		t.Fatalf("expected session_kind schedule, got %q", got)
	}
	if got := sess.GetMetadataValue("memory_policy"); got != "ignore" {
		t.Fatalf("expected memory_policy ignore, got %q", got)
	}
	if got := sess.GetMetadataValue("origin_session_key"); got != "agent:agent-test:user:admin:ws" {
		t.Fatalf("expected origin_session_key to be preserved, got %q", got)
	}
	if created.ConcurrencyPolicy != string(scheduler.ConcurrencyPolicySkipIfRunning) {
		t.Fatalf("expected concurrency_policy %q, got %q", scheduler.ConcurrencyPolicySkipIfRunning, created.ConcurrencyPolicy)
	}
}

func TestHandleWSHistoryUsesExplicitScheduleSessionKey(t *testing.T) {
	root := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleBindingProvider{}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	agentMgr := agent.NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := agentMgr.Register("agent-test", config.AgentConfig{
		Workspace: filepath.Join(root, "agent-test"),
		Provider:  "stub",
		Model:     "stub-model",
		Role:      string(models.RoleAdmin),
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	gw := New(&config.Config{DataDir: root, DefaultAgent: "agent-test"}, filepath.Join(root, "config.yaml"), llmReg, agentMgr, nil, nil)

	scheduleSessionKey := "agent:agent-test:user:admin:schedule:task-1"
	sess := gw.GetSessionManagerRaw().GetOrCreate(scheduleSessionKey)
	sess.Append(models.Message{Role: "assistant", Content: "scheduled output"})

	server := httptest.NewServer(gw.withAuth(http.HandlerFunc(gw.handleWebSocket)))
	defer server.Close()

	wsURL := "ws" + server.URL[len("http"):]
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	req := WSMessage{Action: "get_history", AgentID: "agent-test", UserID: "admin", Session: scheduleSessionKey, Limit: 10}
	if err := conn.WriteJSON(req); err != nil {
		t.Fatalf("write history request: %v", err)
	}

	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read history response: %v", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal history response: %v", err)
	}
	messages, ok := payload["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		t.Fatalf("expected history messages, got %#v", payload["messages"])
	}
	message, ok := messages[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected history message object, got %#v", messages[0])
	}
	if got := message["content"]; got != "scheduled output" {
		t.Fatalf("expected schedule session history content, got %#v", got)
	}

	gw.wsConnMu.RLock()
	defer gw.wsConnMu.RUnlock()
	if len(gw.wsConns) != 1 {
		t.Fatalf("expected 1 websocket connection, got %d", len(gw.wsConns))
	}
	for _, bound := range gw.wsConns {
		if bound == nil {
			t.Fatal("expected websocket connection to be tracked")
		}
		if bound.SessKey != scheduleSessionKey {
			t.Fatalf("expected websocket connection to bind to explicit schedule session %q, got %#v", scheduleSessionKey, bound)
		}
	}
}

func TestTriggerHourlyCurationProducesReportableArtifacts(t *testing.T) {
	root := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "curation-report",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("curation-report", &curationReportProvider{}); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	agentMgr := agent.NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := agentMgr.Register("source-agent", config.AgentConfig{Workspace: filepath.Join(root, "agents", "source-agent"), Provider: "curation-report", Model: "stub-model", Role: string(models.RoleAdmin)}); err != nil {
		t.Fatalf("register source agent: %v", err)
	}
	if err := agentMgr.Register(agent.ExperienceCuratorID, agent.ExperienceCuratorConfig(root, config.AgentConfig{Provider: "curation-report", Model: "stub-model"})); err != nil {
		t.Fatalf("register curator: %v", err)
	}
	gw := New(&config.Config{DataDir: root, DefaultAgent: "source-agent"}, filepath.Join(root, "config.yaml"), llmReg, agentMgr, nil, nil)

	sess := gw.GetOrCreateSession("agent:source-agent:user:alice:ws")
	sess.SetUserID("alice")
	sess.SetMetadataValue("agent_id", "source-agent")
	sess.SetMetadataValue("actor_user_id", "alice")
	sess.SetMetadataValue("session_kind", "normal")
	sess.Append(models.Message{Role: "user", Content: "Please remember I prefer concise updates."})
	sess.UpdatedAt = time.Now().Add(-10 * time.Minute)

	record := scheduler.ScheduleRecord{ID: "system:memory-curation", AgentID: agent.ExperienceCuratorID, Metadata: map[string]string{"task_kind": "memory_curation"}}
	if err := gw.executeCuratorSystemTask(record, "memory_curation", "hourly-report"); err != nil {
		t.Fatalf("execute curator hourly task: %v", err)
	}

	dailyPath := filepath.Join(root, "users", "source-agent_user_alice", "memory", "2026-05-08.md")
	data, err := os.ReadFile(dailyPath)
	if err != nil {
		t.Fatalf("read hourly curation result: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "Hourly curation log") || !strings.Contains(content, "concise updates") {
		t.Fatalf("unexpected hourly artifact: %q", content)
	}
	t.Logf("hourly report\n- target: source-agent/alice\n- selected sessions: 1\n- updated files: users/source-agent_user_alice/memory/2026-05-08.md\n- artifact summary: %s", strings.ReplaceAll(strings.TrimSpace(content), "\n", " | "))
}

func TestTriggerDailyCurationProducesReportableArtifacts(t *testing.T) {
	root := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "curation-report",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("curation-report", &curationReportProvider{}); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	agentMgr := agent.NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := agentMgr.Register("source-agent", config.AgentConfig{Workspace: filepath.Join(root, "agents", "source-agent"), Provider: "curation-report", Model: "stub-model", Role: string(models.RoleAdmin)}); err != nil {
		t.Fatalf("register source agent: %v", err)
	}
	if err := agentMgr.Register(agent.ExperienceCuratorID, agent.ExperienceCuratorConfig(root, config.AgentConfig{Provider: "curation-report", Model: "stub-model"})); err != nil {
		t.Fatalf("register curator: %v", err)
	}
	gw := New(&config.Config{DataDir: root, DefaultAgent: "source-agent"}, filepath.Join(root, "config.yaml"), llmReg, agentMgr, nil, nil)

	sess := gw.GetOrCreateSession("agent:source-agent:user:alice:ws")
	sess.SetUserID("alice")
	sess.SetMetadataValue("agent_id", "source-agent")
	sess.SetMetadataValue("actor_user_id", "alice")
	sess.SetMetadataValue("session_kind", "normal")
	sess.Append(models.Message{Role: "user", Content: "Across sessions, keep updates concise and target-bound."})
	sess.UpdatedAt = time.Now().Add(-2 * time.Hour)

	record := scheduler.ScheduleRecord{ID: "system:experience-curation", AgentID: agent.ExperienceCuratorID, Metadata: map[string]string{"task_kind": "experience_curation"}}
	if err := gw.executeCuratorSystemTask(record, "experience_curation", "daily-report"); err != nil {
		t.Fatalf("execute curator daily task: %v", err)
	}

	memoryPath := filepath.Join(root, "users", "source-agent_user_alice", "MEMORY.md")
	agentsPath := filepath.Join(root, "agents", "source-agent", "AGENTS.md")
	memoryData, err := os.ReadFile(memoryPath)
	if err != nil {
		t.Fatalf("read daily MEMORY.md: %v", err)
	}
	agentsData, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("read daily AGENTS.md: %v", err)
	}
	if !strings.Contains(string(memoryData), "Durable user preference: concise updates") {
		t.Fatalf("unexpected daily MEMORY.md: %q", string(memoryData))
	}
	if !strings.Contains(string(agentsData), "Always keep curation summaries concise and target-bound.") {
		t.Fatalf("unexpected daily AGENTS.md: %q", string(agentsData))
	}
	t.Logf("daily report\n- target: source-agent/alice\n- selected sessions: 1\n- updated files: users/source-agent_user_alice/MEMORY.md, agents/source-agent/AGENTS.md\n- MEMORY.md: %s\n- AGENTS.md: %s", strings.TrimSpace(string(memoryData)), strings.TrimSpace(string(agentsData)))
}

func TestTriggerScheduleUsesManualSource(t *testing.T) {
	root := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleBindingProvider{}); err != nil {
		t.Fatalf("register dynamic provider: %v", err)
	}
	agentMgr := agent.NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := agentMgr.Register("agent-test", config.AgentConfig{
		Workspace: filepath.Join(root, "agent-test"),
		Provider:  "stub",
		Model:     "stub-model",
		Role:      string(models.RoleAdmin),
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	gw := New(&config.Config{DataDir: root, DefaultAgent: "agent-test"}, filepath.Join(root, "config.yaml"), llmReg, agentMgr, nil, nil)
	created, err := gw.CreateSchedule("agent-test", "admin", models.RoleAdmin, command.CreateScheduleRequest{
		Name:     "manual-task",
		Schedule: "* * * * *",
		Prompt:   "run manually",
	})
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	if err := gw.TriggerSchedule("agent-test", "admin", created.ID, scheduler.TriggerSourceManual); err != nil {
		t.Fatalf("trigger schedule: %v", err)
	}
	updated, ok := gw.schedulerService.Get(created.ID)
	if !ok {
		t.Fatalf("expected schedule %s to exist", created.ID)
	}
	if updated.LastTriggerSource != scheduler.TriggerSourceManual {
		t.Fatalf("expected last trigger source %q, got %q", scheduler.TriggerSourceManual, updated.LastTriggerSource)
	}
	runs, err := gw.ListScheduleRuns("agent-test", "admin", created.ID, 10)
	if err != nil {
		t.Fatalf("list schedule runs: %v", err)
	}
	if len(runs) == 0 {
		t.Fatal("expected schedule run history to be recorded")
	}
	if runs[0].TriggerSource != string(scheduler.TriggerSourceManual) {
		t.Fatalf("expected run trigger source %q, got %q", scheduler.TriggerSourceManual, runs[0].TriggerSource)
	}
	if runs[0].ScheduleID != created.ID {
		t.Fatalf("expected run schedule id %q, got %q", created.ID, runs[0].ScheduleID)
	}
}

func TestSendSessionMessageDeliversToBoundChannel(t *testing.T) {
	root := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleBindingProvider{}); err != nil {
		t.Fatalf("register dynamic provider: %v", err)
	}
	agentMgr := agent.NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := agentMgr.Register("agent-test", config.AgentConfig{
		Workspace: filepath.Join(root, "agent-test"),
		Provider:  "stub",
		Model:     "stub-model",
		Role:      string(models.RoleAdmin),
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	gw := New(&config.Config{DataDir: root, DefaultAgent: "agent-test"}, filepath.Join(root, "config.yaml"), llmReg, agentMgr, nil, nil)
	bridge := &capturingBridge{name: "wecom-sales"}
	gw.channelMgr = channels.NewManager()
	gw.channelMgr.Register(bridge)

	sess := gw.GetOrCreateSession("wecom:alice")
	sess.SetMetadataValue("delivery_target", "channel")
	sess.SetMetadataValue("channel", "wecom")
	sess.SetMetadataValue("account_id", "wecom-sales")
	sess.SetMetadataValue("sender_id", "alice")
	sess.SetMetadataValue("thread_id", "chat-1")
	sess.SetMetadataValue("context_token", "")
	sess.SetMetadataValue("response_url", "")

	if _, err := gw.SendSessionMessage(context.Background(), sess.Key, "weather update"); err != nil {
		t.Fatalf("send session message: %v", err)
	}
	if bridge.last == nil {
		t.Fatal("expected channel delivery to occur")
	}
	if bridge.last.TargetID != "alice" {
		t.Fatalf("expected target alice, got %q", bridge.last.TargetID)
	}
	if bridge.last.ThreadID != "chat-1" {
		t.Fatalf("expected thread chat-1, got %q", bridge.last.ThreadID)
	}
	if !strings.Contains(bridge.last.Content, "weather update") {
		t.Fatalf("unexpected content: %q", bridge.last.Content)
	}
	msgs := sess.GetMessages()
	if len(msgs) == 0 || msgs[len(msgs)-1].Content != "weather update" {
		t.Fatal("expected session history to keep appended assistant message")
	}
}

func TestSendSessionMessageFallsBackToProactiveChannel(t *testing.T) {
	root := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleBindingProvider{}); err != nil {
		t.Fatalf("register dynamic provider: %v", err)
	}
	agentMgr := agent.NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := agentMgr.Register("agent-test", config.AgentConfig{
		Workspace: filepath.Join(root, "agent-test"),
		Provider:  "stub",
		Model:     "stub-model",
		Role:      string(models.RoleAdmin),
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	gw := New(&config.Config{DataDir: root, DefaultAgent: "agent-test"}, filepath.Join(root, "config.yaml"), llmReg, agentMgr, nil, nil)
	bridge := &capturingBridge{name: "wecom-sales", err: context.DeadlineExceeded}
	gw.channelMgr = channels.NewManager()
	gw.channelMgr.Register(bridge)

	sess := gw.GetOrCreateSession("wecom:alice")
	sess.SetMetadataValue("delivery_target", "channel")
	sess.SetMetadataValue("channel", "wecom")
	sess.SetMetadataValue("account_id", "wecom-sales")
	sess.SetMetadataValue("sender_id", "alice")
	sess.SetMetadataValue("thread_id", "chat-1")
	sess.SetMetadataValue("context_token", "req-1")
	sess.SetMetadataValue("response_url", "https://wecom.example/reply")

	if _, err := gw.SendSessionMessage(context.Background(), sess.Key, "weather update"); err != nil {
		t.Fatalf("send session message: %v", err)
	}
	if len(bridge.sent) != 2 {
		t.Fatalf("expected 2 send attempts, got %d", len(bridge.sent))
	}
	if bridge.sent[0].ContextToken == "" {
		t.Fatal("expected first attempt to use reply context token")
	}
	if bridge.sent[1].ContextToken != "" || bridge.sent[1].ResponseURL != "" {
		t.Fatalf("expected fallback send to clear short-lived reply fields, got %#v", bridge.sent[1])
	}
	if bridge.last == nil {
		t.Fatal("expected fallback proactive send to succeed")
	}
}

func TestSendSessionMessageDoesNotFallbackWithoutProactiveSupport(t *testing.T) {
	root := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleBindingProvider{}); err != nil {
		t.Fatalf("register dynamic provider: %v", err)
	}
	agentMgr := agent.NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := agentMgr.Register("agent-test", config.AgentConfig{
		Workspace: filepath.Join(root, "agent-test"),
		Provider:  "stub",
		Model:     "stub-model",
		Role:      string(models.RoleAdmin),
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	gw := New(&config.Config{DataDir: root, DefaultAgent: "agent-test"}, filepath.Join(root, "config.yaml"), llmReg, agentMgr, nil, nil)
	bridge := &capturingBridge{name: "weixin-cs", err: context.DeadlineExceeded}
	gw.channelMgr = channels.NewManager()
	gw.channelMgr.Register(bridge)

	sess := gw.GetOrCreateSession("weixin:alice")
	sess.SetMetadataValue("delivery_target", "channel")
	sess.SetMetadataValue("channel", "weixin")
	sess.SetMetadataValue("account_id", "weixin-cs")
	sess.SetMetadataValue("sender_id", "alice")
	sess.SetMetadataValue("thread_id", "chat-1")
	sess.SetMetadataValue("context_token", "ctx-1")

	if _, err := gw.SendSessionMessage(context.Background(), sess.Key, "weather update"); err == nil {
		t.Fatal("expected non-proactive channel send to fail")
	}
	if len(bridge.sent) != 1 {
		t.Fatalf("expected exactly 1 send attempt, got %d", len(bridge.sent))
	}
}

func TestSendSessionMediaMessageDeliversMediaToChannel(t *testing.T) {
	root := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleBindingProvider{}); err != nil {
		t.Fatalf("register dynamic provider: %v", err)
	}
	agentMgr := agent.NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := agentMgr.Register("agent-test", config.AgentConfig{
		Workspace: filepath.Join(root, "agent-test"),
		Provider:  "stub",
		Model:     "stub-model",
		Role:      string(models.RoleAdmin),
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	gw := New(&config.Config{DataDir: root, DefaultAgent: "agent-test"}, filepath.Join(root, "config.yaml"), llmReg, agentMgr, nil, nil)
	bridge := &capturingBridge{name: "weixin-cs"}
	gw.channelMgr = channels.NewManager()
	gw.channelMgr.Register(bridge)

	sess := gw.GetOrCreateSession("weixin:alice")
	sess.SetMetadataValue("delivery_target", "channel")
	sess.SetMetadataValue("channel", "weixin")
	sess.SetMetadataValue("account_id", "weixin-cs")
	sess.SetMetadataValue("sender_id", "alice")
	sess.SetMetadataValue("thread_id", "chat-1")
	sess.SetMetadataValue("context_token", "ctx-1")

	media := []models.OutgoingMedia{{Type: "image", Name: "demo.png", EncryptQueryParam: "enc=image", AESKey: "aes-image"}}
	if _, err := gw.SendSessionMediaMessage(context.Background(), sess.Key, "see attachment", media); err != nil {
		t.Fatalf("send session media message: %v", err)
	}
	if bridge.last == nil {
		t.Fatal("expected media channel delivery to occur")
	}
	if got := len(bridge.last.Media); got != 1 {
		t.Fatalf("expected 1 media item, got %d", got)
	}
	if got := bridge.last.Media[0].Type; got != "image" {
		t.Fatalf("expected image media type, got %q", got)
	}
	if got := bridge.last.Media[0].EncryptQueryParam; got != "enc=image" {
		t.Fatalf("unexpected media encrypt query param: %q", got)
	}
	msgs := sess.GetMessages()
	if len(msgs) == 0 {
		t.Fatal("expected session history to contain assistant media message")
	}
	last := msgs[len(msgs)-1].Content
	if !strings.Contains(last, "see attachment") || !strings.Contains(last, "[image: demo.png]") {
		t.Fatalf("unexpected session history content: %q", last)
	}
}

func TestSessionOutgoingDisplayContentSummarizesMedia(t *testing.T) {
	got := sessionOutgoingDisplayContent(&models.OutgoingMessage{
		Content: "see attachment",
		Media:   []models.OutgoingMedia{{Type: "image", Name: "demo.png"}, {Type: "audio"}},
	})
	if !strings.Contains(got, "see attachment") || !strings.Contains(got, "[image: demo.png]") || !strings.Contains(got, "[audio]") {
		t.Fatalf("unexpected display content: %q", got)
	}
	if got := sessionOutgoingDisplayContent(&models.OutgoingMessage{}); got != "" {
		t.Fatalf("expected empty display content, got %q", got)
	}
}

func TestSendSessionMediaMessageDeliversMediaToWebSocketSessions(t *testing.T) {
	gw := New(&config.Config{DataDir: t.TempDir(), DefaultAgent: "agent-test"}, "", nil, nil, nil, nil)
	server := httptest.NewServer(gw.withAuth(http.HandlerFunc(gw.handleWebSocket)))
	defer server.Close()

	wsURL := "ws" + server.URL[len("http"):]
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	sessionKey := "agent:agent-test:user:alice:ws"
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		bound := false
		gw.wsConnMu.Lock()
		for _, wsConn := range gw.wsConns {
			if wsConn != nil {
				wsConn.SessKey = sessionKey
				wsConn.UserID = "alice"
				wsConn.AgentID = "agent-test"
				bound = true
			}
		}
		gw.wsConnMu.Unlock()
		if bound {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	verified := false
	gw.wsConnMu.RLock()
	for _, wsConn := range gw.wsConns {
		if wsConn != nil && wsConn.SessKey == sessionKey {
			verified = true
			break
		}
	}
	gw.wsConnMu.RUnlock()
	if !verified {
		t.Fatal("expected websocket session binding to be registered")
	}

	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}

	media := []models.OutgoingMedia{{Type: "image", Name: "demo.png", URL: "https://example.com/demo.png"}}
	if _, err := gw.SendSessionMediaMessage(context.Background(), sessionKey, "see attachment", media); err != nil {
		t.Fatalf("send session media message: %v", err)
	}

	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read websocket response: %v", err)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal websocket response: %v", err)
	}
	if resp["type"] != "message" {
		t.Fatalf("expected message response, got %#v", resp["type"])
	}
	mediaPayload, ok := resp["media"].([]interface{})
	if !ok || len(mediaPayload) != 1 {
		t.Fatalf("expected media payload, got %#v", resp["media"])
	}
	first, _ := mediaPayload[0].(map[string]interface{})
	if got := first["type"]; got != "image" {
		t.Fatalf("unexpected media type: %#v", got)
	}
}

func TestSendSessionMediaMessageNormalizesInlineMediaForWebSocketSessions(t *testing.T) {
	gw := New(&config.Config{DataDir: t.TempDir(), DefaultAgent: "agent-test"}, "", nil, nil, nil, nil)
	server := httptest.NewServer(gw.withAuth(http.HandlerFunc(gw.handleWebSocket)))
	defer server.Close()

	wsURL := "ws" + server.URL[len("http"):]
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	sessionKey := "agent:agent-test:user:alice:ws"
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		bound := false
		gw.wsConnMu.Lock()
		for _, wsConn := range gw.wsConns {
			if wsConn != nil {
				wsConn.SessKey = sessionKey
				wsConn.UserID = "alice"
				wsConn.AgentID = "agent-test"
				bound = true
			}
		}
		gw.wsConnMu.Unlock()
		if bound {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}

	media := []models.OutgoingMedia{{Type: "image", Name: "demo.txt", Data: base64.StdEncoding.EncodeToString([]byte("hello media"))}}
	if _, err := gw.SendSessionMediaMessage(context.Background(), sessionKey, "see attachment", media); err != nil {
		t.Fatalf("send session media message: %v", err)
	}

	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read websocket response: %v", err)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal websocket response: %v", err)
	}
	mediaPayload, ok := resp["media"].([]interface{})
	if !ok || len(mediaPayload) != 1 {
		t.Fatalf("expected media payload, got %#v", resp["media"])
	}
	first, _ := mediaPayload[0].(map[string]interface{})
	if got := first["url"]; got == nil || !strings.HasPrefix(got.(string), "/media/") {
		t.Fatalf("expected normalized media url, got %#v", got)
	}
	if got := first["data"]; got != nil {
		t.Fatalf("expected websocket media data to be omitted, got %#v", got)
	}
}

func TestWSMessageSupportsMediaPayload(t *testing.T) {
	raw := []byte(`{"action":"chat","message":"see attachment","media":[{"type":"image","name":"demo.png","url":"https://example.com/demo.png"}]}`)
	var wsMsg WSMessage
	if err := json.Unmarshal(raw, &wsMsg); err != nil {
		t.Fatalf("unmarshal ws message: %v", err)
	}
	if wsMsg.Message != "see attachment" {
		t.Fatalf("unexpected message content: %q", wsMsg.Message)
	}
	if len(wsMsg.Media) != 1 || wsMsg.Media[0].Type != "image" {
		t.Fatalf("unexpected ws media payload: %#v", wsMsg.Media)
	}
}

func TestWebSocketCancelDuringActiveStreamOnSameConnection(t *testing.T) {
	root := t.TempDir()
	provider := &cancellableStreamProvider{started: make(chan string, 4)}
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "cancellable",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("cancellable", provider); err != nil {
		t.Fatalf("register dynamic provider: %v", err)
	}

	agentMgr := agent.NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := agentMgr.Register("agent-test", config.AgentConfig{
		Workspace: filepath.Join(root, "agent-test"),
		Provider:  "cancellable",
		Model:     "stub-model",
		Role:      string(models.RoleAdmin),
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	gw := New(&config.Config{DataDir: root, DefaultAgent: "agent-test"}, filepath.Join(root, "config.yaml"), llmReg, agentMgr, nil, nil)
	server := httptest.NewServer(gw.withAuth(http.HandlerFunc(gw.handleWebSocket)))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?user_id=admin"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	sessionKey := "agent:agent-test:user:admin:ws"
	if err := conn.WriteJSON(WSMessage{Action: "stream", AgentID: "agent-test", UserID: "admin", Session: sessionKey, Message: "run until cancelled"}); err != nil {
		t.Fatalf("write stream request: %v", err)
	}

	select {
	case got := <-provider.started:
		if got != "run until cancelled" {
			t.Fatalf("unexpected started content: %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for provider stream to start")
	}

	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	_, firstRaw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read first stream response: %v", err)
	}
	var firstResp map[string]interface{}
	if err := json.Unmarshal(firstRaw, &firstResp); err != nil {
		t.Fatalf("unmarshal first stream response: %v", err)
	}
	if firstResp["type"] != "content" {
		t.Fatalf("expected first stream response type content, got %#v", firstResp["type"])
	}

	if err := conn.WriteJSON(WSMessage{Action: "cancel", AgentID: "agent-test", UserID: "admin", Session: sessionKey}); err != nil {
		t.Fatalf("write cancel request: %v", err)
	}

	seenCancelled := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read post-cancel response: %v", err)
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(raw, &resp); err != nil {
			t.Fatalf("unmarshal post-cancel response: %v", err)
		}
		if resp["type"] == "cancelled" {
			seenCancelled = true
			break
		}
	}
	if !seenCancelled {
		t.Fatal("expected cancelled response on same websocket connection")
	}

	endDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(endDeadline) {
		gw.activeTasksMu.RLock()
		_, running := gw.activeTasks[sessionKey]
		gw.activeTasksMu.RUnlock()
		if !running {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("expected active task to be cleaned up after cancellation")
}

func TestWebSocketSecondStreamSupersedesFirstOnSameSession(t *testing.T) {
	root := t.TempDir()
	provider := &cancellableStreamProvider{started: make(chan string, 4)}
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "cancellable",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("cancellable", provider); err != nil {
		t.Fatalf("register dynamic provider: %v", err)
	}

	agentMgr := agent.NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := agentMgr.Register("agent-test", config.AgentConfig{
		Workspace: filepath.Join(root, "agent-test"),
		Provider:  "cancellable",
		Model:     "stub-model",
		Role:      string(models.RoleAdmin),
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	gw := New(&config.Config{DataDir: root, DefaultAgent: "agent-test"}, filepath.Join(root, "config.yaml"), llmReg, agentMgr, nil, nil)
	server := httptest.NewServer(gw.withAuth(http.HandlerFunc(gw.handleWebSocket)))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?user_id=admin"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	sessionKey := "agent:agent-test:user:admin:ws"
	if err := conn.WriteJSON(WSMessage{Action: "stream", AgentID: "agent-test", UserID: "admin", Session: sessionKey, Message: "first run"}); err != nil {
		t.Fatalf("write first stream request: %v", err)
	}
	select {
	case got := <-provider.started:
		if got != "first run" {
			t.Fatalf("unexpected first started content: %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for first stream to start")
	}

	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	_, firstRaw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read first stream response: %v", err)
	}
	var firstResp map[string]interface{}
	if err := json.Unmarshal(firstRaw, &firstResp); err != nil {
		t.Fatalf("unmarshal first stream response: %v", err)
	}
	if firstResp["content"] != "first run" {
		t.Fatalf("expected first stream content, got %#v", firstResp["content"])
	}

	if err := conn.WriteJSON(WSMessage{Action: "stream", AgentID: "agent-test", UserID: "admin", Session: sessionKey, Message: "second run"}); err != nil {
		t.Fatalf("write second stream request: %v", err)
	}
	select {
	case got := <-provider.started:
		if got != "second run" {
			t.Fatalf("unexpected second started content: %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for second stream to start")
	}

	deadline := time.Now().Add(5 * time.Second)
	seenSecond := false
	for time.Now().Before(deadline) {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read superseded stream response: %v", err)
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(raw, &resp); err != nil {
			t.Fatalf("unmarshal superseded stream response: %v", err)
		}
		if resp["content"] == "second run" {
			seenSecond = true
			break
		}
		if resp["type"] == "cancelled" {
			t.Fatalf("did not expect cancelled event from superseded first run: %#v", resp)
		}
	}
	if !seenSecond {
		t.Fatal("expected second stream output to arrive after superseding first run")
	}

	endDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(endDeadline) {
		gw.activeTasksMu.RLock()
		task := gw.activeTasks[sessionKey]
		gw.activeTasksMu.RUnlock()
		if task != nil && task.RunID != "" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("expected second run to remain registered as active task")
}

func TestHandleChannelMessageTogglesTypingState(t *testing.T) {
	root := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleBindingProvider{}); err != nil {
		t.Fatalf("register dynamic provider: %v", err)
	}

	agentMgr := agent.NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := agentMgr.Register("agent-test", config.AgentConfig{
		Workspace: filepath.Join(root, "agent-test"),
		Provider:  "stub",
		Model:     "stub-model",
		Role:      string(models.RoleAdmin),
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	gw := New(&config.Config{DataDir: root, DefaultAgent: "agent-test"}, filepath.Join(root, "config.yaml"), llmReg, agentMgr, nil, nil)
	bridge := &capturingBridge{name: "weixin-cs"}
	gw.channelMgr = channels.NewManager()
	gw.channelMgr.Register(bridge)
	gw.router = router.New(agentMgr, map[string]config.ChannelConfig{
		"weixin-cs": {Agent: "agent-test"},
	}, "agent-test")

	msg := &models.NormalizedMessage{
		Channel:      "weixin",
		AccountID:    "weixin-cs",
		SenderID:     "alice",
		UserID:       "alice",
		Content:      "hello",
		ThreadID:     "chat-1",
		ContextToken: "ctx-1",
	}

	gw.handleChannelMessage(msg)

	if len(bridge.typingStates) != 0 {
		t.Fatalf("expected gateway not to toggle typing directly, got %#v", bridge.typingStates)
	}
}

func TestHandleWSHistoryBindsConnectionToResolvedSession(t *testing.T) {
	root := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleBindingProvider{}); err != nil {
		t.Fatalf("register dynamic provider: %v", err)
	}

	agentMgr := agent.NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := agentMgr.Register("agent-test", config.AgentConfig{
		Workspace: filepath.Join(root, "agent-test"),
		Provider:  "stub",
		Model:     "stub-model",
		Role:      string(models.RoleAdmin),
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	gw := New(&config.Config{DataDir: root, DefaultAgent: "agent-test"}, filepath.Join(root, "config.yaml"), llmReg, agentMgr, nil, nil)

	server := httptest.NewServer(gw.withAuth(http.HandlerFunc(gw.handleWebSocket)))
	defer server.Close()

	wsURL := "ws" + server.URL[len("http"):]
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	req := WSMessage{Action: "get_history", AgentID: "agent-test", UserID: "admin", Limit: 10}
	if err := conn.WriteJSON(req); err != nil {
		t.Fatalf("write history request: %v", err)
	}

	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read history response: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal history response: %v", err)
	}
	if resp["type"] != "history" {
		t.Fatalf("expected history response, got %v", resp["type"])
	}

	expectedSessionKey := "agent:agent-test:user:admin:ws"
	gw.wsConnMu.RLock()
	defer gw.wsConnMu.RUnlock()
	if len(gw.wsConns) != 1 {
		t.Fatalf("expected 1 websocket connection, got %d", len(gw.wsConns))
	}
	for _, wsConn := range gw.wsConns {
		if wsConn == nil {
			t.Fatal("expected websocket connection to be tracked")
		}
		if wsConn.SessKey != expectedSessionKey {
			t.Fatalf("expected session key %q, got %q", expectedSessionKey, wsConn.SessKey)
		}
		if wsConn.AgentID != "agent-test" {
			t.Fatalf("expected agent id agent-test, got %q", wsConn.AgentID)
		}
		if wsConn.UserID != "admin" {
			t.Fatalf("expected user id admin, got %q", wsConn.UserID)
		}
	}
}

func schedulerRecordForTest(agentID, userID, scheduleID, originSessionKey, executionSessionKey string) scheduler.ScheduleRecord {
	return scheduler.ScheduleRecord{
		ID:                  scheduleID,
		Scope:               scheduler.ScheduleScopeUser,
		AgentID:             agentID,
		UserID:              userID,
		Name:                "test schedule",
		Schedule:            "* * * * *",
		Prompt:              "run in origin session",
		Enabled:             true,
		OriginSessionKey:    originSessionKey,
		ExecutionSessionKey: executionSessionKey,
	}
}
