package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/internal/mcp"
	"github.com/chawuciren/evoduck/internal/mediautil"
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
	mu                      sync.RWMutex
	agents                  map[string]*Agent
	agentOrder              []string // 记录 agent 注册顺序
	llmReg                  *llm.Registry
	dataDir                 string // 全局数据目录
	sharedSkillsDir         string
	backendEndpoints        config.BackendCallConfig
	sessionToolConfig       config.SessionToolConfig
	memoryConfig            config.MemoryConfig
	mcpConfig               *config.MCPConfig // MCP 配置（保存用于初始化）
	mcpManager              *mcp.Manager      // MCP 管理器
	mcpInitialized          bool              // MCP 是否已初始化
	proxyDecider            *proxy.Decider    // 代理决策器
	scheduleManager         tools.ScheduleManager
	sessionGateway          tools.SessionGateway
	subagentGateway         tools.SubagentGateway
	pluginManager           *plugin.Manager
	configReloader          func(context.Context) (string, error)
	browserManager          *tools.BrowserManager
	toolResultCondenseLimit int
	imageAutoCompressLimit  int
	toolDefaultTimeout      time.Duration // 工具调用兜底超时
	fusionConfig            config.FusionConfig
	imageDescribeConfig     config.ImageDescribeConfig
}

type reloadProvider struct {
	manager *Manager
}

func (p reloadProvider) ReloadSystem(ctx context.Context, scope string) (string, error) {
	return p.manager.ReloadSystem(ctx, scope)
}

// SetFusionConfig 设置 Fusion 圆桌会诊配置（影响之后注册的 agent）
func (m *Manager) SetFusionConfig(cfg config.FusionConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fusionConfig = cfg
}

// SetImageDescribeConfig 设置读图工具配置（影响之后注册的 agent）
func (m *Manager) SetImageDescribeConfig(cfg config.ImageDescribeConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.imageDescribeConfig = cfg
}

// SetToolDefaultTimeout 设置工具调用兜底超时（影响之后注册的 agent）
func (m *Manager) SetToolDefaultTimeout(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolDefaultTimeout = d
}

func NewManager(llmReg *llm.Registry, dataDir string, sharedSkillsDir string, backendEndpoints config.BackendCallConfig, sessionToolConfig config.SessionToolConfig, memoryConfig config.MemoryConfig, mcpConfig *config.MCPConfig, proxyDecider *proxy.Decider, pluginManager *plugin.Manager) *Manager {
	mgr := &Manager{
		agents:                  make(map[string]*Agent),
		llmReg:                  llmReg,
		dataDir:                 dataDir,
		sharedSkillsDir:         sharedSkillsDir,
		backendEndpoints:        backendEndpoints,
		sessionToolConfig:       sessionToolConfig,
		memoryConfig:            memoryConfig,
		mcpConfig:               mcpConfig,
		proxyDecider:            proxyDecider,
		pluginManager:           pluginManager,
		browserManager:          tools.NewBrowserManager(),
		toolResultCondenseLimit: 32 * 1024,
		imageAutoCompressLimit:  32 * 1024,
		toolDefaultTimeout:      60 * time.Second,
	}

	return mgr
}

// StartMCP 异步启动 MCP server 连接（非阻塞）。
// 必须在所有 agent 注册完成后、Gateway 正式对外服务前调用。
// MCP server 的连接/初始化/工具加载全部在后台 goroutine 中进行，单个 server 失败不影响其它。
func (m *Manager) StartMCP() {
	m.mu.Lock()
	cfg := m.mcpConfig
	if cfg == nil || len(cfg.Servers) == 0 || m.mcpManager != nil {
		m.mu.Unlock()
		return
	}
	mcpMgr := mcp.NewManager(cfg, m.proxyDecider)
	mcpMgr.SetToolRegistrar(m) // Manager 自身实现 mcp.ToolRegistrar
	m.mcpManager = mcpMgr
	m.mcpInitialized = true
	m.mu.Unlock()

	logger.Info("Starting MCP servers (async)...")
	mcpMgr.Start(context.Background())
}

// RegisterMCPTools 实现 mcp.ToolRegistrar。
// 把一组 MCP 工具注册到当前所有 agent 的工具注册表中（线程安全）。
func (m *Manager) RegisterMCPTools(serverName string, wrappers []*mcp.MCPToolWrapper) {
	if len(wrappers) == 0 {
		return
	}
	m.mu.RLock()
	agents := make([]*Agent, 0, len(m.agents))
	for _, ag := range m.agents {
		if ag != nil && ag.Tools != nil {
			agents = append(agents, ag)
		}
	}
	m.mu.RUnlock()

	for _, ag := range agents {
		for _, w := range wrappers {
			ag.Tools.Register(w)
			logger.Debug("MCP tool registered", logger.Fields{
				"agent_id": ag.ID,
				"tool":     w.Name(),
				"server":   serverName,
			})
		}
	}
	logger.Info("MCP tools registered to agents", logger.Fields{
		"server":     serverName,
		"tool_count": len(wrappers),
		"agents":     len(agents),
	})
}

// UnregisterMCPTools 实现 mcp.ToolRegistrar。
// 从所有 agent 的工具注册表中移除某 server 的全部工具。
// 由于工具名以 server 名为前缀（见 MCPToolWrapper.Name），通过前缀匹配安全下线。
func (m *Manager) UnregisterMCPTools(serverName string) {
	prefix := serverName + "_"
	m.mu.RLock()
	agents := make([]*Agent, 0, len(m.agents))
	for _, ag := range m.agents {
		if ag != nil && ag.Tools != nil {
			agents = append(agents, ag)
		}
	}
	m.mu.RUnlock()

	for _, ag := range agents {
		for _, def := range ag.Tools.List() {
			name := def.Function.Name
			if strings.HasPrefix(name, prefix) {
				ag.Tools.Unregister(name)
			}
		}
	}
	logger.Info("MCP tools unregistered from agents", logger.Fields{
		"server": serverName,
		"agents": len(agents),
	})
}

// MCPStatus 返回 MCP 各 server 的连接状态快照（详见 mcp.StatusSnapshot）。
func (m *Manager) MCPStatus() mcp.StatusSnapshot {
	m.mu.RLock()
	mgr := m.mcpManager
	m.mu.RUnlock()
	if mgr == nil {
		return mcp.StatusSnapshot{}
	}
	return mgr.Status()
}

// ReconnectMCP 重连 MCP server，每个 server 完成后回调 onResult。
// target 语义：
//   - ""：只重连失败/未连接的（已 connected 的跳过）
//   - "all"：强制重连全部（含已 connected）
//   - 其它：强制重连指定名称
//
// 返回被触发重连的 server 名称列表。
func (m *Manager) ReconnectMCP(ctx context.Context, target string, onResult func(mcp.ServerStatus)) []string {
	m.mu.RLock()
	mgr := m.mcpManager
	m.mu.RUnlock()
	if mgr == nil {
		return nil
	}
	return mgr.ReconnectWithFeedback(ctx, target, onResult)
}

// ShutdownMCP 关闭所有 MCP 连接（用于进程退出）。
func (m *Manager) ShutdownMCP() {
	m.mu.RLock()
	mgr := m.mcpManager
	m.mu.RUnlock()
	if mgr == nil {
		return
	}
	if err := mgr.Close(); err != nil {
		logger.Warn("Failed to close MCP manager", logger.Fields{"error": err.Error()})
	}
}

func (m *Manager) SetToolResultCondenseLimit(limit int) {
	if limit > 0 {
		m.toolResultCondenseLimit = limit
	}
}

func (m *Manager) SetImageAutoCompressLimit(limit int) {
	if limit > 0 {
		m.imageAutoCompressLimit = limit
	}
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

type SourceContextCurationOptions struct {
	TaskKind   string
	SourceUser string
	Sessions   []*session.Session
	Metadata   map[string]string
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
	return runEphemeralRuntime(ctx, ag, sess, input, startedAt, "experience curator", opts.Metadata)
}

func (m *Manager) RunSourceContextCurationEphemeral(ctx context.Context, sourceAgentID, targetUserID, basePrompt string, opts SourceContextCurationOptions) (string, error) {
	startedAt := time.Now()
	taskKind := strings.TrimSpace(opts.TaskKind)
	logger.Info("Starting source-context curation run", logger.Fields{
		"source_agent_id": sourceAgentID,
		"target_user_id":  targetUserID,
		"task_kind":       taskKind,
		"session_count":   len(opts.Sessions),
	})
	ag, err := m.Get(sourceAgentID)
	if err != nil {
		return "", err
	}
	input, err := m.buildSourceContextCurationPrompt(sourceAgentID, targetUserID, strings.TrimSpace(basePrompt), taskKind, opts.Sessions)
	if err != nil {
		return "", err
	}
	sess := session.NewSession("", fmt.Sprintf("ephemeral-%s-curation", sourceAgentID), nil)
	sess.SetUserID(targetUserID)
	sess.SetMetadataValue("ephemeral", "true")
	sess.SetMetadataValue("session_kind", "source_curation")
	sess.SetMetadataValue("memory_policy", "ignore")
	sess.SetMetadataValue("agent_id", sourceAgentID)
	sess.SetMetadataValue("user_id", targetUserID)
	sess.SetMetadataValue("actor_user_id", targetUserID)
	if taskKind != "" {
		sess.SetMetadataValue("task_kind", taskKind)
	}
	if sourceUser := strings.TrimSpace(opts.SourceUser); sourceUser != "" {
		sess.SetMetadataValue("source_user_id", sourceUser)
	}
	if len(opts.Sessions) > 0 {
		sess.SetMetadataValue("source_session_key", strings.TrimSpace(opts.Sessions[0].Key))
	}
	for key, value := range opts.Metadata {
		sess.SetMetadataValue(key, value)
	}
	return runEphemeralRuntime(ctx, ag, sess, input, startedAt, "source-context curation", opts.Metadata)
}

func runEphemeralRuntime(ctx context.Context, ag *Agent, sess *session.Session, input string, startedAt time.Time, label string, metadata map[string]string) (string, error) {
	// 用流式驱动 curator 临时运行：流式靠 SSE 首字节保活，可绕过上游 router 对非流式请求的
	// TTFB/读取超时（曾导致压缩摘要 LLM 调用 30s 处 context deadline exceeded）。
	// MaxIterations 与原 Runtime.Run 硬编码上限一致；SendToolEvents 无前端消费者故关闭。
	// 运行结束后最终 assistant 文本已写入 sess，下方仍按原方式从 sess 提取。
	if err := ag.Runtime.RunStreaming(ctx, sess, input, models.StreamConfig{MaxIterations: 10, SendToolEvents: false}); err != nil {
		logger.Error(label+" run failed", logger.Fields{
			"error":       err.Error(),
			"duration_ms": time.Since(startedAt).Milliseconds(),
			"metadata":    metadata,
		})
		return "", err
	}
	msgs := sess.GetMessages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" && strings.TrimSpace(msgs[i].Content) != "" {
			report := strings.TrimSpace(msgs[i].Content)
			logger.Info(label+" run completed", logger.Fields{
				"duration_ms":   time.Since(startedAt).Milliseconds(),
				"metadata":      metadata,
				"message_count": len(msgs),
				"report_chars":  len(report),
			})
			return report, nil
		}
	}
	logger.Info(label+" run completed with empty report", logger.Fields{
		"duration_ms":   time.Since(startedAt).Milliseconds(),
		"metadata":      metadata,
		"message_count": len(msgs),
	})
	return "", nil
}

func (m *Manager) buildSourceContextCurationPrompt(sourceAgentID, targetUserID, basePrompt, taskKind string, sourceSessions []*session.Session) (string, error) {
	var b strings.Builder
	b.WriteString(basePrompt)
	b.WriteString("\n\n")
	b.WriteString("## Target Source Context\n\n")
	b.WriteString("You are executing a maintenance curation task inside the target source agent and target user context.\n")
	b.WriteString("The memory, bootstrap, and workspace routing in this run already belong to the target source agent and target user being curated.\n")
	b.WriteString("Every source-context section below belongs to the target source agent and target user being curated, not to some separate runtime agent.\n")
	b.WriteString("Prefer memory_search, memory_read, memory_write, and memory_edit for target user memory and source-agent bootstrap updates in this run.\n")
	b.WriteString("Use file tools only as a fallback when a memory tool cannot express the required change.\n")
	b.WriteString("File tools remain limited by the run's configured workspace and authorized directories; they do not dynamically rebind roots for you at call time.\n\n")
	b.WriteString("Target binding:\n")
	b.WriteString(fmt.Sprintf("- source_agent_id: %s\n", sourceAgentID))
	b.WriteString(fmt.Sprintf("- target_user_id: %s\n", targetUserID))
	if taskKind != "" {
		b.WriteString(fmt.Sprintf("- task_kind: %s\n", taskKind))
	}
	b.WriteString(fmt.Sprintf("- source_session_count: %d\n", len(sourceSessions)))

	userWorkspace := NewUserWorkspace(UserWorkspaceConfig{DataDir: m.dataDir, AgentID: sourceAgentID, UserID: targetUserID, Enabled: true, AutoCreate: false})
	recentDailyFiles := recentDailyMemoryFiles(userWorkspace.GetUserMemoryDir(), dailyMemoryWindowForTask(taskKind))
	appendSourceContextSection(&b, "Target source sessions", "These sessions belong to the target source agent and target user being curated.", renderSourceSessions(sourceSessions, taskKind))
	appendSourceContextSection(&b, "Target recent daily memory", "These daily notes belong to the target source agent and target user being curated.", renderFileBundle(recentDailyFiles, 3500))
	appendSourceContextSection(&b, "Target user USER.md", "This file belongs to the target source agent and target user being curated.", readOptionalFile(userWorkspace.GetUserMDPath(), 2500))
	appendSourceContextSection(&b, "Target user MEMORY.md", "This file belongs to the target source agent and target user being curated.", readOptionalFile(userWorkspace.GetUserMemoryPath(), 3500))
	appendSourceContextSection(&b, "Target agent AGENTS.md", "This file belongs to the target source agent being curated.", readOptionalFile(filepath.Join(m.dataDir, "agents", sourceAgentID, "AGENTS.md"), 2500))
	appendSourceContextSection(&b, "Target agent SOUL.md", "This file belongs to the target source agent being curated.", readOptionalFile(filepath.Join(m.dataDir, "agents", sourceAgentID, "SOUL.md"), 2500))
	appendSourceContextSection(&b, "Target agent TOOLS.md", "This file belongs to the target source agent being curated.", readOptionalFile(filepath.Join(m.dataDir, "agents", sourceAgentID, "TOOLS.md"), 2500))
	appendSourceContextSection(&b, "Target agent IDENTITY.md", "This file belongs to the target source agent being curated.", readOptionalFile(filepath.Join(m.dataDir, "agents", sourceAgentID, "IDENTITY.md"), 2000))
	appendSourceContextSection(&b, "Target agent HEARTBEAT.md", "This file belongs to the target source agent being curated.", readOptionalFile(filepath.Join(m.dataDir, "agents", sourceAgentID, "HEARTBEAT.md"), 2000))
	appendSourceContextSection(&b, "Target agent BOOTSTRAP.md", "This file belongs to the target source agent being curated.", readOptionalFile(filepath.Join(m.dataDir, "agents", sourceAgentID, "BOOTSTRAP.md"), 2000))
	return b.String(), nil
}

func appendSourceContextSection(b *strings.Builder, title, prelude, content string) {
	b.WriteString("\n\n")
	b.WriteString("## ")
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString(prelude)
	b.WriteString("\n\n")
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		b.WriteString("(empty)\n")
		return
	}
	b.WriteString(trimmed)
	b.WriteString("\n")
}

func renderSourceSessions(sourceSessions []*session.Session, taskKind string) string {
	if len(sourceSessions) == 0 {
		return ""
	}
	var b strings.Builder
	messageLimit := 24
	messageChars := 1000
	if strings.EqualFold(strings.TrimSpace(taskKind), "memory_curation") {
		messageLimit = 12
		messageChars = 700
	}
	for i, sess := range sourceSessions {
		if sess == nil {
			continue
		}
		b.WriteString(fmt.Sprintf("### Session %d\n", i+1))
		b.WriteString(fmt.Sprintf("- session_key: %s\n", sess.Key))
		b.WriteString(fmt.Sprintf("- session_user_id: %s\n", sess.GetUserID()))
		if actor := strings.TrimSpace(sess.GetMetadataValue("actor_user_id")); actor != "" {
			b.WriteString(fmt.Sprintf("- actor_user_id: %s\n", actor))
		}
		b.WriteString(fmt.Sprintf("- updated_at: %s\n\n", sess.UpdatedAt.Format(time.RFC3339)))
		messages := sess.GetMessages()
		if len(messages) > messageLimit {
			messages = messages[len(messages)-messageLimit:]
		}
		for idx, msg := range messages {
			if msg.Role == "tool" {
				continue
			}
			content := strings.TrimSpace(msg.Content)
			if content == "" {
				continue
			}
			if len(content) > messageChars {
				content = content[:messageChars] + "..."
			}
			b.WriteString(fmt.Sprintf("[%d] %s: %s\n", idx, strings.ToUpper(msg.Role), content))
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func dailyMemoryWindowForTask(taskKind string) int {
	if strings.EqualFold(strings.TrimSpace(taskKind), "experience_curation") {
		return 4
	}
	return 2
}

func recentDailyMemoryFiles(dir string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(files)
	if len(files) > limit {
		files = files[len(files)-limit:]
	}
	return files
}

func renderFileBundle(paths []string, maxCharsPerFile int) string {
	if len(paths) == 0 {
		return ""
	}
	parts := make([]string, 0, len(paths))
	for _, path := range paths {
		content := readOptionalFile(path, maxCharsPerFile)
		if strings.TrimSpace(content) == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("### %s\n\n%s", filepath.Base(path), content))
	}
	return strings.Join(parts, "\n\n")
}

func readOptionalFile(path string, maxChars int) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return ""
	}
	if maxChars > 0 && len(content) > maxChars {
		content = content[:maxChars] + "\n...\n[TRUNCATED]"
	}
	return content
}

func (m *Manager) runExperienceCuratorPreCompact(ctx context.Context, sourceSess *session.Session, msgs []models.Message) (string, error) {
	if sourceSess == nil || len(msgs) == 0 {
		return "", nil
	}
	curatorCtx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()
	sourceAgentID := strings.TrimSpace(sourceSess.GetMetadataValue("agent_id"))
	if sourceAgentID == "" {
		return "", fmt.Errorf("pre-compaction source agent unavailable")
	}
	targetUserID := resolveCurationTargetUserID(sourceSess)
	metadata := map[string]string{
		"session_kind":       "pre_compaction_flush",
		"memory_policy":      "ignore",
		"source_agent_id":    sourceAgentID,
		"source_session_key": sourceSess.Key,
		"source_session_id":  sourceSess.ID,
	}
	if actorUserID := strings.TrimSpace(sourceSess.GetMetadataValue("actor_user_id")); actorUserID != "" {
		metadata["actor_user_id"] = actorUserID
	}
	return m.RunSourceContextCurationEphemeral(curatorCtx, sourceAgentID, targetUserID, buildPreCompactCurationPrompt(sourceSess, msgs, targetUserID), SourceContextCurationOptions{
		TaskKind:   "pre_compaction_flush",
		SourceUser: strings.TrimSpace(sourceSess.GetUserID()),
		Sessions:   []*session.Session{sourceSess},
		Metadata:   metadata,
	})
}

func resolveCurationTargetUserID(sourceSess *session.Session) string {
	if sourceSess == nil {
		return ""
	}
	if actorUserID := strings.TrimSpace(sourceSess.GetMetadataValue("actor_user_id")); actorUserID != "" {
		return actorUserID
	}
	return strings.TrimSpace(sourceSess.GetUserID())
}

func buildPreCompactCurationPrompt(sourceSess *session.Session, msgs []models.Message, targetUserID string) string {
	var b strings.Builder
	b.WriteString("Run pre_compaction memory curation for the source conversation below.\n\n")
	b.WriteString("Boundaries:\n")
	b.WriteString("- You are running source-context curation for the target source agent and target user below, not as a separate memory owner.\n")
	b.WriteString("- Save only durable user/project facts, preferences, decisions, constraints, reusable troubleshooting, or stable knowledge from the source conversation.\n")
	b.WriteString("- Prefer memory_search, memory_read, memory_write, and memory_edit before any file tool.\n")
	b.WriteString("- When the source conversation has meaningful user-facing content, first inspect the current daily memory note memory/YYYY-MM-DD.md and add the necessary note there unless it is already captured.\n")
	b.WriteString("- Promote information into MEMORY.md only when it is stable enough to outlive the day.\n")
	b.WriteString("- Use USER.md only for confirmed user profile or preference information.\n")
	b.WriteString("- Update source-agent bootstrap memory such as AGENTS.md only for durable operating rules that should shape future behavior.\n")
	b.WriteString("- Use file tools only as a fallback when a memory tool cannot express the required change.\n")
	b.WriteString("- File tools remain limited by the run's configured workspace and authorized directories; they do not dynamically rebind roots for you at call time.\n")
	b.WriteString("- Do not stop at an internal summary if something worth preserving should be written into daily memory or long-term memory.\n")
	b.WriteString("- Do not save this curation task, compaction event, tool traces, or internal report text as memory.\n")
	b.WriteString("- Prefer updating existing memory/knowledge over creating duplicates.\n")
	b.WriteString("- Finish with a concise internal report of saved, skipped, and failed items.\n\n")
	b.WriteString("Source metadata:\n")
	b.WriteString(fmt.Sprintf("- session_key: %s\n", sourceSess.Key))
	b.WriteString(fmt.Sprintf("- source_agent_id: %s\n", sourceSess.GetMetadataValue("agent_id")))
	b.WriteString(fmt.Sprintf("- target_user_id: %s\n", targetUserID))
	b.WriteString(fmt.Sprintf("- session_user_id: %s\n", sourceSess.GetUserID()))
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
	summaryCtx, cancel := context.WithTimeout(ctx, 600*time.Second)
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
	toolReg.SetDefaultTimeout(m.toolDefaultTimeout)
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
		registerTool("media_store", true, func() {
			tool, err := tools.NewMediaStoreTool(m.dataDir)
			if err != nil {
				logger.Warn("Failed to create media_store tool", logger.Fields{"agent_id": id, "error": err.Error()})
				return
			}
			toolReg.Register(tool)
		})
		registerTool("image_compress", true, func() { toolReg.Register(tools.NewImageCompressTool()) })
		registerTool("file_write", true, func() { toolReg.Register(tools.NewFileWriteTool(permissions)) })
		registerTool("file_edit", true, func() { toolReg.Register(tools.NewFileEditTool(permissions)) })
		registerTool("file_patch", true, func() { toolReg.Register(tools.NewFilePatchTool(permissions)) })
		registerTool("exec", true, func() { toolReg.Register(tools.NewExecTool(permissions, m.proxyDecider)) })
		registerTool("process", true, func() { toolReg.Register(tools.NewProcessTool(permissions, m.proxyDecider)) })
		registerTool("sleep", true, func() { toolReg.Register(tools.NewSleepTool()) })
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
		// Fusion 圆桌会诊工具（仅 admin）
		if m.fusionConfig.Enabled && len(m.fusionConfig.Panel) > 0 {
			toolReg.Register(tools.NewFusionTool(m.llmReg, m.fusionConfig, func() tools.SessionGateway {
				return m.sessionGateway
			}))
			logger.Info("Fusion tool registered", logger.Fields{"agent_id": id})
		}
	}

	// 读图工具（所有角色可用；需配置 enabled + provider + model）
	if m.imageDescribeConfig.Enabled && m.imageDescribeConfig.Provider != "" && m.imageDescribeConfig.Model != "" {
		toolReg.Register(tools.NewImageDescribeTool(m.llmReg, m.imageDescribeConfig))
		logger.Info("ImageDescribe tool registered", logger.Fields{
			"agent_id": id,
			"provider": m.imageDescribeConfig.Provider,
			"model":    m.imageDescribeConfig.Model,
		})
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

	if m.mcpManager != nil {
		// 使用 MCP manager 当前已连接的工具 wrapper（携带每 server 的 call_timeout），
		// 注册到该 agent。之后 server 在线状态变化时，mcp.Manager 会通过 ToolRegistrar
		// 回调（RegisterMCPTools / UnregisterMCPTools）动态增删，无需在此处重复处理。
		for _, wrapper := range m.mcpManager.GetAllTools() {
			toolReg.Register(wrapper)
			logger.Info("MCP tool registered", logger.Fields{
				"agent_id": id,
				"tool":     wrapper.Name(),
			})
		}
	}

	promptBuilder := NewPromptBuilder(cfg.Workspace, id, m.dataDir, toolReg, skillLoader)
	promptBuilder.SetUserIsolation(cfg.UserIsolation)
	promptBuilder.SetBootstrapConfig(m.memoryConfig.Bootstrap)
	promptBuilder.SetMemoryConfig(m.memoryConfig.MediumTerm)
	promptBuilder.SetLLMProvider(provider)

	// Fusion 工具是否对本 agent 可用（决定是否在 system prompt 中注入 Fusion 使用约束）。
	// 仅 admin 且 fusion 已启用且配置了 panel 时注册该工具。
	fusionAvailable := role == models.RoleAdmin && m.fusionConfig.Enabled && len(m.fusionConfig.Panel) > 0
	promptBuilder.SetFusionEnabled(fusionAvailable)

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
	runtime.SetToolResultCondenseLimit(m.toolResultCondenseLimit)
	runtime.SetImageAutoCompressLimit(m.imageAutoCompressLimit)
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
