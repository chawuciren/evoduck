package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chawuciren/evoduck/internal/command"
	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/internal/scheduler"
	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/internal/tools"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
)

type scheduleTestProvider struct{}

type capturingScheduleManager struct {
	lastReq command.CreateScheduleRequest
}

type capturingSessionGateway struct {
	sessions map[string]*session.Session
}

func (p *scheduleTestProvider) Name() string { return "stub" }

func (p *scheduleTestProvider) Chat(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (*models.Response, error) {
	return nil, nil
}

func (p *scheduleTestProvider) ChatWithOptions(_ context.Context, _ []models.Message, _ []models.ToolDefinition, _ llm.ChatOptions) (*models.Response, error) {
	return nil, nil
}

func (p *scheduleTestProvider) ChatStream(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	ch := make(chan models.StreamEvent)
	close(ch)
	return ch, nil
}

func (p *scheduleTestProvider) SetDefaultOptions(_ llm.ChatOptions) {}
func (p *scheduleTestProvider) GetMaxContextTokens() int            { return 8192 }
func (p *scheduleTestProvider) BuiltinModels() []llm.ProviderModel  { return nil }
func (p *scheduleTestProvider) FetchModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}
func (p *scheduleTestProvider) ListModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}

func TestExperienceCuratorConfigNormalizesDataDir(t *testing.T) {
	cfg := ExperienceCuratorConfig("data", config.AgentConfig{Provider: "stub", Model: "stub-model"})
	if len(cfg.Permissions.AuthorizedDirectories) != 1 {
		t.Fatalf("expected one authorized directory, got %#v", cfg.Permissions.AuthorizedDirectories)
	}
	dataDir := cfg.Permissions.AuthorizedDirectories[0]
	if !filepath.IsAbs(dataDir) {
		t.Fatalf("expected curator data dir to be absolute, got %q", dataDir)
	}
	if cfg.Workspace != filepath.Join(dataDir, "agents", ExperienceCuratorID) {
		t.Fatalf("expected workspace under absolute data dir, got %q", cfg.Workspace)
	}
	if strings.Contains(filepath.ToSlash(cfg.Workspace), "/agents/experience-curator/data") {
		t.Fatalf("workspace must not nest data under curator workspace: %q", cfg.Workspace)
	}
}

func (m *capturingScheduleManager) GetDefaultAgentID() string { return "agent-test" }
func (m *capturingScheduleManager) ListSchedules(agentID, userID string) []command.ScheduleInfo {
	return nil
}
func (m *capturingScheduleManager) CreateSchedule(agentID, userID string, role models.Role, req command.CreateScheduleRequest) (*command.ScheduleInfo, error) {
	m.lastReq = req
	return &command.ScheduleInfo{ID: "sched-1", AgentID: agentID, UserID: userID, Name: req.Name, Schedule: req.Schedule, Prompt: req.Prompt, Enabled: req.Enabled == nil || *req.Enabled, OriginSessionKey: req.OriginSessionKey}, nil
}
func (m *capturingScheduleManager) SetScheduleEnabled(agentID, userID, id string, enabled bool) error {
	return nil
}
func (m *capturingScheduleManager) DeleteSchedule(agentID, userID, id string) error {
	return nil
}
func (m *capturingScheduleManager) TriggerSchedule(agentID, userID, id string, source scheduler.TriggerSource) error {
	return nil
}

func (g *capturingSessionGateway) List() []session.SessionInfo {
	items := make([]session.SessionInfo, 0, len(g.sessions))
	for _, sess := range g.sessions {
		items = append(items, session.SessionInfo{Key: sess.Key, MessageCount: sess.MessageCount(), UpdatedAt: time.Now()})
	}
	return items
}

func (g *capturingSessionGateway) Get(key string) (*session.Session, error) {
	if sess, ok := g.sessions[key]; ok {
		return sess, nil
	}
	return nil, context.DeadlineExceeded
}

func (g *capturingSessionGateway) GetOrCreate(key string) *session.Session {
	if sess, ok := g.sessions[key]; ok {
		return sess
	}
	sess := session.NewSession(key, key, nil)
	g.sessions[key] = sess
	return sess
}

func (g *capturingSessionGateway) SendSessionMessage(ctx context.Context, sessionKey string, content string) (int, error) {
	g.GetOrCreate(sessionKey).Append(models.Message{Role: "assistant", Content: content})
	return 1, nil
}

func (g *capturingSessionGateway) SendSessionOutgoingMessage(ctx context.Context, sessionKey string, outgoing *models.OutgoingMessage) (int, error) {
	content := ""
	if outgoing != nil {
		content = outgoing.Content
	}
	g.GetOrCreate(sessionKey).Append(models.Message{Role: "assistant", Content: content})
	return 1, nil
}

func (g *capturingSessionGateway) RunSessionInput(ctx context.Context, agentID, sessionKey, input string) error {
	g.GetOrCreate(sessionKey).Append(models.Message{Role: "user", Content: input})
	g.GetOrCreate(sessionKey).Append(models.Message{Role: "assistant", Content: "ok"})
	return nil
}

func TestRegisterAddsScheduleTools(t *testing.T) {
	workspace := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleTestProvider{}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	mgr := NewManager(llmReg, workspace, workspace, config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := mgr.Register("agent-test", config.AgentConfig{
		Workspace: workspace,
		Provider:  "stub",
		Model:     "stub-model",
		Role:      string(models.RoleAdmin),
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	ag, err := mgr.Get("agent-test")
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}

	toolNames := []string{
		"schedule_list",
		"schedule_create",
		"schedule_enable",
		"schedule_disable",
		"schedule_delete",
		"system_reload",
	}
	for _, name := range toolNames {
		if _, err := ag.Tools.Get(name); err != nil {
			t.Fatalf("expected tool %s to be registered: %v", name, err)
		}
	}
}

func TestSystemReloadToolReloadsSkills(t *testing.T) {
	workspace := t.TempDir()
	sharedSkillsDir := filepath.Join(workspace, "shared", "skills")
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleTestProvider{}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	mgr := NewManager(llmReg, workspace, sharedSkillsDir, config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := mgr.Register("agent-test", config.AgentConfig{
		Workspace: workspace,
		Provider:  "stub",
		Model:     "stub-model",
		Role:      string(models.RoleAdmin),
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	ag, err := mgr.Get("agent-test")
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}

	if _, err := ag.Skills.Get("fresh-skill"); err != nil {
		t.Fatalf("get fresh skill before create: %v", err)
	} else if err == nil {
		// keep assertion explicit below with value check
	}
	if s, _ := ag.Skills.Get("fresh-skill"); s != nil {
		t.Fatalf("did not expect fresh-skill before reload")
	}

	skillDir := filepath.Join(sharedSkillsDir, "fresh-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: fresh-skill
description: Freshly created skill
---
Fresh instructions.`), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	result, err := ag.Tools.ExecuteWithRole(context.Background(), "system_reload", map[string]interface{}{"scope": "skills"}, models.RoleAdmin)
	if err != nil {
		t.Fatalf("execute system_reload: %v", err)
	}
	if !strings.Contains(result, "Reloaded skills") {
		t.Fatalf("unexpected reload result: %q", result)
	}
	if s, err := ag.Skills.Get("fresh-skill"); err != nil || s == nil {
		t.Fatalf("expected fresh-skill after reload, skill=%v err=%v", s, err)
	}
}

func TestSystemReloadAllRestoresExperienceCuratorScaffold(t *testing.T) {
	workspace := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleTestProvider{}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	mgr := NewManager(llmReg, workspace, filepath.Join(workspace, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	mgr.SetConfigReloader(func(context.Context) (string, error) { return "Reloaded config.", nil })
	if err := mgr.Register(ExperienceCuratorID, ExperienceCuratorConfig(workspace, config.AgentConfig{Provider: "stub", Model: "stub-model"})); err != nil {
		t.Fatalf("register curator: %v", err)
	}
	ag, err := mgr.Get(ExperienceCuratorID)
	if err != nil {
		t.Fatalf("get curator: %v", err)
	}
	agentsPath := filepath.Join(ag.Config.Workspace, "AGENTS.md")
	soulPath := filepath.Join(ag.Config.Workspace, "SOUL.md")
	if err := os.Remove(agentsPath); err != nil {
		t.Fatalf("remove AGENTS.md: %v", err)
	}
	if err := os.Remove(soulPath); err != nil {
		t.Fatalf("remove SOUL.md: %v", err)
	}

	result, err := mgr.ReloadSystem(context.Background(), "all")
	if err != nil {
		t.Fatalf("reload all: %v", err)
	}
	if !strings.Contains(result, "Restored 1 system scaffold") {
		t.Fatalf("unexpected reload result: %q", result)
	}
	if _, err := os.Stat(agentsPath); err != nil {
		t.Fatalf("expected AGENTS.md restored: %v", err)
	}
	if _, err := os.Stat(soulPath); err != nil {
		t.Fatalf("expected SOUL.md restored: %v", err)
	}
}

func TestScheduleCreateToolBindsCurrentSessionKey(t *testing.T) {
	manager := &capturingScheduleManager{}
	tool := tools.NewScheduleCreateTool("agent-test", func() tools.ScheduleManager { return manager })
	enabled := true
	ctx := tools.WithSessionKey(context.Background(), "agent:agent-test:user:admin:ws")

	_, err := tool.ExecuteWithUserContext(ctx, map[string]interface{}{
		"name":     "minute-test",
		"schedule": "* * * * *",
		"prompt":   "run something",
		"enabled":  enabled,
	}, models.RoleAdmin, "admin", true, t.TempDir())
	if err != nil {
		t.Fatalf("execute schedule_create: %v", err)
	}

	if strings.TrimSpace(manager.lastReq.OriginSessionKey) != "agent:agent-test:user:admin:ws" {
		t.Fatalf("expected origin session key to be bound, got %q", manager.lastReq.OriginSessionKey)
	}
}

func TestAdminAlwaysGetsSessionTools(t *testing.T) {
	workspace := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleTestProvider{}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	mgr := NewManager(llmReg, workspace, workspace, config.BackendCallConfig{}, config.SessionToolConfig{Enabled: false}, config.MemoryConfig{}, nil, nil, nil)
	mgr.SetSessionGateway(&capturingSessionGateway{sessions: map[string]*session.Session{}})
	if err := mgr.Register("agent-test", config.AgentConfig{
		Workspace: workspace,
		Provider:  "stub",
		Model:     "stub-model",
		Role:      string(models.RoleAdmin),
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	ag, err := mgr.Get("agent-test")
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	for _, name := range []string{"sessions_list", "sessions_history", "sessions_send", "sessions_run"} {
		if _, err := ag.Tools.Get(name); err != nil {
			t.Fatalf("expected admin tool %s to be registered: %v", name, err)
		}
	}
}

func TestSessionToolsResolveGatewayAfterRegistration(t *testing.T) {
	workspace := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleTestProvider{}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	mgr := NewManager(llmReg, workspace, workspace, config.BackendCallConfig{}, config.SessionToolConfig{Enabled: true}, config.MemoryConfig{}, nil, nil, nil)
	if err := mgr.Register("agent-test", config.AgentConfig{
		Workspace: workspace,
		Provider:  "stub",
		Model:     "stub-model",
		Role:      string(models.RoleAdmin),
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	gateway := &capturingSessionGateway{sessions: map[string]*session.Session{}}
	mgr.SetSessionGateway(gateway)
	ag, err := mgr.Get("agent-test")
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}

	ag.Tools.SetUserContext("alice", true, workspace)
	ctx := tools.WithSessionKey(context.Background(), "agent:agent-test:user:alice:schedule:task-1")
	result, err := ag.Tools.ExecuteWithRole(ctx, "sessions_send", map[string]interface{}{
		"session_key": "agent:agent-test:user:alice:ws",
		"message":     "weather update",
	}, models.RoleAdmin)
	if err != nil {
		t.Fatalf("execute sessions_send: %v", err)
	}
	if !strings.Contains(result, "Sent message to session") {
		t.Fatalf("unexpected result: %q", result)
	}
	if len(gateway.sessions["agent:agent-test:user:alice:ws"].GetMessages()) == 0 {
		t.Fatal("expected target session to receive message")
	}
}

func TestEmployeeToolOverrideRestrictsDefaultTools(t *testing.T) {
	workspace := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleTestProvider{}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	mgr := NewManager(llmReg, workspace, workspace, config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := mgr.Register("agent-test", config.AgentConfig{
		Workspace: workspace,
		Provider:  "stub",
		Model:     "stub-model",
		Role:      string(models.RoleEmployee),
		Permissions: config.AgentPermissionConfig{
			AuthorizedTools: []string{"file_read"},
		},
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	ag, err := mgr.Get("agent-test")
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if _, err := ag.Tools.Get("file_read"); err != nil {
		t.Fatalf("expected file_read to be registered: %v", err)
	}
	if _, err := ag.Tools.Get("time"); err == nil {
		t.Fatal("expected time to be hidden by tool override")
	}
	if _, err := ag.Tools.Get("file_write"); err == nil {
		t.Fatal("expected file_write to be hidden by tool override")
	}
}

func TestSubagentToolRegistrationByRole(t *testing.T) {
	workspace := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleTestProvider{}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	mgr := NewManager(llmReg, workspace, workspace, config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	for _, tc := range []struct {
		agentID string
		role    models.Role
	}{
		{agentID: "agent-customer", role: models.RoleCustomer},
		{agentID: "agent-employee", role: models.RoleEmployee},
	} {
		if err := mgr.Register(tc.agentID, config.AgentConfig{
			Workspace: workspace,
			Provider:  "stub",
			Model:     "stub-model",
			Role:      string(tc.role),
		}); err != nil {
			t.Fatalf("register %s: %v", tc.agentID, err)
		}
	}

	userAgent, err := mgr.Get("agent-customer")
	if err != nil {
		t.Fatalf("get user agent: %v", err)
	}
	for _, name := range []string{"subagent_list", "subagent_status", "subagent_result"} {
		if _, err := userAgent.Tools.Get(name); err != nil {
			t.Fatalf("expected default subagent tool %s for user role: %v", name, err)
		}
	}
	for _, name := range []string{"subagent_start_internal", "subagent_start_external", "subagent_cancel"} {
		if _, err := userAgent.Tools.Get(name); err == nil {
			t.Fatalf("did not expect privileged subagent tool %s for user role", name)
		}
	}

	employeeAgent, err := mgr.Get("agent-employee")
	if err != nil {
		t.Fatalf("get employee agent: %v", err)
	}
	for _, name := range []string{"subagent_list", "subagent_status", "subagent_result", "subagent_start_internal", "subagent_start_external", "subagent_cancel"} {
		if _, err := employeeAgent.Tools.Get(name); err != nil {
			t.Fatalf("expected subagent tool %s for employee role: %v", name, err)
		}
	}
}

func TestSubagentToolOverrideKeepsAllowlistedEntries(t *testing.T) {
	workspace := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleTestProvider{}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	mgr := NewManager(llmReg, workspace, workspace, config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := mgr.Register("agent-employee-allowlist", config.AgentConfig{
		Workspace: workspace,
		Provider:  "stub",
		Model:     "stub-model",
		Role:      string(models.RoleEmployee),
		Permissions: config.AgentPermissionConfig{
			AuthorizedTools: []string{"subagent_status", "subagent_cancel"},
		},
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	ag, err := mgr.Get("agent-employee-allowlist")
	if err != nil {
		t.Fatalf("get employee agent: %v", err)
	}
	if _, err := ag.Tools.Get("subagent_status"); err != nil {
		t.Fatalf("expected allowlisted tool subagent_status: %v", err)
	}
	if _, err := ag.Tools.Get("subagent_cancel"); err != nil {
		t.Fatalf("expected allowlisted tool subagent_cancel: %v", err)
	}
	if _, err := ag.Tools.Get("subagent_list"); err == nil {
		t.Fatal("expected non-allowlisted subagent_list to be hidden")
	}
	if _, err := ag.Tools.Get("time"); err == nil {
		t.Fatal("expected non-allowlisted time tool to be hidden")
	}
}

func TestBuildSourceContextCurationPromptIncludesTargetArtifacts(t *testing.T) {
	root := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleTestProvider{}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	mgr := NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := mgr.Register("agent-test", config.AgentConfig{
		Workspace: filepath.Join(root, "agents", "agent-test"),
		Provider:  "stub",
		Model:     "stub-model",
		Role:      string(models.RoleAdmin),
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	userDir := filepath.Join(root, "users", "agent-test_user_alice")
	memoryDir := filepath.Join(userDir, "memory")
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		t.Fatalf("mkdir memory dir: %v", err)
	}
	agentDir := filepath.Join(root, "agents", "agent-test")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	for path, content := range map[string]string{
		filepath.Join(memoryDir, "2026-05-07.md"): "recent daily note 1",
		filepath.Join(memoryDir, "2026-05-08.md"): "recent daily note 2",
		filepath.Join(userDir, "USER.md"):         "preferred name: Alice",
		filepath.Join(userDir, "MEMORY.md"):       "durable preference: concise replies",
		filepath.Join(agentDir, "AGENTS.md"):      "agent rule: verify targets before writing",
		filepath.Join(agentDir, "SOUL.md"):        "mission: keep memory tidy",
		filepath.Join(agentDir, "TOOLS.md"):       "tool rule: prefer memory tools",
		filepath.Join(agentDir, "IDENTITY.md"):    "identity note",
		filepath.Join(agentDir, "HEARTBEAT.md"):   "heartbeat note",
		filepath.Join(agentDir, "BOOTSTRAP.md"):   "bootstrap note",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	sess := session.NewSession("agent:agent-test:user:alice:ws", "sess-1", nil)
	sess.SetUserID("alice")
	sess.SetMetadataValue("actor_user_id", "alice")
	sess.UpdatedAt = time.Date(2026, 5, 8, 10, 30, 0, 0, time.UTC)
	sess.Append(models.Message{Role: "user", Content: "Please remember I prefer short updates."})
	sess.Append(models.Message{Role: "assistant", Content: "Understood."})

	prompt, err := mgr.buildSourceContextCurationPrompt("agent-test", "alice", "base curation prompt", "experience_curation", []*session.Session{sess})
	if err != nil {
		t.Fatalf("buildSourceContextCurationPrompt: %v", err)
	}

	for _, want := range []string{
		"base curation prompt",
		"## Target Source Context",
		"Prefer memory_search, memory_read, memory_write, and memory_edit for target user memory and source-agent bootstrap updates in this run.",
		"Use file tools only as a fallback when a memory tool cannot express the required change.",
		"configured workspace and authorized directories",
		"source_agent_id: agent-test",
		"target_user_id: alice",
		"task_kind: experience_curation",
		"## Target source sessions",
		"These sessions belong to the target source agent and target user being curated.",
		"session_key: agent:agent-test:user:alice:ws",
		"Please remember I prefer short updates.",
		"## Target recent daily memory",
		"recent daily note 2",
		"## Target user USER.md",
		"preferred name: Alice",
		"## Target user MEMORY.md",
		"durable preference: concise replies",
		"## Target agent AGENTS.md",
		"agent rule: verify targets before writing",
		"## Target agent SOUL.md",
		"mission: keep memory tidy",
		"## Target agent TOOLS.md",
		"tool rule: prefer memory tools",
		"## Target agent IDENTITY.md",
		"identity note",
		"## Target agent HEARTBEAT.md",
		"heartbeat note",
		"## Target agent BOOTSTRAP.md",
		"bootstrap note",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected prompt to contain %q\nfull prompt:\n%s", want, prompt)
		}
	}
}
