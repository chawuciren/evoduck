package command

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/pkg/models"
)

// ============================================================================
// ResumeCommand - 恢复历史会话命令
// 用法：
//   /resume              列出最近归档（默认 10 条/页）
//   /resume next         下一页
//   /resume prev         上一页
//   /resume list [page]  直接跳到指定页（1-based）
//   /resume <id>         恢复指定归档（输入归档 ID 末尾几位即可匹配）
// history 作为 /resume 的别名
// ============================================================================

const (
	resumeDefaultPageSize = 10
	resumeCancelTimeout   = 10 * time.Second
	// archiveTitleTimeout 归档标题生成超时。推理模型（GLM-5.2 等）+ 流式调用，
	// reasoning 可能很慢，5 分钟是安全默认（与 /resume 自己的 cancel 窗口解耦，
	// /resume 内部会等标题完成再 SwapMessages，故需要充裕预算）。
	archiveTitleTimeout = 5 * time.Minute
)

// resumePageCursor 每会话的分页游标（命令本身无状态）
var resumePageCursor sync.Map

type ResumeCommand struct{}

func NewResumeCommand() *ResumeCommand { return &ResumeCommand{} }

func (c *ResumeCommand) Name() string { return "resume" }
func (c *ResumeCommand) Description() string {
	return "Browse or restore archived sessions (/history is an alias)"
}
func (c *ResumeCommand) Usage() string {
	return "/resume [next|prev|list <page>|<id>]"
}
func (c *ResumeCommand) RequiredRole() models.Role { return RoleAll }

// Execute 执行 /resume 命令
func (c *ResumeCommand) Execute(ctx *Context) (*Result, error) {
	if ctx.Session == nil || ctx.Gateway == nil {
		return nil, fmt.Errorf("session or gateway unavailable")
	}
	sm := ctx.Gateway.GetSessionManager()
	if sm == nil {
		return nil, fmt.Errorf("session manager unavailable")
	}
	store := sm.ArchiveStore()
	if store == nil {
		return nil, fmt.Errorf("archive store not enabled")
	}

	arg := strings.TrimSpace(ctx.Args)

	// 分支：恢复指定 ID（任何非保留字的输入都按 ID 匹配）
	if arg != "" && arg != "next" && arg != "prev" && arg != "list" && !strings.HasPrefix(arg, "list ") {
		return c.doRestore(ctx, store, arg)
	}

	// 分页
	page := c.loadPageCursor(ctx.SessionKey, ctx.AgentID)
	switch {
	case arg == "next":
		page++
	case arg == "prev":
		if page > 1 {
			page--
		}
	}
	if strings.HasPrefix(arg, "list ") {
		if n, err := parseIntStrict(strings.TrimPrefix(arg, "list ")); err == nil && n >= 1 {
			page = n
		} else {
			return nil, fmt.Errorf("invalid page: %q", strings.TrimPrefix(arg, "list "))
		}
	} else if arg == "list" {
		page = 1
	} else if arg == "" {
		page = 1
	}
	c.savePageCursor(ctx.SessionKey, ctx.AgentID, page)

	return c.renderList(ctx.SessionKey, store, page)
}

// doRestore 恢复指定归档
func (c *ResumeCommand) doRestore(ctx *Context, store *session.ArchiveStore, idHint string) (*Result, error) {
	list, err := store.List(ctx.SessionKey)
	if err != nil {
		return nil, fmt.Errorf("read archive list failed: %w", err)
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("no archived sessions to restore")
	}

	// ID 末尾匹配
	target := -1
	idHint = strings.TrimSpace(idHint)
	for i, item := range list {
		if item.Meta.ID == idHint || strings.HasSuffix(item.Meta.ID, idHint) {
			target = i
			break
		}
	}
	if target < 0 {
		return nil, fmt.Errorf("no archive matched id suffix %q", idHint)
	}
	picked := list[target]

	// 1) 先把当前会话也归档（避免覆盖丢失当前对话）
	curMsgs := ctx.Session.GetMessages()
	if len(curMsgs) > 0 {
		titleCtx, titleCancel := context.WithTimeout(context.Background(), archiveTitleTimeout)
		title := ctx.Gateway.GenerateArchiveTitle(titleCtx, ctx.AgentID, curMsgs)
		titleCancel()
		// 统一走 Manager.ArchiveAndClear（内部 Fix+Save+Clear，与 /new 路径一致）
		if sm := ctx.Gateway.GetSessionManager(); sm != nil {
			_ = sm.ArchiveAndClear(ctx.SessionKey, ctx.AgentID, title)
		}
	}

	// 2) 强制停止当前进行中任务。软切换：超时不阻断（generation 标记会丢弃旧 goroutine 写入）。
	waitCtx, waitCancel := context.WithTimeout(context.Background(), resumeCancelTimeout+2*time.Second)
	defer waitCancel()
	dirty, cancelErr := ctx.Gateway.CancelAndWait(waitCtx, ctx.SessionKey, resumeCancelTimeout)
	if cancelErr != nil && !dirty {
		return nil, fmt.Errorf("failed to stop current task (not switched): %w", cancelErr)
	}
	_ = dirty // 后续 message 里可附加警告，这里不阻断

	// 3) 加载归档消息
	//    （SwapMessages 会全量替换 msgs，无需在此 Fix 当前 session——落盘归档已在第 1 步 Fix 过）
	newMsgs, err := store.Load(ctx.SessionKey, picked.Meta.ID)
	if err != nil {
		return nil, fmt.Errorf("load archive failed: %w", err)
	}
	if len(newMsgs) == 0 {
		return nil, fmt.Errorf("archive is empty")
	}

	// 5) 原子替换（SwapMessages 内部会持久化到 jsonl；generation 自增使旧 goroutine 写入失效）
	ctx.Session.SwapMessages(newMsgs)

	title := escapeMarkdownInline(picked.Meta.Title)
	if title == "" {
		title = "(untitled)"
	}

	msg := fmt.Sprintf("✓ Restored session #%d · `%s` · %d messages\n\n**%s**",
		target+1,
		picked.Meta.CreatedAt.Format("2006-01-02 15:04"),
		len(newMsgs),
		title,
	)

	return NewResultWithAction(msg, "resume_session", map[string]any{
		"session_key":   ctx.SessionKey,
		"archive_id":    picked.Meta.ID,
		"message_count": len(newMsgs),
	}), nil
}

// renderList 渲染归档列表（分页）。输出为 Markdown：标题 + 有序列表（loose list）+
// 分页导航。每条归档一行列表项，Preview（首条用户消息）作为会话主题标识。
// 段落/列表项之间用空行分隔，避免 Markdown 软换行把内容折叠成一行。
func (c *ResumeCommand) renderList(sessionKey string, store *session.ArchiveStore, page int) (*Result, error) {
	list, err := store.List(sessionKey)
	if err != nil {
		return nil, fmt.Errorf("read archive list failed: %w", err)
	}
	total := len(list)
	if total == 0 {
		return NewResult("📭 No archived sessions yet.\n\nTip: running `/new` archives the current conversation, which you can restore later with `/resume`."), nil
	}

	pageSize := resumeDefaultPageSize
	totalPages := (total + pageSize - 1) / pageSize
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * pageSize
	end := start + pageSize
	if end > total {
		end = total
	}
	pageItems := list[start:end]

	var b strings.Builder
	// Header
	b.WriteString("## 📚 Session Archives\n\n")
	sessionWord := "sessions"
	if total == 1 {
		sessionWord = "session"
	}
	fmt.Fprintf(&b, "*%d %s · page %d/%d*\n\n", total, sessionWord, page, totalPages)

	// 列表项：每条归档一行，项间空行（loose list）保证块级换行。
	// Preview 优先（Title 常退化为 "Previous conversation summary:" 噪声）。
	for i, item := range pageItems {
		idx := start + i + 1
		idSuffix := item.Meta.ID
		if len(idSuffix) > 6 {
			idSuffix = idSuffix[len(idSuffix)-6:]
		}
		msgWord := "messages"
		if item.Meta.MessageCount == 1 {
			msgWord = "message"
		}
		title := escapeMarkdownInline(item.Meta.Title)
		if title == "" {
			title = "(untitled)"
		}
		fmt.Fprintf(&b, "%d. `[%s]` · %d %s · `id %s`\n   **%s**\n\n",
			idx,
			item.Meta.CreatedAt.Format("01-02 15:04"),
			item.Meta.MessageCount, msgWord, idSuffix, title,
		)
	}

	// Footer 导航
	b.WriteString("---\n\n")
	lastID := lastIDSuffix(list)
	fmt.Fprintf(&b, "*Restore with `/resume %s`", lastID)
	if page < totalPages {
		b.WriteString(" · `/resume next` for next page")
	}
	if page > 1 {
		b.WriteString(" · `/resume prev` for previous page")
	}
	b.WriteString("*\n")

	return NewResult(b.String()), nil
}

// escapeMarkdownInline 转义 Preview 中可能破坏 Markdown 排版的内联特殊字符
// （`*_\`[]<> 等），防止用户输入的会话内容把列表/斜体/代码等结构搞乱。
func escapeMarkdownInline(s string) string {
	return strings.NewReplacer(
		"\\", "\\\\",
		"`", "\\`",
		"*", "\\*",
		"_", "\\_",
		"[", "\\[",
		"]", "\\]",
		"<", "\\<",
		">", "\\>",
	).Replace(s)
}

// cursorKey 拼接 sessionKey 和 agentID，避免多 agent 共享同一进程时串页。
// （sessionKey 通常已包含 channel+sender，但同一用户切换 agent 时 key 可能不变）
func (c *ResumeCommand) cursorKey(sessionKey, agentID string) string {
	if agentID == "" {
		return sessionKey
	}
	return sessionKey + ":" + agentID
}

func (c *ResumeCommand) loadPageCursor(sessionKey, agentID string) int {
	k := c.cursorKey(sessionKey, agentID)
	if v, ok := resumePageCursor.Load(k); ok {
		if n, ok := v.(int); ok {
			return n
		}
	}
	return 1
}

func (c *ResumeCommand) savePageCursor(sessionKey, agentID string, page int) {
	resumePageCursor.Store(c.cursorKey(sessionKey, agentID), page)
}

func parseIntStrict(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not a number")
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

func lastIDSuffix(list []session.ArchiveEntry) string {
	if len(list) == 0 {
		return ""
	}
	id := list[0].Meta.ID
	if len(id) > 6 {
		return id[len(id)-6:]
	}
	return id
}

// ============================================================================
// HistoryAliasCommand - /history 作为 /resume 的别名（按用户需求覆盖原 history）
// ============================================================================

type HistoryAliasCommand struct {
	*ResumeCommand
}

func NewHistoryAliasCommand() *HistoryAliasCommand {
	return &HistoryAliasCommand{ResumeCommand: NewResumeCommand()}
}

func (c *HistoryAliasCommand) Name() string { return "history" }
func (c *HistoryAliasCommand) Description() string {
	return "Archived sessions (alias of /resume)"
}
func (c *HistoryAliasCommand) Usage() string {
	return "/history [next|prev|list <page>|<id>]  (同 /resume)"
}
