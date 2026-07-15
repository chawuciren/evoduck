package command

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chawuciren/evoduck/pkg/models"
)

// RegisterBuiltinCommands 注册所有内置命令
func RegisterBuiltinCommands(reg *Registry) {
	// 基础命令 (所有用户可用)
	reg.MustRegister(NewHelpCommand(reg))
	reg.MustRegister(NewStatusCommand())
	reg.MustRegister(NewNewCommand())
	reg.MustRegister(NewResetCommand())
	reg.MustRegister(NewHistoryCommand())
	reg.MustRegister(NewModelCommand())
	reg.MustRegister(NewAgentCommand())
	reg.MustRegister(NewAgentsCommand())
	reg.MustRegister(NewExportCommand())
	reg.MustRegister(NewCompressCommand())

	// 高级命令 (需要 employee 或 admin 权限)
	reg.MustRegister(NewModelsCommand())
	reg.MustRegister(NewLogsCommand())
	reg.MustRegister(NewMemoryCommand())
	reg.MustRegister(NewScheduleCommand())
	reg.MustRegister(NewMCPCommand())
}

// ============================================================================
// HelpCommand - 帮助命令
// ============================================================================

type HelpCommand struct {
	registry *Registry
}

func NewHelpCommand(reg *Registry) *HelpCommand {
	return &HelpCommand{registry: reg}
}

func (c *HelpCommand) Name() string              { return "help" }
func (c *HelpCommand) Description() string       { return "显示所有可用命令" }
func (c *HelpCommand) Usage() string             { return "/help [命令名]" }
func (c *HelpCommand) RequiredRole() models.Role { return RoleAll }

func (c *HelpCommand) Execute(ctx *Context) (*Result, error) {
	// 如果有参数，显示特定命令的详细用法
	if ctx.Args != "" {
		h, ok := c.registry.Get(ctx.Args)
		if !ok {
			return nil, fmt.Errorf("未找到命令: /%s", ctx.Args)
		}

		// 检查权限
		if !c.registry.checkRole(h.RequiredRole(), ctx.Role) {
			return nil, fmt.Errorf("权限不足: /%s 需要 %s 权限", ctx.Args, h.RequiredRole())
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("# /%s 命令\n\n", h.Name()))
		sb.WriteString(fmt.Sprintf("**描述**: %s\n\n", h.Description()))
		if h.Usage() != "" {
			sb.WriteString(fmt.Sprintf("**用法**: %s\n\n", h.Usage()))
		}
		if h.RequiredRole() != RoleAll && h.RequiredRole() != "" {
			sb.WriteString(fmt.Sprintf("**权限**: %s\n\n", h.RequiredRole()))
		}
		return NewResult(sb.String()), nil
	}

	// 显示所有可用命令
	commands := c.registry.List(ctx.Role)
	return NewResult(FormatHelpText(commands)), nil
}

// ============================================================================
// StatusCommand - 状态命令
// ============================================================================

type StatusCommand struct{}

func NewStatusCommand() *StatusCommand { return &StatusCommand{} }

func (c *StatusCommand) Name() string              { return "status" }
func (c *StatusCommand) Description() string       { return "显示当前会话状态" }
func (c *StatusCommand) Usage() string             { return "/status" }
func (c *StatusCommand) RequiredRole() models.Role { return RoleAll }

func (c *StatusCommand) Execute(ctx *Context) (*Result, error) {
	var sb strings.Builder
	sb.WriteString("# 会话状态\n\n")

	// Session 信息
	sb.WriteString("## Session\n")
	sb.WriteString(fmt.Sprintf("- **Key**: %s\n", ctx.SessionKey))
	if ctx.Session != nil {
		sb.WriteString(fmt.Sprintf("- **消息数**: %d\n", ctx.Session.MessageCount()))
		sb.WriteString(fmt.Sprintf("- **用户 ID**: %s\n", ctx.Session.GetUserID()))
	} else {
		sb.WriteString("- **状态**: 未初始化\n")
	}

	// Agent 信息
	sb.WriteString("\n## Agent\n")
	sb.WriteString(fmt.Sprintf("- **当前 Agent**: %s\n", ctx.AgentID))

	// LLM 信息
	sb.WriteString("\n## LLM\n")
	if ctx.Gateway != nil {
		provider, model := ctx.Gateway.GetLLMInfo()
		sb.WriteString(fmt.Sprintf("- **Provider**: %s\n", provider))
		sb.WriteString(fmt.Sprintf("- **Model**: %s\n", model))
		sb.WriteString(fmt.Sprintf("- **运行时间**: %s\n", formatDuration(time.Since(time.Unix(ctx.Gateway.GetStartTime(), 0)))))
	}

	// 用户信息
	sb.WriteString("\n## User\n")
	sb.WriteString(fmt.Sprintf("- **Role**: %s\n", ctx.Role))
	sb.WriteString(fmt.Sprintf("- **User ID**: %s\n", ctx.UserID))

	return NewResult(sb.String()), nil
}

// ============================================================================
// NewCommand - 新会话命令
// ============================================================================

type NewCommand struct{}

func NewNewCommand() *NewCommand { return &NewCommand{} }

func (c *NewCommand) Name() string              { return "new" }
func (c *NewCommand) Description() string       { return "开始新会话，清空历史" }
func (c *NewCommand) Usage() string             { return "/new" }
func (c *NewCommand) RequiredRole() models.Role { return RoleAll }

func (c *NewCommand) Execute(ctx *Context) (*Result, error) {
	message := "✓ 已开始新会话"
	if ctx.Session != nil && ctx.Gateway != nil {
		flushResult, err := ctx.Gateway.FlushSessionMemory(ctx.AgentID, ctx.Session, ctx.Role, ctx.UserID)
		message = formatSessionResetMessage(message, flushResult, err)
		ctx.Session.Clear()
	} else if ctx.Session != nil {
		ctx.Session.Clear()
	}

	return NewResultWithAction(message, "new_session", map[string]any{
		"session_key": ctx.SessionKey,
	}), nil
}

// ============================================================================
// ResetCommand - 重置命令 (同 NewCommand)
// ============================================================================

type ResetCommand struct{}

func NewResetCommand() *ResetCommand { return &ResetCommand{} }

func (c *ResetCommand) Name() string              { return "reset" }
func (c *ResetCommand) Description() string       { return "重置会话，清空历史 (同 /new)" }
func (c *ResetCommand) Usage() string             { return "/reset" }
func (c *ResetCommand) RequiredRole() models.Role { return RoleAll }

func (c *ResetCommand) Execute(ctx *Context) (*Result, error) {
	message := "✓ 已重置会话"
	if ctx.Session != nil && ctx.Gateway != nil {
		flushResult, err := ctx.Gateway.FlushSessionMemory(ctx.AgentID, ctx.Session, ctx.Role, ctx.UserID)
		message = formatSessionResetMessage(message, flushResult, err)
		ctx.Session.Clear()
	} else if ctx.Session != nil {
		ctx.Session.Clear()
	}

	return NewResultWithAction(message, "new_session", map[string]any{
		"session_key": ctx.SessionKey,
	}), nil
}

// ============================================================================
// HistoryCommand - 历史命令
// ============================================================================

type HistoryCommand struct{}

func NewHistoryCommand() *HistoryCommand { return &HistoryCommand{} }

func (c *HistoryCommand) Name() string              { return "history" }
func (c *HistoryCommand) Description() string       { return "显示最近 N 条历史消息" }
func (c *HistoryCommand) Usage() string             { return "/history [数量]" }
func (c *HistoryCommand) RequiredRole() models.Role { return RoleAll }

func (c *HistoryCommand) Execute(ctx *Context) (*Result, error) {
	// 解析参数
	limit := 10
	if ctx.Args != "" {
		n, err := strconv.Atoi(ctx.Args)
		if err != nil {
			return nil, fmt.Errorf("无效参数: %s (应为数字)", ctx.Args)
		}
		if n > 0 {
			limit = n
		}
	}

	if ctx.Session == nil {
		return NewResult("⚠️ 会话未初始化"), nil
	}

	msgs := ctx.Session.GetMessages()
	if len(msgs) == 0 {
		return NewResult("⚠️ 无历史消息"), nil
	}

	// 获取最近 N 条
	start := len(msgs) - limit
	if start < 0 {
		start = 0
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# 历史消息 (最近 %d 条)\n\n", limit))

	for i := start; i < len(msgs); i++ {
		m := msgs[i]
		timeStr := m.Timestamp.Format("15:04:05")
		sb.WriteString(fmt.Sprintf("**%s** [%s]:\n", m.Role, timeStr))
		content := m.Content
		if len(content) > 200 {
			content = content[:200] + "..."
		}
		sb.WriteString(content + "\n\n")
	}

	sb.WriteString(fmt.Sprintf("---\n总计: %d 条消息", len(msgs)))
	return NewResult(sb.String()), nil
}

// ============================================================================
// ModelCommand - 当前模型命令
// ============================================================================

type ModelCommand struct{}

func NewModelCommand() *ModelCommand { return &ModelCommand{} }

func (c *ModelCommand) Name() string              { return "model" }
func (c *ModelCommand) Description() string       { return "显示当前使用的模型" }
func (c *ModelCommand) Usage() string             { return "/model" }
func (c *ModelCommand) RequiredRole() models.Role { return RoleAll }

func (c *ModelCommand) Execute(ctx *Context) (*Result, error) {
	if ctx.Gateway == nil {
		return NewResult("⚠️ 无法获取模型信息"), nil
	}

	provider, model := ctx.Gateway.GetLLMInfo()

	var sb strings.Builder
	sb.WriteString("# 当前模型\n\n")
	sb.WriteString(fmt.Sprintf("- **Provider**: %s\n", provider))
	sb.WriteString(fmt.Sprintf("- **Model**: %s\n", model))
	sb.WriteString(fmt.Sprintf("- **Agent**: %s\n", ctx.AgentID))

	return NewResult(sb.String()), nil
}

// ============================================================================
// ModelsCommand - 可用模型列表命令
// ============================================================================

type ModelsCommand struct{}

func NewModelsCommand() *ModelsCommand { return &ModelsCommand{} }

func (c *ModelsCommand) Name() string              { return "models" }
func (c *ModelsCommand) Description() string       { return "显示所有可用模型列表" }
func (c *ModelsCommand) Usage() string             { return "/models [provider]" }
func (c *ModelsCommand) RequiredRole() models.Role { return models.RoleEmployee }

func (c *ModelsCommand) Execute(ctx *Context) (*Result, error) {
	if ctx.Gateway == nil {
		return NewResult("⚠️ 无法获取模型信息"), nil
	}

	providers, err := ctx.Gateway.ListLLMProviders(context.Background())
	if err != nil {
		return nil, err
	}
	if len(providers) == 0 {
		return NewResult("# 可用模型\n\n⚠️ 当前未配置任何 LLM Provider。"), nil
	}

	filter := strings.TrimSpace(ctx.Args)
	var sb strings.Builder
	sb.WriteString("# 可用模型\n\n")
	matched := 0
	for _, provider := range providers {
		if filter != "" && !strings.EqualFold(provider.Name, filter) {
			continue
		}
		matched++
		title := provider.Name
		if provider.IsDefault {
			title += " (default)"
		}
		sb.WriteString("## ")
		sb.WriteString(title)
		sb.WriteString("\n\n")
		if provider.Error != "" {
			sb.WriteString(fmt.Sprintf("- ⚠️ 获取模型失败: %s\n\n", provider.Error))
			continue
		}
		if len(provider.Models) == 0 {
			sb.WriteString("- 无可用模型\n\n")
			continue
		}
		sb.WriteString("| ID | Context | Max Output | Features |\n")
		sb.WriteString("|----|---------|------------|----------|\n")
		for _, model := range provider.Models {
			features := make([]string, 0, 4)
			if model.SupportsTools {
				features = append(features, "tools")
			}
			if model.SupportsStreaming {
				features = append(features, "stream")
			}
			if model.SupportsVision {
				features = append(features, "vision")
			}
			if model.Reasoning {
				features = append(features, "reasoning")
			}
			featureText := "-"
			if len(features) > 0 {
				featureText = strings.Join(features, ", ")
			}
			contextWindow := "-"
			if model.ContextWindow > 0 {
				contextWindow = strconv.Itoa(model.ContextWindow)
			}
			maxTokens := "-"
			if model.MaxTokens > 0 {
				maxTokens = strconv.Itoa(model.MaxTokens)
			}
			sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n", model.ID, contextWindow, maxTokens, featureText))
		}
		sb.WriteString("\n")
	}

	if matched == 0 {
		return NewResult(fmt.Sprintf("# 可用模型\n\n⚠️ 未找到 Provider: `%s`", filter)), nil
	}
	return NewResult(strings.TrimSpace(sb.String())), nil
}

// ============================================================================
// AgentCommand - 切换 Agent 命令
// ============================================================================

type AgentCommand struct{}

func NewAgentCommand() *AgentCommand { return &AgentCommand{} }

func (c *AgentCommand) Name() string              { return "agent" }
func (c *AgentCommand) Description() string       { return "切换到指定 Agent" }
func (c *AgentCommand) Usage() string             { return "/agent <agent_id>" }
func (c *AgentCommand) RequiredRole() models.Role { return RoleAll }

func (c *AgentCommand) Execute(ctx *Context) (*Result, error) {
	if ctx.Args == "" {
		return NewResult("⚠️ 请指定 Agent ID\n\n用法: `/agent <agent_id>`\n\n使用 `/agents` 查看所有可用 Agent。"), nil
	}

	targetAgentID := ctx.Args

	// 验证 Agent 是否存在
	if ctx.Gateway != nil {
		agentMgr := ctx.Gateway.GetAgentManager()
		if agentMgr != nil {
			_, err := agentMgr.Get(targetAgentID)
			if err != nil {
				return nil, fmt.Errorf("Agent 不存在: %s", targetAgentID)
			}
		}
	}

	return NewResultWithAction(
		fmt.Sprintf("✓ 已切换到 Agent: %s", targetAgentID),
		"switch_agent",
		map[string]any{"agent_id": targetAgentID},
	), nil
}

// ============================================================================
// AgentsCommand - Agent 列表命令
// ============================================================================

type AgentsCommand struct{}

func NewAgentsCommand() *AgentsCommand { return &AgentsCommand{} }

func (c *AgentsCommand) Name() string              { return "agents" }
func (c *AgentsCommand) Description() string       { return "显示所有可用 Agent" }
func (c *AgentsCommand) Usage() string             { return "/agents" }
func (c *AgentsCommand) RequiredRole() models.Role { return RoleAll }

func (c *AgentsCommand) Execute(ctx *Context) (*Result, error) {
	var sb strings.Builder
	sb.WriteString("# 可用 Agent\n\n")

	if ctx.Gateway == nil {
		sb.WriteString("⚠️ 无法获取 Agent 信息")
		return NewResult(sb.String()), nil
	}

	agentMgr := ctx.Gateway.GetAgentManager()
	if agentMgr == nil {
		sb.WriteString("⚠️ Agent Manager 未初始化")
		return NewResult(sb.String()), nil
	}

	agents := agentMgr.List()
	if len(agents) == 0 {
		sb.WriteString("⚠️ 无可用 Agent")
		return NewResult(sb.String()), nil
	}

	sb.WriteString("| ID | Role | Provider | Model |\n")
	sb.WriteString("|----|------|----------|-------|\n")

	for _, a := range agents {
		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n", a.ID, a.Role, a.Provider, a.Model))
	}

	sb.WriteString(fmt.Sprintf("\n---\n总计: %d 个 Agent", len(agents)))
	sb.WriteString("\n\n💡 使用 `/agent <id>` 切换 Agent")
	return NewResult(sb.String()), nil
}

// ============================================================================
// ExportCommand - 导出会话命令
// ============================================================================

type ExportCommand struct{}

func NewExportCommand() *ExportCommand { return &ExportCommand{} }

func (c *ExportCommand) Name() string              { return "export" }
func (c *ExportCommand) Description() string       { return "导出当前会话为 JSON" }
func (c *ExportCommand) Usage() string             { return "/export" }
func (c *ExportCommand) RequiredRole() models.Role { return RoleAll }

func (c *ExportCommand) Execute(ctx *Context) (*Result, error) {
	if ctx.Session == nil {
		return NewResult("⚠️ 会话未初始化"), nil
	}

	msgs := ctx.Session.GetMessages()
	if len(msgs) == 0 {
		return NewResult("⚠️ 无消息可导出"), nil
	}

	// 构建导出数据
	exportData := map[string]interface{}{
		"session_key":   ctx.SessionKey,
		"agent_id":      ctx.AgentID,
		"exported_at":   time.Now().Format(time.RFC3339),
		"message_count": len(msgs),
		"messages":      msgs,
	}

	jsonData, err := json.MarshalIndent(exportData, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("导出失败: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("# 导出成功\n\n")
	sb.WriteString(fmt.Sprintf("- **Session**: %s\n", ctx.SessionKey))
	sb.WriteString(fmt.Sprintf("- **消息数**: %d\n", len(msgs)))
	sb.WriteString(fmt.Sprintf("- **时间**: %s\n", time.Now().Format("2006-01-02 15:04:05")))
	sb.WriteString("\n```json\n")
	sb.WriteString(string(jsonData))
	sb.WriteString("\n```\n")

	return NewResult(sb.String()), nil
}

// ============================================================================
// CompressCommand - 上下文压缩命令
// ============================================================================

type CompressCommand struct{}

func NewCompressCommand() *CompressCommand { return &CompressCommand{} }

func (c *CompressCommand) Name() string              { return "compress" }
func (c *CompressCommand) Description() string       { return "手动触发会话压缩" }
func (c *CompressCommand) Usage() string             { return "/compress" }
func (c *CompressCommand) RequiredRole() models.Role { return RoleAll }

func (c *CompressCommand) Execute(ctx *Context) (*Result, error) {
	if ctx.Session == nil {
		return NewResult("⚠️ 会话未初始化"), nil
	}
	if ctx.Gateway == nil {
		return NewResult("⚠️ Gateway 未连接，无法执行上下文压缩"), nil
	}

	result, err := ctx.Gateway.CompactSession(ctx.AgentID, ctx.Session)
	if err != nil {
		return nil, err
	}

	var sb strings.Builder
	sb.WriteString("# 上下文压缩\n\n")
	if result == nil {
		sb.WriteString("⚠️ 未获取到压缩结果")
		return NewResult(sb.String()), nil
	}
	if result.Skipped {
		sb.WriteString("⚠️ 未执行压缩")
		if result.SkippedReason != "" {
			sb.WriteString(fmt.Sprintf("\n\n原因: %s", result.SkippedReason))
		}
		return NewResult(sb.String()), nil
	}

	sb.WriteString("✓ 已执行上下文压缩\n\n")
	sb.WriteString(fmt.Sprintf("- **压缩前消息数**: %d\n", result.BeforeMessages))
	sb.WriteString(fmt.Sprintf("- **压缩后消息数**: %d\n", result.AfterMessages))
	if result.SummaryInserted {
		sb.WriteString("- **摘要写入**: 是\n")
	} else {
		sb.WriteString("- **摘要写入**: 否\n")
	}
	return NewResult(sb.String()), nil
}

// ============================================================================
// LogsCommand - 日志查看命令
// ============================================================================

type LogsCommand struct{}

func NewLogsCommand() *LogsCommand { return &LogsCommand{} }

func (c *LogsCommand) Name() string              { return "logs" }
func (c *LogsCommand) Description() string       { return "查看系统日志" }
func (c *LogsCommand) Usage() string             { return "/logs [level] [limit]" }
func (c *LogsCommand) RequiredRole() models.Role { return models.RoleAdmin }

func (c *LogsCommand) Execute(ctx *Context) (*Result, error) {
	// 解析参数
	level := ""
	limit := 20

	if ctx.Args != "" {
		parts := strings.Split(ctx.Args, " ")
		if len(parts) > 0 && parts[0] != "" {
			level = parts[0]
		}
		if len(parts) > 1 {
			n, err := strconv.Atoi(parts[1])
			if err == nil && n > 0 {
				limit = n
			}
		}
	}

	if ctx.Gateway == nil {
		return NewResult("⚠️ 无法获取日志"), nil
	}

	logs := ctx.Gateway.GetLogs(level, limit)

	if len(logs) == 0 {
		return NewResult("⚠️ 无日志记录"), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# 系统日志 (%d 条)\n\n", len(logs)))

	for _, log := range logs {
		timeStr := time.Unix(log.Time/1000, 0).Format("15:04:05")
		sb.WriteString(fmt.Sprintf("- [%s] **%s**: %s\n", timeStr, log.Level, log.Message))
	}

	return NewResult(sb.String()), nil
}

// ============================================================================
// MemoryCommand - 记忆系统状态命令
// ============================================================================

type MemoryCommand struct{}

func NewMemoryCommand() *MemoryCommand { return &MemoryCommand{} }

func (c *MemoryCommand) Name() string { return "memory" }
func (c *MemoryCommand) Description() string {
	return "显示记忆系统状态或触发系统整理任务"
}
func (c *MemoryCommand) Usage() string             { return "/memory [extract|experience]" }
func (c *MemoryCommand) RequiredRole() models.Role { return models.RoleEmployee }

func (c *MemoryCommand) Execute(ctx *Context) (*Result, error) {
	if ctx.Args == "extract" {
		if ctx.Gateway == nil {
			return NewResult("⚠️ Gateway 未连接，无法触发 memory curation"), nil
		}
		result, err := ctx.Gateway.RunMemoryCuration()
		if err != nil {
			return nil, err
		}
		return NewResult(fmt.Sprintf("✓ 已触发小时级 memory curation\n\n- Schedule: `%s`\n- Session: `%s`\n- Task kind: `%s`", result.ScheduleID, result.SessionKey, result.TaskKind)), nil
	}
	if ctx.Args == "experience" {
		if ctx.Gateway == nil {
			return NewResult("⚠️ Gateway 未连接，无法触发 experience curation"), nil
		}
		result, err := ctx.Gateway.RunExperienceCuration()
		if err != nil {
			return nil, err
		}
		return NewResult(fmt.Sprintf("✓ 已触发天级 experience curation\n\n- Schedule: `%s`\n- Session: `%s`\n- Task kind: `%s`", result.ScheduleID, result.SessionKey, result.TaskKind)), nil
	}
	if ctx.Args == "cleanup" {
		return NewResult("⚠️ `/memory cleanup` 已停用。请使用 `/memory experience` 触发天级经验/知识/技能整理。"), nil
	}

	var sb strings.Builder
	sb.WriteString("# 记忆系统状态\n\n")
	sb.WriteString("## Session 记忆\n")
	if ctx.Session != nil {
		sb.WriteString(fmt.Sprintf("- **消息数**: %d\n", ctx.Session.MessageCount()))
		sb.WriteString(fmt.Sprintf("- **用户 ID**: %s\n", ctx.Session.GetUserID()))
	} else {
		sb.WriteString("- 未初始化\n")
	}

	sb.WriteString("\n## User 记忆\n")
	sb.WriteString(fmt.Sprintf("- **Agent ID**: %s\n", ctx.AgentID))

	if ctx.Gateway != nil {
		memStatus := ctx.Gateway.GetMemoryStatus(ctx.AgentID, ctx.UserID)
		if memStatus != nil {
			if memStatus.MemoryMDEnabled {
				sizeKB := float64(memStatus.MemoryMDSize) / 1024
				sb.WriteString(fmt.Sprintf("- **MEMORY.md**: ✅ 已存在 (%.1f KB)\n", sizeKB))
			} else {
				sb.WriteString("- **MEMORY.md**: ⚪ 未创建\n")
			}
		} else {
			sb.WriteString("- **MEMORY.md**: 无法获取状态\n")
		}

		sb.WriteString("\n## 系统整理命令\n")
		sb.WriteString("- `/memory extract`: 触发小时级 memory curation\n")
		sb.WriteString("- `/memory experience`: 触发天级 experience curation\n")

		sb.WriteString("\n## Scheduler 任务\n")
		jobs := ctx.Gateway.ListSchedulerJobs()
		if len(jobs) == 0 {
			sb.WriteString("- 当前无已注册任务\n")
		} else {
			for _, job := range jobs {
				status := "disabled"
				if job.Enabled {
					status = "enabled"
				}
				sb.WriteString(fmt.Sprintf("- **%s** (%s): `%s` [%s]\n", job.ID, job.Scope, job.Schedule, status))
				if job.Description != "" {
					sb.WriteString(fmt.Sprintf("  - %s\n", job.Description))
				}
			}
		}
	} else {
		sb.WriteString("- **MEMORY.md**: 无法获取状态 (Gateway 未连接)\n")
		sb.WriteString("\n## Scheduler 任务\n- 无法获取状态 (Gateway 未连接)\n")
	}

	sb.WriteString("\n💡 记忆是普通 Markdown 文件；使用 `file_write` 或 `file_edit` 写入，使用 `memory_search` / `memory_read` 检索和读取。")
	return NewResult(sb.String()), nil
}

// ============================================================================
// 辅助函数
// ============================================================================

func formatSessionResetMessage(base string, flushResult *MemoryFlushResult, flushErr error) string {
	if flushErr != nil {
		return base + "（记忆同步未完成）"
	}
	if flushResult == nil || flushResult.Skipped {
		return base
	}
	if flushResult.Flushed && flushResult.LongtermCount == 0 && flushResult.MediumCount == 0 {
		return base + "（已完成记忆整理）"
	}
	parts := make([]string, 0, 2)
	if flushResult.LongtermCount > 0 {
		parts = append(parts, fmt.Sprintf("%d 条长期记忆", flushResult.LongtermCount))
	}
	if flushResult.MediumCount > 0 {
		parts = append(parts, fmt.Sprintf("%d 条近期记忆", flushResult.MediumCount))
	}
	if len(parts) == 0 {
		return base
	}
	return fmt.Sprintf("%s（已同步 %s）", base, strings.Join(parts, "，"))
}

func formatUnixStatusTime(ts int64) string {
	if ts <= 0 {
		return "未执行"
	}
	return time.Unix(ts, 0).Format("2006-01-02 15:04:05")
}

// formatMCPUnixTime 把 Unix 时间戳格式化为人类可读时间（用于 /mcp 命令输出）。
func formatMCPUnixTime(ts int64) string {
	if ts <= 0 {
		return "-"
	}
	return time.Unix(ts, 0).Format("01-02 15:04:05")
}

// ============================================================================
// ScheduleCommand - 定时任务命令
// ============================================================================

type ScheduleCommand struct{}

func NewScheduleCommand() *ScheduleCommand { return &ScheduleCommand{} }

func (c *ScheduleCommand) Name() string        { return "schedule" }
func (c *ScheduleCommand) Description() string { return "管理用户级定时任务" }
func (c *ScheduleCommand) Usage() string {
	return "/schedule [list|enable <id>|disable <id>|delete <id>]"
}
func (c *ScheduleCommand) RequiredRole() models.Role { return models.RoleEmployee }

func (c *ScheduleCommand) Execute(ctx *Context) (*Result, error) {
	if ctx.Gateway == nil {
		return NewResult("⚠️ Gateway 未连接，无法管理定时任务"), nil
	}
	args := strings.Fields(ctx.Args)
	if len(args) == 0 || args[0] == "list" {
		schedules := ctx.Gateway.ListSchedules(ctx.AgentID, ctx.UserID)
		var sb strings.Builder
		sb.WriteString("# 用户定时任务\n\n")
		if len(schedules) == 0 {
			sb.WriteString("- 当前无定时任务\n")
			return NewResult(sb.String()), nil
		}
		for _, schedule := range schedules {
			status := "disabled"
			if schedule.Enabled {
				status = "enabled"
			}
			sb.WriteString(fmt.Sprintf("- **%s** (%s): `%s` [%s]\n", schedule.ID, schedule.Scope, schedule.Schedule, status))
			if schedule.Description != "" {
				sb.WriteString(fmt.Sprintf("  - %s\n", schedule.Description))
			}
			if schedule.LastError != "" {
				sb.WriteString(fmt.Sprintf("  - last_error: %s\n", schedule.LastError))
			}
		}
		return NewResult(sb.String()), nil
	}
	if len(args) < 2 {
		return NewResult("⚠️ 用法: /schedule [list|enable <id>|disable <id>|delete <id>]"), nil
	}
	id := args[1]
	switch args[0] {
	case "enable":
		if err := ctx.Gateway.SetScheduleEnabled(ctx.AgentID, ctx.UserID, id, true); err != nil {
			return nil, err
		}
		return NewResult(fmt.Sprintf("✓ 已启用定时任务: %s", id)), nil
	case "disable":
		if err := ctx.Gateway.SetScheduleEnabled(ctx.AgentID, ctx.UserID, id, false); err != nil {
			return nil, err
		}
		return NewResult(fmt.Sprintf("✓ 已禁用定时任务: %s", id)), nil
	case "delete":
		if err := ctx.Gateway.DeleteSchedule(ctx.AgentID, ctx.UserID, id); err != nil {
			return nil, err
		}
		return NewResult(fmt.Sprintf("✓ 已删除定时任务: %s", id)), nil
	default:
		return NewResult("⚠️ 不支持的子命令"), nil
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d秒", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%d分钟", int(d.Minutes()))
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	return fmt.Sprintf("%d小时%d分钟", hours, mins)
}

// ============================================================================
// MCPCommand - MCP server 状态与重连命令
// ============================================================================

// MCPCommand 查看 MCP server 连接状态 / 触发重连。
type MCPCommand struct{}

func NewMCPCommand() *MCPCommand { return &MCPCommand{} }

func (c *MCPCommand) Name() string        { return "mcp" }
func (c *MCPCommand) Description() string { return "Show MCP server connection status or reconnect failed servers" }
func (c *MCPCommand) Usage() string {
	return "/mcp [status|reconnect [name]]"
}
func (c *MCPCommand) RequiredRole() models.Role { return models.RoleEmployee }

func (c *MCPCommand) Execute(ctx *Context) (*Result, error) {
	if ctx.Gateway == nil {
		return NewResult("⚠️ Gateway not connected, cannot get MCP status"), nil
	}

	args := strings.Fields(ctx.Args)
	// 默认子命令为 status
	sub := "status"
	if len(args) > 0 {
		sub = args[0]
	}

	switch sub {
	case "status", "":
		return NewResult(formatMCPStatus(ctx.Gateway.GetMCPStatus())), nil
	case "reconnect", "connect", "retry":
		target := ""
		if len(args) > 1 {
			target = strings.TrimSpace(args[1])
		}
		// 主结果：立即返回"已触发哪些 server"。每个 server 的重连结果异步走 FollowUps 通道逐条反馈。
		followUps := make(chan string, 16)
		triggered := ctx.Gateway.ReconnectMCP(ctx.Ctx, target, func(s MCPServerStatus) {
			followUps <- formatMCPReconnectResult(s)
		})
		// 单独 goroutine 等待所有 server 的回调完成（或超时），然后关闭通道。
		// 关闭后 gateway 的 FollowUps 读取循环退出，结束反馈。
		go func(count int) {
			timer := time.NewTimer(180 * time.Second)
			defer timer.Stop()
			received := 0
		waitLoop:
			for received < count {
				select {
				case <-followUps:
					received++
				case <-timer.C:
					break waitLoop
				}
			}
			close(followUps)
		}(len(triggered))

		var sb strings.Builder
		sb.WriteString("# MCP Reconnect Triggered\n\n")
		if len(triggered) == 0 {
			sb.WriteString("✅ All MCP servers are already connected — nothing to reconnect.\n")
			sb.WriteString("\n💡 Use `/mcp reconnect all` to force reconnect every server.\n")
			return NewResult(sb.String()), nil
		}
		mode := "failed / not-connected servers"
		if target == "all" {
			mode = "all servers (forced)"
		} else if target != "" {
			mode = fmt.Sprintf("server `%s`", target)
		}
		sb.WriteString(fmt.Sprintf("Reconnecting %s in the background:\n\n", mode))
		for _, n := range triggered {
			sb.WriteString(fmt.Sprintf("- `%s`\n", n))
		}
		sb.WriteString("\n💡 Each server's result will be reported below as it completes.\n")
		return &Result{Content: sb.String(), ActionData: make(map[string]any), FollowUps: followUps}, nil
	default:
		return NewResult("⚠️ Usage: /mcp [status|reconnect [name]]"), nil
	}
}

// formatMCPReconnectResult 渲染单个 server 重连完成后的反馈消息。
func formatMCPReconnectResult(s MCPServerStatus) string {
	var sb strings.Builder
	label := mcpStateLabel(s.State, s.Online)
	if s.Online {
		sb.WriteString(fmt.Sprintf("# ✅ %s reconnected\n\n", s.Name))
	} else {
		sb.WriteString(fmt.Sprintf("# ❌ %s reconnect failed\n\n", s.Name))
	}
	sb.WriteString(fmt.Sprintf("- **Status**: %s\n", label))
	sb.WriteString(fmt.Sprintf("- **Tools**: %d\n", s.ToolCount))
	if s.Server != "" {
		sb.WriteString(fmt.Sprintf("- **Server**: %s\n", s.Server))
		if s.Version != "" {
			sb.WriteString(fmt.Sprintf("- **Version**: %s\n", s.Version))
		}
	}
	if s.Error != "" {
		errMsg := s.Error
		if len(errMsg) > 300 {
			errMsg = errMsg[:300] + "..."
		}
		sb.WriteString(fmt.Sprintf("- **Error**: %s\n", errMsg))
	}
	sb.WriteString(fmt.Sprintf("- **Attempts**: %d\n", s.Attempts))
	return sb.String()
}

// formatMCPStatus 把 MCP 状态快照渲染为 Markdown 表格。
func formatMCPStatus(snap MCPStatusSnapshot) string {
	var sb strings.Builder
	sb.WriteString("# MCP Server Status\n\n")
	if len(snap.Servers) == 0 {
		sb.WriteString("⚪ No enabled MCP servers configured.\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("- **Online**: %d / %d\n", snap.Online, snap.Total))
	if snap.Connecting > 0 {
		sb.WriteString(fmt.Sprintf("- **Connecting**: %d\n", snap.Connecting))
	}
	if snap.Failed > 0 {
		sb.WriteString(fmt.Sprintf("- **Failed**: %d\n", snap.Failed))
	}
	sb.WriteString("\n| Name | Status | Tools | Server | Version | Connected |\n")
	sb.WriteString("|------|--------|-------|--------|---------|-----------|\n")

	for _, s := range snap.Servers {
		statusText := mcpStateLabel(s.State, s.Online)
		serverName := s.Server
		if serverName == "" {
			serverName = "-"
		}
		version := s.Version
		if version == "" {
			version = "-"
		}
		connectedAt := formatMCPUnixTime(s.ConnectedAt)
		sb.WriteString(fmt.Sprintf("| %s | %s | %d | %s | %s | %s |\n", s.Name, statusText, s.ToolCount, serverName, version, connectedAt))
	}

	sb.WriteString("\n**Legend**: 🟢 online / 🔵 connecting / 🟡 reconnecting / 🔴 failed\n\n")
	sb.WriteString("💡 Use `/mcp reconnect` to reconnect all, or `/mcp reconnect <name>` for a specific server.\n")
	return sb.String()
}

// mcpStateLabel 把状态码映射为带 emoji 的可读标签。
func mcpStateLabel(state string, online bool) string {
	switch strings.ToLower(state) {
	case "connected":
		return "🟢 online"
	case "connecting":
		return "🔵 connecting"
	case "reconnecting":
		return "🟡 reconnecting"
	case "failed":
		return "🔴 failed"
	case "pending":
		return "⚪ pending"
	case "closed":
		return "⚫ closed"
	case "disabled":
		return "disabled"
	default:
		if online {
			return "🟢 online"
		}
		return state
	}
}
