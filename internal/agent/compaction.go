package agent

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
)

// Compactor 上下文压缩器
type Compactor struct {
	maxMessages        int          // 最大消息数
	maxTokens          int          // 最大 Token 数
	keepRecent         int          // 保留最近消息数
	llmProvider        llm.Provider // LLM 提供商（用于生成摘要和 memory flush）
	workspace          string       // Agent workspace 路径
	flushBeforeCompact bool         // 压缩前是否自动 flush
	preCompactCurator  PreCompactCurator
	summaryGenerator   SummaryGenerator
	notifier           CompactionNotifier
}

type PreCompactCurator func(ctx context.Context, sess *session.Session, msgs []models.Message) (string, error)
type SummaryGenerator func(ctx context.Context, sess *session.Session, msgs []models.Message, flushReport string) (string, error)
type CompactionNotifier func(sess *session.Session, mode string)

type compactionNeed struct {
	Needed          bool
	Reason          string
	MessageCount    int
	MaxMessages     int
	EstimatedTokens int
	MaxTokens       int
}

// CompactorConfig 压缩器配置
type CompactorConfig struct {
	MaxMessages        int    // 最大消息数
	MaxTokens          int    // 最大 Token 数
	KeepRecent         int    // 保留最近消息数
	Workspace          string // Agent workspace
	FlushBeforeCompact bool   // 压缩前自动 flush
}

// NewCompactor 创建压缩器
func NewCompactor(provider llm.Provider, config CompactorConfig) *Compactor {
	// 设置默认值
	if config.MaxMessages == 0 {
		config.MaxMessages = 200
	}
	if config.MaxTokens == 0 {
		config.MaxTokens = 128000
	}
	if config.KeepRecent == 0 {
		config.KeepRecent = 10
	}
	return &Compactor{
		maxMessages:        config.MaxMessages,
		maxTokens:          config.MaxTokens,
		keepRecent:         config.KeepRecent,
		llmProvider:        provider,
		workspace:          config.Workspace,
		flushBeforeCompact: config.FlushBeforeCompact,
	}
}

func (c *Compactor) SetPreCompactCurator(curator PreCompactCurator) {
	c.preCompactCurator = curator
}

func (c *Compactor) SetSummaryGenerator(generator SummaryGenerator) {
	c.summaryGenerator = generator
}

func (c *Compactor) SetNotifier(notifier CompactionNotifier) {
	c.notifier = notifier
}

// ShouldCompact 判断是否需要压缩
func (c *Compactor) ShouldCompact(sess *session.Session) bool {
	return c.compactionNeed(sess).Needed
}

func (c *Compactor) compactionNeed(sess *session.Session) compactionNeed {
	if sess == nil {
		return compactionNeed{}
	}
	msgCount := sess.MessageCount()
	if msgCount > c.maxMessages {
		return compactionNeed{Needed: true, Reason: "message_count", MessageCount: msgCount, MaxMessages: c.maxMessages, MaxTokens: c.maxTokens}
	}

	// 使用更精确的 Token 估算方法
	msgs := sess.GetMessages()
	estimatedTokens := estimateMessagesTokens(msgs)

	return compactionNeed{
		Needed:          estimatedTokens > c.maxTokens,
		Reason:          "estimated_tokens",
		MessageCount:    msgCount,
		MaxMessages:     c.maxMessages,
		EstimatedTokens: estimatedTokens,
		MaxTokens:       c.maxTokens,
	}
}

// estimateMessagesTokens 使用混合字符比例估算消息的 Token 数
// 中文约 2 字符 = 1 token，英文约 4 字符 = 1 token
func estimateMessagesTokens(msgs []models.Message) int {
	totalTokens := 0
	for _, m := range msgs {
		// 计算内容 token：根据中英文比例动态估算
		totalTokens += estimateTextTokens(m.Content)
		totalTokens += estimateMessageMediaTokens(m.Media)
		// 角色和元数据也占用 Token
		totalTokens += 10 // role overhead
		if len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				totalTokens += estimateTextTokens(tc.Function.Name) + estimateTextTokens(tc.Function.Arguments) + 30 // tool call overhead
			}
		}
	}
	return totalTokens
}

func estimateMessageMediaTokens(media []models.OutgoingMedia) int {
	total := 0
	for _, item := range media {
		bytes := mediaByteSize(item)
		if bytes <= 0 {
			continue
		}
		total += estimateImageTokensFromBytes(bytes)
	}
	return total
}

func mediaByteSize(media models.OutgoingMedia) int64 {
	if media.FileSize > 0 {
		return media.FileSize
	}
	if path := strings.TrimSpace(media.Path); path != "" {
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			return info.Size()
		}
	}
	if data := strings.TrimSpace(media.Data); data != "" {
		decodedLen := base64.StdEncoding.DecodedLen(len(data))
		if decodedLen > 0 {
			return int64(decodedLen)
		}
	}
	return 0
}

func estimateImageTokensFromBytes(size int64) int {
	if size <= 0 {
		return 0
	}
	tokens := int(size / 768)
	if size%768 != 0 {
		tokens++
	}
	if tokens < 32 {
		return 32
	}
	return tokens
}

// estimateTextTokens 根据字符类型估算文本的 Token 数
func estimateTextTokens(text string) int {
	if text == "" {
		return 0
	}

	// 统计中文字符和非中文字符
	chineseCount := 0
	nonChineseCount := 0

	for _, r := range text {
		if r >= 0x4E00 && r <= 0x9FFF { // CJK Unified Ideographs range
			chineseCount++
		} else {
			nonChineseCount++
		}
	}

	// 中文: ~2 chars = 1 token
	// 英文/其他: ~4 chars = 1 token
	chineseTokens := chineseCount / 2
	nonChineseTokens := nonChineseCount / 4

	// 加上 10% 的缓冲
	total := chineseTokens + nonChineseTokens
	return total + total/10
}

// Compact 执行压缩
func (c *Compactor) Compact(ctx context.Context, sess *session.Session) error {
	if !c.ShouldCompact(sess) {
		return nil
	}
	return c.compact(ctx, sess, "automatic")
}

// ForceCompact performs manual compaction even when thresholds are not exceeded.
func (c *Compactor) ForceCompact(ctx context.Context, sess *session.Session) error {
	if sess == nil {
		return fmt.Errorf("session is required")
	}
	return c.compact(ctx, sess, "manual")
}

func (c *Compactor) compact(ctx context.Context, sess *session.Session, mode string) error {
	if sess == nil {
		return fmt.Errorf("session is required")
	}

	msgs := sess.GetMessages()
	if len(msgs) <= c.keepRecent {
		return nil
	}

	// 保留最近的 replay/tool 对话链，避免压缩切断 provider 所需的回放上下文。
	effectiveKeepRecent := effectiveKeepRecentForReplay(msgs, c.keepRecent)
	splitIdx := len(msgs) - effectiveKeepRecent
	oldMsgs := msgs[:splitIdx]
	recentMsgs := msgs[splitIdx:]

	logger.Info("Starting context compaction", logger.Fields{
		"total_messages":      len(msgs),
		"messages_to_compact": len(oldMsgs),
		"messages_to_keep":    len(recentMsgs),
	})

	// memory_policy=ignore sessions may still be compacted, but their internal
	// execution trace must not be persisted into memory/knowledge by compaction.
	skipFlush := strings.EqualFold(strings.TrimSpace(sess.GetMetadataValue("memory_policy")), "ignore")
	flushReport := ""
	if c.flushBeforeCompact && c.preCompactCurator != nil && !skipFlush {
		logger.Info("Starting pre-compaction memory curation", logger.Fields{
			"session_key":          sess.Key,
			"messages_to_curate":   len(oldMsgs),
			"messages_to_keep":     len(recentMsgs),
			"flush_before_compact": c.flushBeforeCompact,
		})
		report, err := c.preCompactCurator(ctx, sess, oldMsgs)
		flushReport = report
		if err != nil {
			logger.Error("Pre-compaction memory curation failed (continuing compaction)", logger.Fields{
				"error": err.Error(),
			})
			// 不中断流程，继续压缩
		} else {
			logger.Info("Pre-compaction memory curation completed", logger.Fields{
				"session_key":   sess.Key,
				"report_length": len(strings.TrimSpace(report)),
			})
		}
	}

	// 生成摘要。失败或空摘要时跳过本次压缩，保留原始上下文，等待后续再次触发。
	summary, err := c.generateSummary(ctx, sess, oldMsgs, flushReport)
	if err != nil {
		logger.Error("Failed to generate summary; skipping compaction", logger.Fields{"error": err.Error()})
		return err
	}
	if strings.TrimSpace(summary) == "" {
		logger.Warn("Generated empty summary; skipping compaction", logger.Fields{"session_key": sess.Key})
		return fmt.Errorf("empty compaction summary")
	}

	// 替换会话
	sess.ReplaceWithSummary(summary, recentMsgs)

	logger.Info("Context compaction completed", logger.Fields{
		"summary_length": len(summary),
		"mode":           mode,
	})
	if c.notifier != nil {
		c.notifier(sess, mode)
	}

	return nil
}

// generateSummary 使用 LLM 生成摘要
func (c *Compactor) generateSummary(ctx context.Context, sess *session.Session, msgs []models.Message, flushReport string) (string, error) {
	if c.summaryGenerator != nil {
		return c.summaryGenerator(ctx, sess, msgs, flushReport)
	}

	// 构建摘要请求
	prompt := c.buildSummaryPrompt(msgs, flushReport)

	// 调用 LLM
	response, err := c.llmProvider.Chat(ctx, []models.Message{
		{
			Role:    "system",
			Content: c.getSummarySystemPrompt(),
		},
		{
			Role:    "user",
			Content: prompt,
		},
	}, nil)
	if err != nil {
		return "", fmt.Errorf("LLM chat: %w", err)
	}

	return response.Content, nil
}

// getSummarySystemPrompt 获取摘要系统提示
func (c *Compactor) getSummarySystemPrompt() string {
	return `You are a conversation summarizer. Your task is to create a comprehensive yet concise summary of the conversation below.

## CRITICAL: What to Preserve (DO NOT omit)

1. **User's Work/Technical Discussions**: Any discussion about architecture decisions, business logic, requirements, design patterns, debugging, performance, security, or code changes. These are the most important things to preserve.

2. **Specific Technical Details**: File paths, function names, configuration changes, API endpoints, error codes, version numbers, and specific implementation details.

3. **Problems and Solutions**: What problems were identified, what root causes were discovered, and what fixes were applied (or rejected and why).

4. **Decisions and Trade-offs**: Any decisions made, why they were made, and alternative approaches that were considered.

5. **User Preferences and Requirements**: How the user wants things done, constraints they specified, and standards they follow.

6. **Pending Tasks**: Open questions, unresolved issues, and next steps.

7. **Key Facts and Context**: Real-world facts, data, and context information shared during the conversation.

## What to Compress or Omit

- Greeting exchanges and small talk (condense to a single line if needed)
- Repetitive discussions that reach the same conclusion
- Debug exploration paths that led nowhere (only keep the final answer)
- Generic explanations already covered by other content

## Format

- Use structured sections with headers
- Bullet points for facts and decisions
- Keep it concise but dense with information
- Preserve chronological relationships when relevant
- Write in a technical, information-rich style
- DO NOT create a "conversation log" - create a technical record`
}

// buildSummaryPrompt 构建摘要请求
func (c *Compactor) buildSummaryPrompt(msgs []models.Message, flushReport string) string {
	var conversation strings.Builder

	if strings.TrimSpace(flushReport) != "" {
		conversation.WriteString("Pre-compaction memory flush report (for awareness only; do not duplicate already-saved details unless needed for continuity):\n")
		conversation.WriteString(strings.TrimSpace(flushReport))
		conversation.WriteString("\n\n")
	}

	// 完整分析所有被压缩的消息，不限制只分析最近的
	// 早期消息中的关键信息同样可能被遗漏，必须纳入分析
	for i, m := range msgs {
		// 跳过工具消息（工具结果数据量大但信息密度低）
		if m.Role == "tool" {
			continue
		}

		content := m.Content

		// 格式化时间
		timestamp := ""
		if !m.Timestamp.IsZero() {
			timestamp = m.Timestamp.Format("15:04")
		}

		conversation.WriteString(fmt.Sprintf("[%d] [%s %s] %s\n", i, timestamp, strings.ToUpper(m.Role), content))
	}

	conversation.WriteString(fmt.Sprintf("\nPlease summarize the following conversation:\n"))
	conversation.WriteString("Provide a concise summary that captures key information, user preferences, decisions, and pending tasks.\n")

	return conversation.String()
}

// generateSimpleSummary 生成简单摘要（不使用 LLM）
func (c *Compactor) generateSimpleSummary(msgs []models.Message) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("## Conversation Summary (%d messages)", len(msgs)))
	parts = append(parts, fmt.Sprintf("Generated at: %s", time.Now().Format(time.RFC3339)))
	parts = append(parts, "", "> Note: This is a basic summary. LLM-based summary generation failed.", "")

	// 提取关键信息
	var userMessages []string
	var assistantMessages []string
	var toolResults []string

	for _, m := range msgs {
		if m.Role == "user" && len(m.Content) > 0 {
			userMessages = append(userMessages, m.Content)
		} else if m.Role == "assistant" && len(m.Content) > 0 {
			assistantMessages = append(assistantMessages, m.Content)
		} else if m.Role == "tool" && len(m.Content) > 0 {
			toolResults = append(toolResults, m.Content)
		}
	}

	if len(userMessages) > 0 {
		parts = append(parts, "\n### User Messages")
		for i, msg := range userMessages {
			if i < 10 { // 最多显示 10 条
				if len(msg) > 500 {
					msg = msg[:500] + "..."
				}
				parts = append(parts, fmt.Sprintf("- %s", msg))
			}
		}
		if len(userMessages) > 10 {
			parts = append(parts, fmt.Sprintf("- ... and %d more messages", len(userMessages)-10))
		}
	}

	if len(toolResults) > 0 {
		parts = append(parts, "\n### Tool Executions")
		for i, r := range toolResults {
			if i < 10 { // 最多显示 10 条工具结果
				if len(r) > 300 {
					r = r[:300] + "..."
				}
				parts = append(parts, fmt.Sprintf("- %s", r))
			}
		}
		if len(toolResults) > 10 {
			parts = append(parts, fmt.Sprintf("- ... and %d more tool executions", len(toolResults)-10))
		}
	}

	if len(assistantMessages) > 0 {
		parts = append(parts, "\n### Assistant Responses")
		for i, msg := range assistantMessages {
			if i < 5 { // 最多显示 5 条
				if len(msg) > 500 {
					msg = msg[:500] + "..."
				}
				parts = append(parts, fmt.Sprintf("- %s", msg))
			}
		}
		if len(assistantMessages) > 5 {
			parts = append(parts, fmt.Sprintf("- ... and %d more responses", len(assistantMessages)-5))
		}
	}

	return strings.Join(parts, "\n")
}

// EstimateTokens 估算消息的 Token 数（使用混合字符比例）
func (c *Compactor) EstimateTokens(msgs []models.Message) int {
	return estimateMessagesTokens(msgs)
}

// CompactIfNeeded 检查并在需要时压缩
func (c *Compactor) CompactIfNeeded(ctx context.Context, sess *session.Session) error {
	need := c.compactionNeed(sess)
	if need.Needed {
		logger.Info("Context compaction threshold exceeded", logger.Fields{
			"reason":           need.Reason,
			"message_count":    need.MessageCount,
			"max_messages":     need.MaxMessages,
			"estimated_tokens": need.EstimatedTokens,
			"max_tokens":       need.MaxTokens,
		})
		return c.Compact(ctx, sess)
	}
	return nil
}
