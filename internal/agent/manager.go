package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/internal/mediautil"
	"github.com/chawuciren/evoduck/internal/mcp"
	"github.com/chawuciren/evoduck/internal/memory"
	"github.com/chawuciren/evoduck/internal/plugin"
	"github.com/chawuciren/evoduck/internal/profile"
	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/internal/skill"
	"github.com/chawuciren/evoduck/internal/tools"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

const ExperienceCuratorID = "experience-curator"

type Manager struct {
	mu                sync.RWMutex
	agents            map[string]*Agent
	agentOrder        []string // 记录 agent 注册顺序
	llmReg            *llm.Registry
	dataDir           string // 全局数据目录
	sharedSkillsDir   string
	backendEndpoints  config.BackendCallConfig
	sessionToolConfig config.SessionToolConfig
	memoryConfig      config.MemoryConfig
	mcpConfig         *config.MCPConfig // MCP 配置（保存用于初始化）
	mcpManager        *mcp.Manager      // MCP 管理器
	mcpInitialized    bool              // MCP 是否已初始化
	proxyDecider      *proxy.Decider    // 代理决策器
	scheduleManager   tools.ScheduleManager
	sessionGateway    tools.SessionGateway
	subagentGateway   tools.SubagentGateway
	pluginManager     *plugin.Manager
	configReloader    func(context.Context) (string, error)
	browserManager    *tools.BrowserManager
}

type reloadProvider struct {
	manager *Manager
}

func (p reloadProvider) ReloadSystem(ctx context.Context, scope string) (string, error) {
	return p.manager.ReloadSystem(ctx, scope)
}

func NewManager(llmReg *llm.Registry, dataDir string, sharedSkillsDir string, backendEndpoints config.BackendCallConfig, sessionToolConfig config.SessionToolConfig, memoryConfig config.MemoryConfig, mcpConfig *config.MCPConfig, proxyDecider *proxy.Decider, pluginManager *plugin.Manager) *Manager {
	mgr := &Manager{
		agents:            make(map[string]*Agent),
		llmReg:            llmReg,
		dataDir:           dataDir,
		sharedSkillsDir:   sharedSkillsDir,
		backendEndpoints:  backendEndpoints,
		sessionToolConfig: sessionToolConfig,
		memoryConfig:      memoryConfig,
		mcpConfig:         mcpConfig,
		proxyDecider:      proxyDecider,
		pluginManager:     pluginManager,
		browserManager:    tools.NewBrowserManager(),
	}

	return mgr
}

type Agent struct {
	ID            string
	Config        config.AgentConfig
	Runtime       *Runtime
	Loop          *AgentLoop
	Tools         *tools.Registry
	Skills        *skill.Loader
	Role          models.Role
	MemoryManager *memory.Manager
	UserIsolation config.UserIsolationConfig // 用户隔离配置
	System        bool
	Hidden        bool
}

type EphemeralRunOptions struct {
	UserID   string
	Metadata map[string]string
}

func ExperienceCuratorConfig(dataDir string, base config.AgentConfig) config.AgentConfig {
	cfg := base
	curatorDataDir := filepath.Clean(dataDir)
	if absDataDir, err := filepath.Abs(curatorDataDir); err == nil {
		curatorDataDir = absDataDir
	}
	cfg.Role = string(models.RoleAdmin)
	cfg.Workspace = filepath.Join(curatorDataDir, "agents", ExperienceCuratorID)
	cfg.UserIsolation.AutoCreate = false
	cfg.UserIsolation.AutoProfile = false
	cfg.Permissions.AuthorizedDirectories = []string{curatorDataDir}
	cfg.Permissions.AuthorizedTools = []string{
		"time",
		"file_read",
		"file_list",
		"file_write",
		"file_edit",
		"file_patch",
		"sessions_list",
		"sessions_history",
		"memory_search",
		"memory_read",
		"memory_write",
		"memory_edit",
		"knowledge_tree",
		"knowledge_search",
		"knowledge_read",
		"skill_list",
		"skill_detail",
		"skill_use",
		"skill",
		"system_reload",
	}
	return cfg
}

func (m *Manager) EnsureExperienceCurator(base config.AgentConfig) error {
	cfg := ExperienceCuratorConfig(m.dataDir, base)
	m.mu.RLock()
	existing, ok := m.agents[ExperienceCuratorID]
	m.mu.RUnlock()
	if ok {
		if strings.TrimSpace(existing.Config.Workspace) != "" {
			cfg.Workspace = existing.Config.Workspace
		}
		return ensureAgentScaffold(ExperienceCuratorID, cfg.Workspace)
	}
	return m.Register(ExperienceCuratorID, cfg)
}

func (m *Manager) RunExperienceCuratorEphemeral(ctx context.Context, input string, opts EphemeralRunOptions) (string, error) {
	startedAt := time.Now()
	logger.Info("Starting experience curator ephemeral run", logger.Fields{
		"user_id":     opts.UserID,
		"metadata":    opts.Metadata,
		"input_chars": len(input),
	})
	ag, err := m.Get(ExperienceCuratorID)
	if err != nil {
		logger.Error("Experience curator unavailable", logger.Fields{"error": err.Error()})
		return "", err
	}
	sess := session.NewSession("", "ephemeral-"+ExperienceCuratorID, nil)
	sess.SetUserID(opts.UserID)
	sess.SetMetadataValue("ephemeral", "true")
	sess.SetMetadataValue("session_kind", "system_task")
	sess.SetMetadataValue("memory_policy", "ignore")
	sess.SetMetadataValue("agent_id", ExperienceCuratorID)
	for key, value := range opts.Metadata {
		sess.SetMetadataValue(key, value)
	}
	if err := ag.Runtime.Run(ctx, sess, input); err != nil {
		logger.Error("Experience curator ephemeral run failed", logger.Fields{
			"error":       err.Error(),
			"duration_ms": time.Since(startedAt).Milliseconds(),
			"metadata":    opts.Metadata,
		})
		return "", err
	}
	msgs := sess.GetMessages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" && strings.TrimSpace(msgs[i].Content) != "" {
			report := strings.TrimSpace(msgs[i].Content)
			logger.Info("Experience curator ephemeral run completed", logger.Fields{
				"duration_ms":   time.Since(startedAt).Milliseconds(),
				"metadata":      opts.Metadata,
				"message_count": len(msgs),
				"report_chars":  len(report),
			})
			return report, nil
		}
	}
	logger.Info("Experience curator ephemeral run completed with empty report", logger.Fields{
		"duration_ms":   time.Since(startedAt).Milliseconds(),
		"metadata":      opts.Metadata,
		"message_count": len(msgs),
	})
	return "", nil
}

func (m *Manager) runExperienceCuratorPreCompact(ctx context.Context, sourceSess *session.Session, msgs []models.Message) (string, error) {
	if sourceSess == nil || len(msgs) == 0 {
		return "", nil
	}
	curatorCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	metadata := map[string]string{
		"session_kind":       "pre_compaction_flush",
		"memory_policy":      "ignore",
		"source_session_key": sourceSess.Key,
		"source_session_id":  sourceSess.ID,
	}
	if sourceAgentID := strings.TrimSpace(sourceSess.GetMetadataValue("agent_id")); sourceAgentID != "" {
		metadata["source_agent_id"] = sourceAgentID
	}
	if actorUserID := strings.TrimSpace(sourceSess.GetMetadataValue("actor_user_id")); actorUserID != "" {
		metadata["actor_user_id"] = actorUserID
	}
	return m.RunExperienceCuratorEphemeral(curatorCtx, buildPreCompactCurationPrompt(sourceSess, msgs), EphemeralRunOptions{
		UserID:   sourceSess.GetUserID(),
		Metadata: metadata,
	})
}

func buildPreCompactCurationPrompt(sourceSess *session.Session, msgs []models.Message) string {
	var b strings.Builder
	b.WriteString("Run pre_compaction memory curation for the source conversation below.\n\n")
	b.WriteString("Boundaries:\n")
	b.WriteString("- Use the existing experience-curator tools and normal tool loop.\n")
	b.WriteString("- Save only durable user/project facts, preferences, decisions, constraints, reusable troubleshooting, or stable knowledge from the source conversation.\n")
	b.WriteString("- This is cross-agent curation: write source agent user memory with file tools under users/<source_agent_id>_user_<target_user_id>/, not under the curator workspace and not with memory_write for the curator's own agent namespace.\n")
	b.WriteString("- Always inspect or create the target source-agent daily memory file memory/YYYY-MM-DD.md for the current date when the source conversation contains meaningful user-facing content.\n")
	b.WriteString("- Do not save this curation task, compaction event, tool traces, or internal report text as memory.\n")
	b.WriteString("- Prefer updating existing memory/knowledge over creating duplicates.\n")
	b.WriteString("- Finish with a concise internal report of saved, skipped, and failed items.\n\n")
	b.WriteString("Source metadata:\n")
	b.WriteString(fmt.Sprintf("- session_key: %s\n", sourceSess.Key))
	b.WriteString(fmt.Sprintf("- source_agent_id: %s\n", sourceSess.GetMetadataValue("agent_id")))
	b.WriteString(fmt.Sprintf("- target_user_id: %s\n", sourceSess.GetUserID()))
	b.WriteString(fmt.Sprintf("- actor_user_id: %s\n\n", sourceSess.GetMetadataValue("actor_user_id")))
	b.WriteString("Source conversation segment to preserve before compaction:\n")
	for i, msg := range msgs {
		if msg.Role == "tool" {
			continue
		}
		content := msg.Content
		if len(content) > 800 {
			content = content[:800] + "..."
		}
		b.WriteString(fmt.Sprintf("[%d] %s: %s\n", i, strings.ToUpper(msg.Role), content))
	}
	return b.String()
}

func (m *Manager) runExperienceCuratorCompactionSummary(ctx context.Context, sourceSess *session.Session, msgs []models.Message, flushReport string) (string, error) {
	if sourceSess == nil || len(msgs) == 0 {
		return "", nil
	}
	summaryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	metadata := map[string]string{
		"session_kind":       "compaction_summary",
		"memory_policy":      "ignore",
		"source_session_key": sourceSess.Key,
		"source_session_id":  sourceSess.ID,
	}
	if sourceAgentID := strings.TrimSpace(sourceSess.GetMetadataValue("agent_id")); sourceAgentID != "" {
		metadata["source_agent_id"] = sourceAgentID
	}
	if actorUserID := strings.TrimSpace(sourceSess.GetMetadataValue("actor_user_id")); actorUserID != "" {
		metadata["actor_user_id"] = actorUserID
	}
	return m.RunExperienceCuratorEphemeral(summaryCtx, buildCompactionSummaryPrompt(sourceSess, msgs, flushReport), EphemeralRunOptions{
		UserID:   sourceSess.GetUserID(),
		Metadata: metadata,
	})
}

func buildCompactionSummaryPrompt(sourceSess *session.Session, msgs []models.Message, flushReport string) string {
	var b strings.Builder
	b.WriteString("Run compaction_summary for the source conversation below.\n\n")
	b.WriteString("Goal:\n")
	b.WriteString("- Return a dense but concise summary that can replace the older conversation segment during context compaction.\n")
	b.WriteString("- Preserve user requirements, decisions, constraints, technical details, unresolved tasks, and important context.\n")
	b.WriteString("- Do not write memory, knowledge, skills, or files for this summary task. Only return the summary text.\n")
	b.WriteString("- If there is no meaningful content to preserve, return an empty response.\n\n")
	b.WriteString("Source metadata:\n")
	b.WriteString(fmt.Sprintf("- session_key: %s\n", sourceSess.Key))
	b.WriteString(fmt.Sprintf("- source_agent_id: %s\n", sourceSess.GetMetadataValue("agent_id")))
	b.WriteString(fmt.Sprintf("- target_user_id: %s\n", sourceSess.GetUserID()))
	b.WriteString(fmt.Sprintf("- actor_user_id: %s\n\n", sourceSess.GetMetadataValue("actor_user_id")))
	if strings.TrimSpace(flushReport) != "" {
		b.WriteString("Pre-compaction memory flush report for awareness only:\n")
		b.WriteString(strings.TrimSpace(flushReport))
		b.WriteString("\n\n")
	}
	b.WriteString("Conversation segment to summarize:\n")
	for i, msg := range msgs {
		if msg.Role == "tool" {
			continue
		}
		content := msg.Content
		if len(content) > 1000 {
			content = content[:1000] + "..."
		}
		b.WriteString(fmt.Sprintf("[%d] %s: %s\n", i, strings.ToUpper(msg.Role), content))
	}
	return b.String()
}

func (m *Manager) RestoreSystemScaffolds() (int, error) {
	agents := m.ListAll()
	count := 0
	for _, agent := range agents {
		if agent == nil || !agent.System {
			continue
		}
		if err := ensureAgentScaffold(agent.ID, agent.Config.Workspace); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (m *Manager) SetScheduleManager(scheduleManager tools.ScheduleManager) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scheduleManager = scheduleManager
}

func (m *Manager) GetScheduleManager() tools.ScheduleManager {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.scheduleManager
}

func (m *Manager) SetSessionGateway(sessionGateway tools.SessionGateway) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionGateway = sessionGateway
}

func (m *Manager) GetSessionGateway() tools.SessionGateway {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessionGateway
}

func (m *Manager) SetSubagentGateway(subagentGateway tools.SubagentGateway) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subagentGateway = subagentGateway
}

func (m *Manager) GetSubagentGateway() tools.SubagentGateway {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.subagentGateway
}

func (m *Manager) SetConfigReloader(reloader func(context.Context) (string, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configReloader = reloader
}

func (m *Manager) reloadConfig(ctx context.Context) (string, error) {
	m.mu.RLock()
	reloader := m.configReloader
	m.mu.RUnlock()
	if reloader == nil {
		return "", fmt.Errorf("config reload is not available")
	}
	return reloader(ctx)
}

func (m *Manager) GetMCPManager() *mcp.Manager {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.mcpManager
}

func (m *Manager) Register(id string, cfg config.AgentConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := ensureAgentScaffold(id, cfg.Workspace); err != nil {
		return fmt.Errorf("ensure scaffold for agent %s: %w", id, err)
	}

	providerName, modelName, err := m.llmReg.ResolveProviderModel(cfg.Provider, cfg.Model)
	if err != nil {
		return fmt.Errorf("resolve LLM for agent %s: %w", id, err)
	}

	provider, err := m.llmReg.Get(providerName)
	if err != nil {
		return fmt.Errorf("load LLM provider for agent %s: %w", id, err)
	}

	// 设置 Agent 级别的 LLM 选项（Temperature 等）
	llmOpts := llm.ChatOptions{
		Model:       modelName,
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxTokens,
		TopP:        cfg.TopP,
	}
	if cfg.Temperature != nil {
		logger.Info("Setting agent LLM options", logger.Fields{
			"agent_id":    id,
			"provider":    providerName,
			"model":       modelName,
			"temperature": *cfg.Temperature,
		})
	}
	provider.SetDefaultOptions(llmOpts)

	cfg.Provider = providerName
	cfg.Model = modelName

	// 创建工具注册表
	toolReg := tools.NewRegistry()
	role := models.Role(cfg.Role)
	permissions := tools.NewAgentPermissions(role, cfg.Workspace, cfg.Permissions)
	hasToolOverride := len(cfg.Permissions.AuthorizedTools) > 0
	registerTool := func(name string, defaultAllowed bool, build func()) {
		allowed := defaultAllowed
		if hasToolOverride {
			allowed = permissions.CanUseTool(name, defaultAllowed)
		}
		if allowed {
			build()
		}
	}

	// 所有角色都可用的工具
	registerTool("time", true, func() { toolReg.Register(tools.NewTimeTool()) })
	registerTool("web_fetch", true, func() { toolReg.Register(tools.NewWebFetchTool(m.proxyDecider)) })
	registerTool("web_search", true, func() { toolReg.Register(tools.NewWebSearchTool(m.proxyDecider)) })

	// Browser automation tools
	registerTool("browser_navigate", true, func() { toolReg.Register(tools.NewBrowserNavigateTool(m.browserManager)) })
	registerTool("browser_click", true, func() { toolReg.Register(tools.NewBrowserClickTool(m.browserManager)) })
	registerTool("browser_type", true, func() { toolReg.Register(tools.NewBrowserTypeTool(m.browserManager)) })
	registerTool("browser_screenshot", true, func() { toolReg.Register(tools.NewBrowserScreenshotTool(m.browserManager)) })
	registerTool("browser_snapshot", true, func() { toolReg.Register(tools.NewBrowserSnapshotTool(m.browserManager)) })
	registerTool("browser_close", true, func() { toolReg.Register(tools.NewBrowserCloseTool(m.browserManager)) })
	registerTool("browser_evaluate", true, func() { toolReg.Register(tools.NewBrowserEvaluateTool(m.browserManager)) })
	registerTool("browser_wait_for", true, func() { toolReg.Register(tools.NewBrowserWaitForTool(m.browserManager)) })
	registerTool("browser_scroll", true, func() { toolReg.Register(tools.NewBrowserScrollTool(m.browserManager)) })
	registerTool("browser_get_html", true, func() { toolReg.Register(tools.NewBrowserGetHTMLTool(m.browserManager)) })
	registerTool("browser_hover", true, func() { toolReg.Register(tools.NewBrowserHoverTool(m.browserManager)) })
	registerTool("browser_press_key", true, func() { toolReg.Register(tools.NewBrowserPressKeyTool(m.browserManager)) })

	registerTool("knowledge_tree", true, func() { toolReg.Register(tools.NewKnowledgeTreeTool(m.dataDir)) })
	registerTool("knowledge_search", true, func() { toolReg.Register(tools.NewKnowledgeSearchTool(m.dataDir)) })
	registerTool("knowledge_read", true, func() { toolReg.Register(tools.NewKnowledgeReadTool(m.dataDir)) })

	// 文件操作工具（所有角色可用）
	registerTool("file_read", true, func() { toolReg.Register(tools.NewFileReadTool(permissions)) })
	registerTool("file_list", true, func() { toolReg.Register(tools.NewFileListTool(permissions)) })
	registerTool("memory_search", true, func() { toolReg.Register(tools.NewMemorySearchTool(cfg.Workspace, id, m.dataDir)) })
	registerTool("memory_read", true, func() { toolReg.Register(tools.NewMemoryReadTool(cfg.Workspace, id, m.dataDir)) })
	registerTool("memory_write", true, func() { toolReg.Register(tools.NewMemoryWriteTool(cfg.Workspace, id, m.dataDir)) })
	registerTool("memory_edit", true, func() { toolReg.Register(tools.NewMemoryEditTool(cfg.Workspace, id, m.dataDir)) })
	registerTool("schedule_list", true, func() {
		toolReg.Register(tools.NewScheduleListTool(id, func() tools.ScheduleManager {
			return m.scheduleManager
		}))
	})
	registerTool("subagent_list", true, func() {
		toolReg.Register(tools.NewSubagentListTool(id, func() tools.SubagentGateway {
			return m.subagentGateway
		}))
	})
	registerTool("subagent_status", true, func() {
		toolReg.Register(tools.NewSubagentStatusTool(id, func() tools.SubagentGateway {
			return m.subagentGateway
		}))
	})
	registerTool("subagent_result", true, func() {
		toolReg.Register(tools.NewSubagentResultTool(id, func() tools.SubagentGateway {
			return m.subagentGateway
		}))
	})
	sessionPolicy := tools.NewSessionToolPolicy(m.sessionToolConfig)
	if permissions.CanUseTool("sessions_list", sessionPolicy.IsAllowed(role, "sessions_list") || role == models.RoleAdmin) {
		toolReg.Register(tools.NewSessionListTool(id, func() tools.SessionGateway {
			return m.sessionGateway
		}, sessionPolicy))
	}
	if permissions.CanUseTool("sessions_history", sessionPolicy.IsAllowed(role, "sessions_history") || role == models.RoleAdmin) {
		toolReg.Register(tools.NewSessionHistoryTool(id, func() tools.SessionGateway {
			return m.sessionGateway
		}, sessionPolicy))
	}
	if permissions.CanUseTool("sessions_send", sessionPolicy.IsAllowed(role, "sessions_send") || role == models.RoleAdmin) {
		toolReg.Register(tools.NewSessionSendTool(id, func() tools.SessionGateway {
			return m.sessionGateway
		}, sessionPolicy))
	}
	if permissions.CanUseTool("sessions_run", sessionPolicy.IsAllowed(role, "sessions_run") || role == models.RoleAdmin) {
		toolReg.Register(tools.NewSessionRunTool(id, func() tools.SessionGateway {
			return m.sessionGateway
		}, sessionPolicy))
	}

	// backend_call 工具（所有角色可用，但 customer 受限）
	if len(m.backendEndpoints.Endpoints) > 0 {
		registerTool("backend_call", true, func() { toolReg.Register(tools.NewBackendCallTool(m.backendEndpoints, m.proxyDecider)) })
	}

	// 仅 employee 和 admin 可用的工具
	if role == models.RoleEmployee || role == models.RoleAdmin {
		registerTool("http_call", true, func() { toolReg.Register(tools.NewHTTPCallTool(m.proxyDecider)) })
		registerTool("file_write", true, func() { toolReg.Register(tools.NewFileWriteTool(permissions)) })
		registerTool("file_edit", true, func() { toolReg.Register(tools.NewFileEditTool(permissions)) })
		registerTool("file_patch", true, func() { toolReg.Register(tools.NewFilePatchTool(permissions)) })
		registerTool("exec", true, func() { toolReg.Register(tools.NewExecTool(permissions, m.proxyDecider)) })
		registerTool("process", true, func() { toolReg.Register(tools.NewProcessTool(permissions, m.proxyDecider)) })
		registerTool("code_execution", true, func() { toolReg.Register(tools.NewCodeExecutionTool()) })
		registerTool("knowledge_write", true, func() { toolReg.Register(tools.NewKnowledgeWriteTool(m.dataDir)) })
		registerTool("knowledge_edit", true, func() { toolReg.Register(tools.NewKnowledgeEditTool(m.dataDir)) })
		registerTool("knowledge_delete", true, func() { toolReg.Register(tools.NewKnowledgeDeleteTool(m.dataDir)) })
		registerTool("schedule_create", true, func() {
			toolReg.Register(tools.NewScheduleCreateTool(id, func() tools.ScheduleManager {
				return m.scheduleManager
			}))
		})
		registerTool("schedule_enable", true, func() {
			toolReg.Register(tools.NewScheduleEnableTool(id, func() tools.ScheduleManager {
				return m.scheduleManager
			}, true))
		})
		registerTool("schedule_disable", true, func() {
			toolReg.Register(tools.NewScheduleEnableTool(id, func() tools.ScheduleManager {
				return m.scheduleManager
			}, false))
		})
		registerTool("schedule_delete", true, func() {
			toolReg.Register(tools.NewScheduleDeleteTool(id, func() tools.ScheduleManager {
				return m.scheduleManager
			}))
		})
		registerTool("schedule_trigger", true, func() {
			toolReg.Register(tools.NewScheduleTriggerTool(id, func() tools.ScheduleManager {
				return m.scheduleManager
			}))
		})
		registerTool("subagent_start_internal", true, func() {
			toolReg.Register(tools.NewSubagentStartInternalTool(id, func() tools.SubagentGateway {
				return m.subagentGateway
			}))
		})
		registerTool("subagent_start_external", true, func() {
			toolReg.Register(tools.NewSubagentStartExternalTool(id, func() tools.SubagentGateway {
				return m.subagentGateway
			}))
		})
		registerTool("subagent_cancel", true, func() {
			toolReg.Register(tools.NewSubagentCancelTool(id, func() tools.SubagentGateway {
				return m.subagentGateway
			}))
		})
		registerTool("system_reload", true, func() { toolReg.Register(tools.NewSystemReloadTool(reloadProvider{manager: m})) })
	}

	// 加载 Skills
	skillLoader := skill.NewLoader(cfg.Workspace, m.sharedSkillsDir)
	if err := skillLoader.LoadAll(); err != nil {
		logger.Warn("Failed to load skills for agent", logger.Fields{
			"agent_id": id,
			"error":    err.Error(),
		})
	}

	registerTool("skill_list", true, func() { toolReg.Register(tools.NewSkillListTool(skillLoader, role)) })
	registerTool("skill_detail", true, func() { toolReg.Register(tools.NewSkillDetailTool(skillLoader, role)) })
	registerTool("skill_use", true, func() { toolReg.Register(tools.NewSkillUseTool(skillLoader, role)) })
	registerTool("skill", true, func() { toolReg.Register(tools.NewSkillTool(skillLoader, role)) })

	if m.pluginManager != nil {
		for _, toolAdapter := range m.pluginManager.ListToolAdapters() {
			if _, err := toolReg.Get(toolAdapter.Name()); err == nil {
				return fmt.Errorf("plugin tool name conflict: %s", toolAdapter.Name())
			}
			toolReg.Register(toolAdapter)
			logger.Info("Plugin tool registered", logger.Fields{
				"agent_id": id,
				"tool":     toolAdapter.Name(),
			})
		}
	}

	if m.mcpConfig != nil && len(m.mcpConfig.Servers) > 0 && !m.mcpInitialized {
		logger.Info("Initializing MCP servers...")
		mcpMgr := mcp.NewManager(m.mcpConfig, m.proxyDecider)
		if err := mcpMgr.Initialize(context.Background()); err != nil {
			logger.Warn("Failed to initialize MCP servers, continuing without MCP tools", logger.Fields{
				"error": err.Error(),
			})
		} else {
			logger.Info("MCP servers initialized successfully")
			m.mcpManager = mcpMgr
			m.mcpInitialized = true

			totalTools := 0
			for _, client := range mcpMgr.GetAllClients() {
				totalTools += len(client.GetAllTools())
			}
			logger.Info("MCP tools available", logger.Fields{"count": totalTools})
		}
	}

	if m.mcpManager != nil {
		for _, client := range m.mcpManager.GetAllClients() {
			for _, tool := range client.GetAllTools() {
				wrapper := mcp.NewMCPToolWrapper(client, tool)
				toolReg.Register(wrapper)
				logger.Info("MCP tool registered", logger.Fields{
					"agent_id": id,
					"tool":     wrapper.Name(),
				})
			}
		}
	}

	promptBuilder := NewPromptBuilder(cfg.Workspace, id, m.dataDir, toolReg, skillLoader)
	promptBuilder.SetUserIsolation(cfg.UserIsolation)
	promptBuilder.SetBootstrapConfig(m.memoryConfig.Bootstrap)
	promptBuilder.SetMemoryConfig(m.memoryConfig.MediumTerm)
	promptBuilder.SetLLMProvider(provider)

	memMgr, err := memory.NewManager(cfg.Workspace, m.memoryConfig.MediumTerm.Dir, m.memoryConfig.CoreMemory.File, m.dataDir, id)
	if err != nil {
		logger.Warn("Failed to create memory manager", logger.Fields{
			"agent_id": id,
			"error":    err.Error(),
		})
	}

	compactor := NewCompactor(provider, CompactorConfig{
		MaxMessages:        m.memoryConfig.ShortTerm.MaxMessages,
		MaxTokens:          m.memoryConfig.ShortTerm.MaxTokens,
		KeepRecent:         m.memoryConfig.ShortTerm.KeepRecent,
		Workspace:          cfg.Workspace,
		FlushBeforeCompact: m.memoryConfig.ShortTerm.FlushBeforeCompact,
	})

	if id != ExperienceCuratorID {
		compactor.SetPreCompactCurator(m.runExperienceCuratorPreCompact)
		compactor.SetSummaryGenerator(m.runExperienceCuratorCompactionSummary)
	}

	runtime := NewRuntime(id, cfg.Workspace, provider, toolReg, promptBuilder, role, compactor, true, m.pluginManager)
	if store, err := mediautil.NewStore(m.dataDir); err != nil {
		logger.Warn("Failed to create runtime media store", logger.Fields{"agent_id": id, "error": err.Error()})
	} else {
		runtime.SetMediaStore(store)
	}

	registerTool("task_plan", true, func() { toolReg.Register(tools.NewTaskPlanTool(runtime)) })

	loop := NewAgentLoop(runtime)

	m.agents[id] = &Agent{
		ID:            id,
		Config:        cfg,
		Runtime:       runtime,
		Loop:          loop,
		Tools:         toolReg,
		Skills:        skillLoader,
		Role:          role,
		MemoryManager: memMgr,
		UserIsolation: cfg.UserIsolation,
		System:        id == ExperienceCuratorID,
		Hidden:        id == ExperienceCuratorID,
	}

	m.agentOrder = append(m.agentOrder, id)

	return nil
}

func ensureAgentScaffold(id string, workspace string) error {
	if id == ExperienceCuratorID {
		return profile.EnsureExperienceCuratorScaffold(workspace)
	}
	return profile.EnsureAgentScaffold(workspace)
}

func (m *Manager) ReloadSystem(ctx context.Context, scope string) (string, error) {
	switch scope {
	case "skills":
		count, err := m.reloadSkills(ctx)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Reloaded skills for %d agent(s).", count), nil
	case "all":
		configResult, err := m.reloadConfig(ctx)
		if err != nil {
			return "", err
		}
		scaffolds, err := m.RestoreSystemScaffolds()
		if err != nil {
			return "", err
		}
		count, err := m.reloadSkills(ctx)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Reloaded system runtime state: %s Restored %d system scaffold(s), skills for %d agent(s).", configResult, scaffolds, count), nil
	case "config":
		return m.reloadConfig(ctx)
	default:
		return "", fmt.Errorf("unsupported reload scope: %s", scope)
	}
}

func (m *Manager) reloadSkills(ctx context.Context) (int, error) {
	agents := m.ListAll()

	count := 0
	for _, agent := range agents {
		if ctx != nil {
			select {
			case <-ctx.Done():
				return count, ctx.Err()
			default:
			}
		}
		if agent.Skills == nil {
			continue
		}
		if err := agent.Skills.LoadAll(); err != nil {
			return count, fmt.Errorf("reload skills for agent %s: %w", agent.ID, err)
		}
		count++
	}
	return count, nil
}

func (m *Manager) Get(id string) (*Agent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	agent, ok := m.agents[id]
	if !ok {
		return nil, fmt.Errorf("agent not found: %s", id)
	}
	return agent, nil
}

func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.agents)
}

func (m *Manager) List() []*Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var agents []*Agent
	for _, id := range m.agentOrder {
		if a, ok := m.agents[id]; ok {
			if a.Hidden {
				continue
			}
			agents = append(agents, a)
		}
	}
	return agents
}

func (m *Manager) ListAll() []*Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var agents []*Agent
	for _, id := range m.agentOrder {
		if a, ok := m.agents[id]; ok {
			agents = append(agents, a)
		}
	}
	return agents
}
