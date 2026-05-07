package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/internal/skill"
	"github.com/chawuciren/evoduck/internal/tools"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
)

// 模块日志器
var ptLog = logger.NewModuleLogger("prompt")

var agentPromptFileWhitelist = []string{"AGENTS.md", "SOUL.md", "TOOLS.md", "IDENTITY.md", "HEARTBEAT.md", "BOOTSTRAP.md"}

type PromptBuilder struct {
	workspace     string
	agentID       string
	dataDir       string
	toolRegistry  *tools.Registry
	skillLoader   *skill.Loader
	userIsolation config.UserIsolationConfig // 用户隔离配置
	bootstrap     config.BootstrapConfig     // Bootstrap 文件长度限制
	memoryConfig  config.MediumTermConfig    // 记忆加载配置
	totalChars    int                        // 当前 prompt 总字符数（用于统计）
	llmProvider   llm.Provider               // LLM 提供商（用于压缩）
	// 阶段2：稳定块预构建缓存
	stableSystemPrompt string // 缓存的稳定系统提示（不依赖运行时状态）
	stablePromptHash   string // 用于检测是否需要重建（基于工具/技能/文件变化）
}

func NewPromptBuilder(workspace string, agentID string, dataDir string, toolReg *tools.Registry, skillLoader *skill.Loader) *PromptBuilder {
	return &PromptBuilder{
		workspace:    workspace,
		agentID:      agentID,
		dataDir:      dataDir,
		toolRegistry: toolReg,
		skillLoader:  skillLoader,
	}
}

// SetUserIsolation 设置用户隔离配置
func (pb *PromptBuilder) SetUserIsolation(cfg config.UserIsolationConfig) {
	pb.userIsolation = cfg
}

// SetBootstrapConfig 设置 Bootstrap 长度限制配置
func (pb *PromptBuilder) SetBootstrapConfig(cfg config.BootstrapConfig) {
	pb.bootstrap = cfg
}

// SetMemoryConfig 设置记忆加载配置
func (pb *PromptBuilder) SetMemoryConfig(cfg config.MediumTermConfig) {
	pb.memoryConfig = cfg
}

// SetLLMProvider 设置 LLM 提供商
func (pb *PromptBuilder) SetLLMProvider(provider llm.Provider) {
	pb.llmProvider = provider
}

// loadFileNoTrack 加载文件但不计入 totalChars (用于压缩层初始化)
func (pb *PromptBuilder) loadFileNoTrack(name string) string {
	path := filepath.Join(pb.workspace, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func (pb *PromptBuilder) Build(_ context.Context, sess *session.Session, userMessage string) ([]models.Message, error) {

	ptLog.Debug("Building prompt", logger.Fields{"workspace": pb.workspace})

	// 重置总字符数计数器
	pb.totalChars = 0

	// 创建用户工作空间（默认启用用户隔离）
	var userWorkspace *UserWorkspace
	userID := pb.privateMemoryUserID(sess)
	if userID != "" {
		userWorkspace = NewUserWorkspace(UserWorkspaceConfig{
			DataDir:    pb.dataDir,
			AgentID:    pb.agentID,
			UserID:     userID,
			Enabled:    true,
			AutoCreate: pb.userIsolation.AutoCreate,
		})
		ptLog.Debug("User isolation enabled", logger.Fields{"user_id": userID, "agent_id": pb.agentID})
	}

	// ========== 阶段1：稳定前缀块（按顺序，最大化 KV Cache 前缀匹配）==========
	var stableParts []string

	// 1. 稳定规则（从 buildSystemBase 拆分出的纯规则部分）
	stableParts = append(stableParts, pb.buildStableRules())

	// 2. 工具定义（稳定）
	if pb.toolRegistry != nil {
		toolSection := pb.buildToolDefinitions()
		if toolSection != "" {
			stableParts = append(stableParts, toolSection)
			ptLog.Debug("Tool definitions added", logger.Fields{"tool_count": len(pb.toolRegistry.List())})
		}
	}

	// 3. Skills 列表（稳定）
	if pb.skillLoader != nil {
		skillSection := pb.buildSkillList()
		if skillSection != "" {
			stableParts = append(stableParts, skillSection)
			ptLog.Debug("Skills added", logger.Fields{"skill_count": len(pb.skillLoader.List())})
		}
	}

	// 4. 固定注入 Agent 级白名单 Markdown 文件（稳定）
	for _, name := range agentPromptFileWhitelist {
		fullPath := filepath.Join(pb.workspace, name)
		if context := pb.buildFileContext("agent", name, fullPath); context != "" {
			stableParts = append(stableParts, context)
			ptLog.Debug("Agent prompt file loaded", logger.Fields{"file": name, "chars": len(context)})
		}
	}

	// ========== 阶段2：动态尾部块（每次请求可能变化）==========
	var dynamicParts []string

	// 6. 系统信息表（动态 - hostname, cwd, time 等）
	dynamicParts = append(dynamicParts, pb.buildDynamicSystemInfo())
	ptLog.Debug("Dynamic system info added")

	if inventory := pb.buildMemoryInventory(userWorkspace); inventory != "" {
		dynamicParts = append(dynamicParts, inventory)
	}

	// 7. 用户 USER.md（动态 - 用户隔离）
	if userWorkspace != nil {
		if _, err := userWorkspace.GetUserMD(); err == nil {
			if context := pb.buildFileContext("user", "USER.md", userWorkspace.GetUserMDPath()); context != "" {
				dynamicParts = append(dynamicParts, context)
				ptLog.Debug("User USER.md loaded", logger.Fields{"user_id": userID, "chars": len(context)})
			}
		}
	}

	// 8. 用户 MEMORY.md（动态 - 用户隔离）
	if userWorkspace != nil {
		if context := pb.buildFileContext("user", "MEMORY.md", userWorkspace.GetUserMemoryPath()); context != "" {
			dynamicParts = append(dynamicParts, context)
			ptLog.Debug("User MEMORY.md loaded", logger.Fields{"user_id": userID, "chars": len(context)})
		}
	}

	// 9. Session start 只注入今天和昨天 daily notes；普通 turn 不注入 daily notes 正文。
	if pb.isSessionStart(sess) {
		if userWorkspace != nil {
			for _, daily := range pb.dailyMemoryFiles(userWorkspace.GetUserMemoryDir()) {
				if context := pb.buildFileContext("user_daily", filepath.ToSlash(filepath.Join("memory", filepath.Base(daily))), daily); context != "" {
					dynamicParts = append(dynamicParts, context)
				}
			}
		}
	}

	// 10. 当前时间（动态）
	dynamicParts = append(dynamicParts, fmt.Sprintf("## Current Time\n\n%s", time.Now().Format(time.RFC3339)))

	if scheduleContext := pb.buildScheduleExecutionContext(sess); scheduleContext != "" {
		dynamicParts = append(dynamicParts, scheduleContext)
	}

	// ========== 组装：稳定部分在前，动态部分在后 ==========
	allParts := append(stableParts, dynamicParts...)

	var messages []models.Message
	systemContent := strings.Join(allParts, "\n\n")
	if systemContent != "" {
		messages = append(messages, models.Message{
			Role:    "system",
			Content: systemContent,
		})
		ptLog.Debug("System prompt built", logger.Fields{
			"chars":         len(systemContent),
			"stable_chars":  len(strings.Join(stableParts, "\n\n")),
			"dynamic_chars": len(strings.Join(dynamicParts, "\n\n")),
		})
	}

	// 11. 会话历史（已包含用户消息，因为 runtime 在调用 Build 前已添加）
	historyMsgs := sess.GetMessages()
	messages = append(messages, historyMsgs...)
	ptLog.Debug("Session history added", logger.Fields{"message_count": len(historyMsgs)})

	ptLog.Info("Prompt complete", logger.Fields{"total_messages": len(messages)})

	// 检查总字符数是否超限
	if pb.bootstrap.MaxTotalChars > 0 && pb.totalChars > pb.bootstrap.MaxTotalChars {
		ptLog.Warn("Bootstrap exceeds limit", logger.Fields{
			"total_chars": pb.totalChars,
			"max_chars":   pb.bootstrap.MaxTotalChars,
		})
	}

	return messages, nil
}

func (pb *PromptBuilder) privateMemoryUserID(sess *session.Session) string {
	if sess == nil {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(sess.GetMetadataValue("session_kind")), "schedule") {
		return ""
	}
	if actorID := strings.TrimSpace(sess.GetMetadataValue("actor_user_id")); actorID != "" {
		return actorID
	}
	return sess.GetUserID()
}

// buildStableRules 构建稳定的规则部分（不依赖运行时状态）
// 这些内容在每次请求中保持不变，可以被 KV Cache 缓存
func (pb *PromptBuilder) buildStableRules() string {
	return fmt.Sprintf(`## Core Behavior
- Respond helpfully and accurately
- Use available tools when needed
- Follow operating instructions from AGENTS.md
- Maintain personality described in SOUL.md
	- Reference user memory and shared knowledge when relevant

## Task Planning (task_plan Tool) - IMPORTANT

### Default Behavior
- If the task is obviously a direct answer or obviously needs only ONE non-task_plan tool call, you may execute directly
- Otherwise, default to task_plan first
- If you expect TWO OR MORE non-task_plan tool calls, use task_plan
- For non-trivial workflows, task_plan is the normal path: investigation, exploration, multi-file reading, read/search/analyze/fix, search/read/modify/test

### Lightweight Probe Is Allowed
- You may do ONE lightweight probe before the first task_plan when needed to discover scope
- Good probes: one grep, one read, one list/query call
- After that probe, if the task is clearly multi-step, call task_plan before continuing

### Why Use task_plan
- Make planning the default for work that is not obviously single-step
- Track progress across multiple tool calls
- Avoid missing steps or repeating work
- Make multi-step work visible to the user
- Keep execution stable when the plan evolves

### Status Values
pending → running → done/skipped

### HARD RULES
1. For non-trivial work, call task_plan early and keep it updated as your understanding improves
2. task_plan may be updated later when scope changes or new subtasks are discovered
3. Only ONE subtask can be "running" at a time
4. Multiple subtasks may be "pending"
5. NEVER reset done/skipped tasks — mark failed tasks as "skipped" or add a new follow-up task instead
6. When failing: try an alternative → if still blocked → skip or append a new approach → continue

### Examples
- Direct question: answer directly or use one tool, no task_plan needed
- Bug investigation: grep code → read key files → inspect related file → explain root cause → use task_plan
- Code change: search → read → edit → test → use task_plan
- Probe then plan: do one grep/read to learn scope, then call task_plan before the remaining tool work
- Plan update: after discovering another file or validation step, update task_plan without resetting completed work

### Failure Handling
Try alternative → if still fails → mark "skipped" or add a new task → continue
NEVER recreate the plan from scratch; NEVER reset completed progress

## Tool Usage Rules
- Only use tools that are listed and available
- Provide clear tool call parameters
- Wait for tool results before responding
- Don't make up tool results
- For file operations, use paths relative to workspace: %s
- For exec tool, check system for default shell

	## Tool Routing: Memory vs Knowledge vs Skill
	- Use memory for user-specific remembered facts that should persist across sessions: user preferences, user constraints, user-specific decisions, and recent user context
	- Use shared knowledge for reusable notes and documents that multiple agents may need to read or update
	- Use skills for reusable step-by-step task instructions or playbooks

	### Which tool to use first
	- If you need a remembered fact about the current user, use memory_search first
	- If you need a shared note, document, checklist, architecture decision, debugging runbook, project convention, or research writeup, use knowledge_tree or knowledge_search first, then knowledge_read
	- For project-level or domain-level questions, do not rely on memory by default; check knowledge first unless the answer is already clearly present in the current conversation
	- If you are about to make a project-specific recommendation, change, or conclusion and it may already be documented, check knowledge first
	- If you need a reusable procedure or role-specific instruction set, use skill_list or skill_detail first, then skill_use only when the skill clearly applies

	### Write behavior
	- Write user-specific profile details, preferences, facts, or constraints by editing the appropriate Markdown memory file with file_write or file_edit
	- Write shared documentation for broader reuse by editing Markdown files under the shared knowledge root with file_write or file_edit when directory permissions allow it
	- Write agent identity only to SOUL.md and agent operating rules only to AGENTS.md
	- Do not write shared project documentation or procedural instructions into memory when they belong in knowledge or a skill
	- When the conversation produces reusable knowledge and you have enough concrete detail, save it by writing or editing a knowledge Markdown file
	- Before creating a new knowledge entry, search knowledge first if a similar decision, checklist, workflow, or note may already exist

	### Preference order
	- Prefer skill tools over searching for procedural instructions in raw files
	- Prefer knowledge tools for shared documentation
	- Prefer memory_search for current-user-specific remembered facts

	### Proactive Knowledge Use
	- Search knowledge proactively before answering questions about project history, architecture, design rationale, troubleshooting steps, workflows, operational checklists, or prior research
	- Write knowledge proactively after producing a reusable artifact: a confirmed design decision, a debugging conclusion, a checklist, a workflow, a reference note, or a research summary
	- Do not write one-off chat fragments to knowledge; save only material that is likely to help in future tasks beyond this single conversation
	- If the content is mainly about the user, store it in memory; if it is mainly about the work itself, store it in knowledge
	- When choosing a knowledge path, prefer a stable top-level category such as decisions/, architecture/, debugging/, workflows/, checklists/, research/, or product/

	## Memory Management

	### When to Record Memory Files
	- type="medium": temporary user context, current task details, next few days
	- type="longterm": user preferences, user-specific constraints, user-specific key decisions, permanent user facts

	### Memory Loading
	- user MEMORY.md (longterm): permanent user-specific information, always visible
	- memory/YYYY-MM-DD.md: recent activities for context`, pb.workspace)
}

// buildDynamicSystemInfo 构建动态的系统信息（每次请求可能变化）
// 这些内容依赖运行时状态，每次请求都可能不同
func (pb *PromptBuilder) buildDynamicSystemInfo() string {
	hostname, _ := os.Hostname()
	cwd, _ := os.Getwd()
	now := time.Now().Format("2006-01-02 15:04:05 MST")

	defaultShell := "sh"
	if runtime.GOOS == "windows" {
		defaultShell = "cmd"
	}

	return fmt.Sprintf(`## System Information

| Property | Value |
|----------|-------|
| **OS** | %s |
| **Architecture** | %s |
| **Hostname** | %s |
| **Working Directory** | %s |
| **Workspace** | %s |
| **Current Time** | %s |
| **Default Shell** | %s |`,
		runtime.GOOS,
		runtime.GOARCH,
		hostname,
		cwd,
		pb.workspace,
		now,
		defaultShell)
}

func (pb *PromptBuilder) buildScheduleExecutionContext(sess *session.Session) string {
	if sess == nil {
		return ""
	}
	if !strings.EqualFold(strings.TrimSpace(sess.GetMetadataValue("session_kind")), "schedule") {
		return ""
	}

	executionSessionKey := strings.TrimSpace(sess.Key)
	originSessionKey := strings.TrimSpace(sess.GetMetadataValue("origin_session_key"))
	agentID := strings.TrimSpace(sess.GetMetadataValue("agent_id"))
	userID := strings.TrimSpace(sess.GetMetadataValue("user_id"))
	taskKind := strings.TrimSpace(sess.GetMetadataValue("task_kind"))

	var parts []string
	parts = append(parts, "## Scheduled Task Context")
	parts = append(parts, "")
	parts = append(parts, "You are currently executing a scheduled task inside a dedicated schedule session.")
	parts = append(parts, "Use this context to decide whether the task requires a user-facing delivery.")
	parts = append(parts, "")
	parts = append(parts, fmt.Sprintf("- `execution_session_key`: `%s`", executionSessionKey))
	if originSessionKey != "" {
		parts = append(parts, fmt.Sprintf("- `origin_session_key`: `%s`", originSessionKey))
	}
	if agentID != "" {
		parts = append(parts, fmt.Sprintf("- `agent_id`: `%s`", agentID))
	}
	if userID != "" {
		parts = append(parts, fmt.Sprintf("- `user_id`: `%s`", userID))
	}
	if taskKind != "" {
		parts = append(parts, fmt.Sprintf("- `task_kind`: `%s`", taskKind))
	}
	parts = append(parts, "")
	parts = append(parts, "If the task clearly requires notifying the user or sending results back, you may call `sessions_send`.")
	parts = append(parts, "`sessions_send` accepts text via `content` (or legacy `message`) and can optionally include a `media` array for images, audio, video, or files.")
	parts = append(parts, "When you want to reply back to the current session, you may omit `session_key`; it defaults to the current session.")
	if originSessionKey != "" {
		parts = append(parts, "When sending a user-facing message for this scheduled task, use `origin_session_key` as the `session_key` so the reply goes back to the original user conversation instead of staying inside the schedule execution session.")
	}
	parts = append(parts, "Do not send a message back unless the task itself implies user-facing delivery.")

	return strings.Join(parts, "\n")
}

// BuildStableSystemPrompt 预构建稳定系统提示（阶段2：缓存优化）
// 在初始化或配置变化时调用一次，后续 Build() 直接使用缓存
// 稳定部分包含：规则、工具定义、技能列表、AGENTS.md、SOUL.md
func (pb *PromptBuilder) BuildStableSystemPrompt() string {
	var parts []string

	// 1. 稳定规则
	parts = append(parts, pb.buildStableRules())

	// 2. 工具定义（稳定）
	if pb.toolRegistry != nil {
		toolSection := pb.buildToolDefinitions()
		if toolSection != "" {
			parts = append(parts, toolSection)
		}
	}

	// 3. Skills 列表（稳定）
	if pb.skillLoader != nil {
		skillSection := pb.buildSkillList()
		if skillSection != "" {
			parts = append(parts, skillSection)
		}
	}

	// 4. AGENTS.md（稳定）
	agentsContent := pb.loadFileNoTrack("AGENTS.md")
	if agentsContent != "" {
		parts = append(parts, "## Operating Instructions\n\n"+agentsContent)
	}

	// 5. SOUL.md（稳定）
	soulContent := pb.loadFileNoTrack("SOUL.md")
	if soulContent != "" {
		parts = append(parts, "## Personality & Tone\n\n"+soulContent)
	}

	pb.stableSystemPrompt = strings.Join(parts, "\n\n")
	ptLog.Debug("Stable system prompt built", logger.Fields{"chars": len(pb.stableSystemPrompt)})
	return pb.stableSystemPrompt
}

func (pb *PromptBuilder) buildToolDefinitions() string {
	toolDefs := pb.toolRegistry.List()
	if len(toolDefs) == 0 {
		return ""
	}

	var parts []string
	parts = append(parts, "## Available Tools\n\nYou have access to the following tools. To use a tool, respond with a tool call.\n")

	for _, def := range toolDefs {
		parts = append(parts, pb.formatToolDefinition(def))
	}

	return strings.Join(parts, "\n")
}

func (pb *PromptBuilder) formatToolDefinition(def models.ToolDefinition) string {
	var parts []string

	// Tool name and description
	desc := def.Function.Description
	if desc == "" {
		desc = "No description available"
	}
	parts = append(parts, fmt.Sprintf("### %s\n\n%s", def.Function.Name, desc))

	// Parameters
	if def.Function.Parameters != nil {
		if props, ok := def.Function.Parameters["properties"].(map[string]interface{}); ok && len(props) > 0 {
			parts = append(parts, "\n**Parameters:**")

			// Get required parameters
			required := make(map[string]bool)
			if req, ok := def.Function.Parameters["required"].([]interface{}); ok {
				for _, r := range req {
					if s, ok := r.(string); ok {
						required[s] = true
					}
				}
			}

			// Format each parameter
			for paramName, paramDef := range props {
				paramInfo := pb.formatParameter(paramName, paramDef, required[paramName])
				parts = append(parts, paramInfo)
			}
		}
	}

	parts = append(parts, "")
	return strings.Join(parts, "\n")
}

func (pb *PromptBuilder) formatParameter(name string, def interface{}, required bool) string {
	paramMap, ok := def.(map[string]interface{})
	if !ok {
		return fmt.Sprintf("- `%s`: (unknown type)", name)
	}

	// Get type
	paramType := "unknown"
	if t, ok := paramMap["type"].(string); ok {
		paramType = t
	}

	// Get description
	paramDesc := ""
	if d, ok := paramMap["description"].(string); ok {
		paramDesc = d
	}

	// Format line
	reqStr := ""
	if required {
		reqStr = " (required)"
	}

	if paramDesc != "" {
		return fmt.Sprintf("- `%s` (%s)%s: %s", name, paramType, reqStr, paramDesc)
	}
	return fmt.Sprintf("- `%s` (%s)%s", name, paramType, reqStr)
}

func (pb *PromptBuilder) buildSkillList() string {
	skills := pb.skillLoader.List()
	if len(skills) == 0 {
		return ""
	}

	var parts []string
	parts = append(parts, "## Available Skills\n\nSkills are optional instruction modules. Use `skill_list` to see available skills, `skill_detail` to inspect one, and `skill_use` to load full instructions only when needed.\n")

	for _, s := range skills {
		desc := s.Description
		if desc == "" {
			desc = "No description"
		}
		parts = append(parts, fmt.Sprintf("- `%s`: %s", s.Name, desc))
	}

	return strings.Join(parts, "\n")
}

func (pb *PromptBuilder) loadFile(name string) string {
	path := filepath.Join(pb.workspace, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	content := string(data)
	originalLen := len(content)

	// 检查文件长度限制
	if pb.bootstrap.MaxFileChars > 0 && originalLen > pb.bootstrap.MaxFileChars {
		// 截断
		content = pb.truncateContent(content, name)
		ptLog.Warn("File truncated", logger.Fields{
			"file":        name,
			"original":    originalLen,
			"truncated":   len(content),
			"max_allowed": pb.bootstrap.MaxFileChars,
		})
	}

	// 更新总字符数
	pb.totalChars += len(content)

	// 检查总字符数警告阈值
	if pb.bootstrap.MaxTotalChars > 0 && pb.bootstrap.WarningThreshold > 0 {
		threshold := int(float64(pb.bootstrap.MaxTotalChars) * pb.bootstrap.WarningThreshold)
		if pb.totalChars >= threshold && pb.totalChars-len(content) < threshold {
			ptLog.Warn("Prompt size threshold reached", logger.Fields{
				"percentage": int(pb.bootstrap.WarningThreshold * 100),
				"current":    pb.totalChars,
				"max":        pb.bootstrap.MaxTotalChars,
			})
		}
	}

	return content
}

type promptFileInfo struct {
	Path     string
	FullPath string
	Scope    string
	Size     int64
	ModTime  time.Time
}

func (pb *PromptBuilder) buildFileContext(scope, relPath, fullPath string) string {
	data, err := os.ReadFile(fullPath)
	if err != nil || len(data) == 0 {
		return ""
	}
	content := string(data)
	originalLen := len(content)
	truncated := false
	if pb.bootstrap.MaxFileChars > 0 && originalLen > pb.bootstrap.MaxFileChars {
		content = pb.truncateContent(content, relPath)
		truncated = true
		ptLog.Warn("File context truncated", logger.Fields{"file": relPath, "original": originalLen, "truncated": len(content)})
	}
	pb.totalChars += len(content)
	lineCount := countLines(content)
	var b strings.Builder
	b.WriteString(fmt.Sprintf("<!-- FILE_CONTEXT scope=%q path=%q full_path=%q lines=%q -->\n", scope, filepath.ToSlash(relPath), fullPath, lineRangeLabel(lineCount)))
	b.WriteString(numberedContent(content))
	if truncated {
		b.WriteString("\nTRUNCATED: use file_read with full_path to inspect full content")
	}
	b.WriteString("\n<!-- END_FILE_CONTEXT -->")
	return b.String()
}

func (pb *PromptBuilder) buildMemoryInventory(userWorkspace *UserWorkspace) string {
	var agentFiles []promptFileInfo
	for _, name := range agentPromptFileWhitelist {
		if info, ok := promptFileStat("agent", name, filepath.Join(pb.workspace, name)); ok {
			agentFiles = append(agentFiles, info)
		}
	}

	var userFiles []promptFileInfo
	var dailyFiles []promptFileInfo
	if userWorkspace != nil {
		if info, ok := promptFileStat("user", "USER.md", userWorkspace.GetUserMDPath()); ok {
			userFiles = append(userFiles, info)
		}
		if info, ok := promptFileStat("user", "MEMORY.md", userWorkspace.GetUserMemoryPath()); ok {
			userFiles = append(userFiles, info)
		}
		dailyFiles = append(dailyFiles, listInventoryFiles(userWorkspace.GetUserMemoryDir(), "memory", "user_daily")...)
	}

	if len(agentFiles) == 0 && len(userFiles) == 0 && len(dailyFiles) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("<!-- MEMORY_INVENTORY agent_id=%q -->\n", pb.agentID))
	writeInventorySection(&b, "Agent memory files", agentFiles)
	writeInventorySection(&b, "User memory files", userFiles)
	writeInventorySection(&b, "Daily notes", dailyFiles)
	b.WriteString("Use memory_search/memory_read to inspect items not injected below.\n")
	b.WriteString("<!-- END_MEMORY_INVENTORY -->")
	return b.String()
}

func promptFileStat(scope, relPath, fullPath string) (promptFileInfo, bool) {
	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		return promptFileInfo{}, false
	}
	return promptFileInfo{Path: filepath.ToSlash(relPath), FullPath: fullPath, Scope: scope, Size: info.Size(), ModTime: info.ModTime()}, true
}

func listInventoryFiles(dir, relPrefix, scope string) []promptFileInfo {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	files := make([]promptFileInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".md" {
			continue
		}
		fullPath := filepath.Join(dir, entry.Name())
		if info, ok := promptFileStat(scope, filepath.ToSlash(filepath.Join(relPrefix, entry.Name())), fullPath); ok {
			files = append(files, info)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

func writeInventorySection(b *strings.Builder, title string, files []promptFileInfo) {
	b.WriteString(title + ":\n")
	if len(files) == 0 {
		b.WriteString("- none\n")
		return
	}
	const maxInventoryFiles = 50
	limit := len(files)
	truncated := false
	if limit > maxInventoryFiles {
		limit = maxInventoryFiles
		truncated = true
	}
	for _, file := range files[:limit] {
		b.WriteString(fmt.Sprintf("- %s | full_path=%q | scope=%s | size=%d | modified=%q\n", file.Path, file.FullPath, file.Scope, file.Size, file.ModTime.Format("2006-01-02 15:04")))
	}
	if truncated {
		b.WriteString("- TRUNCATED: use memory_search to inspect additional files\n")
	}
}

func (pb *PromptBuilder) isSessionStart(sess *session.Session) bool {
	if sess == nil {
		return false
	}
	return len(sess.GetMessages()) <= 1
}

func (pb *PromptBuilder) dailyMemoryFiles(dir string) []string {
	today := time.Now().Format("2006-01-02") + ".md"
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02") + ".md"
	var paths []string
	for _, name := range []string{today, yesterday} {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			paths = append(paths, path)
		}
	}
	return paths
}

func countLines(content string) int {
	if content == "" {
		return 0
	}
	return len(strings.Split(content, "\n"))
}

func lineRangeLabel(lineCount int) string {
	if lineCount <= 0 {
		return "0-0"
	}
	return fmt.Sprintf("1-%d", lineCount)
}

func numberedContent(content string) string {
	lines := strings.Split(content, "\n")
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(fmt.Sprintf("%4d→%s", i+1, line))
	}
	return b.String()
}

// truncateContent 根据截断策略截断内容
func (pb *PromptBuilder) truncateContent(content, filename string) string {
	maxChars := pb.bootstrap.MaxFileChars
	strategy := pb.bootstrap.TruncationStrategy

	if len(content) <= maxChars {
		return content
	}

	var truncated string
	switch strategy {
	case "tail":
		// 保留结尾
		truncated = content[len(content)-maxChars:]
		truncated = "...(truncated)\n" + truncated
	default:
		// 保留开头
		truncated = content[:maxChars]
		truncated = truncated + "\n...(truncated)"
	}

	return truncated
}

func (pb *PromptBuilder) loadRecentMemory() string {
	memDir := filepath.Join(pb.workspace, "memory")
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	var parts []string
	for _, day := range []string{today, yesterday} {
		path := filepath.Join(memDir, day+".md")
		data, err := os.ReadFile(path)
		if err == nil {
			parts = append(parts, fmt.Sprintf("### %s\n%s", day, string(data)))
		}
	}

	return strings.Join(parts, "\n\n")
}
