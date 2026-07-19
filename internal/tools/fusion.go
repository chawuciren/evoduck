package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
)

const panelSystemPrompt = `你是一位被邀请参与圆桌会诊的资深技术专家。你将看到当前会话的完整上下文记录和一个需要会诊的问题。

请基于上下文，针对问题给出你的专业分析：
1. 分析问题的核心难点和关键约束
2. 给出你的解决方案或建议（可以有多个选项），并明确你推荐哪一个、为什么
3. 主动指出该方案/结论的薄弱环节、潜在失败场景、被忽略的约束，以及是否存在更优替代方案
4. 说明你的理由和潜在风险

要点：不要只做顺从的附和者。如果会诊对象是一个待验证的结论或方案，请以审稿人/红队的视角去挑战它——只有经得起挑战的结论才值得交付。直接输出你的完整分析。`

const judgeSystemPrompt = `你是圆桌会诊的主持人兼裁判。你将获得：当前会话的完整上下文、会诊问题，以及多位专家的独立分析。

你的职责是基于上下文进行**事实校验、冲突调和与裁决**，而非从头重新分析问题：

1. **切题性校验**：检查每位专家是否紧扣会诊问题作答，还是被上下文中的无关内容带偏、跑题。
2. **事实性校验**：对照上下文，检查专家引用的文件名、函数签名、代码片段、配置项等是否准确，指出任何幻觉或事实错误。
3. **冲突调和**：当专家意见分歧时，明确标出分歧点，结合上下文事实判定哪一方更可信、为什么；不要和稀泥，必要时给出明确取舍。
4. **裁决**：综合上述校验，给出最终结论与可执行的下一步（明确动作或明确排除项），并标注结论的置信度（高/中/低）和剩余风险。

重要约束：
- 你是裁判，不是第四个专家。禁止无视专家意见自己从头分析一遍问题。
- 如果专家意见与上下文事实冲突，以上下文事实为准。
- 如果所有专家都跑题或存在严重事实错误，请明确指出并给出你认为正确的方向。
- 最终结论必须**明确、可执行**，不要以"需要进一步分析"草草收尾；如果确实信息不足，请明确列出还需要补查哪些信息。

你的裁决将作为最终会诊结论，请果断、明确。`

// FusionTool 多模型圆桌会诊工具
type FusionTool struct {
	llmReg    *llm.Registry
	cfg       config.FusionConfig
	sessionGW SessionGatewayProvider
}

// NewFusionTool 创建 Fusion 圆桌会诊工具
func NewFusionTool(llmReg *llm.Registry, cfg config.FusionConfig, sessionGW SessionGatewayProvider) *FusionTool {
	return &FusionTool{
		llmReg:    llmReg,
		cfg:       cfg,
		sessionGW: sessionGW,
	}
}

func (t *FusionTool) Name() string { return "fusion" }

// IsTimeoutExempt 自管超时，豁免 Registry 全局兜底
func (t *FusionTool) IsTimeoutExempt() bool { return true }

func (t *FusionTool) Description() string {
	return `Convene a multi-model roundtable consultation (Fusion) to fact-check and stress-test your analysis or plan before you act on it.

Distributes the current full session context and a specific question to multiple pre-configured expert models in parallel. Each expert provides independent analysis (no tool execution, single round). A judge model then synthesizes a final, fact-checked verdict from the expert opinions.

## When you MUST call Fusion (mandatory checkpoints — treat as required, not optional)
Fusion is not just an escalation tool for stuck cases. It is a standard checkpoint for any non-trivial conclusion. You MUST call it at least once in each of these situations:

1. **Before drawing any research/investigation conclusion.**
   When you finish gathering information (reading files, searching code, fetching docs, exploring) and are about to summarize findings or hand back an answer to the user, run Fusion on the conclusion first. Do NOT report a research conclusion to the user without at least one Fusion pass to validate it.

2. **Between research and execution in a multi-step task.**
   When a task has a clear "investigation/analysis" phase followed by a "do the work" phase (e.g. investigate root cause → fix code; research options → implement; survey codebase → refactor), you MUST run at least one Fusion at the boundary — after research is done, before the first irreversible execution step (edit, write, exec, deploy, file edit). The Fusion result should shape what you actually do.

3. **Before any high-stakes or hard-to-reverse action.**
   Before edits, deletions, deploys, large refactors, schema changes, irreversible commands, or anything where being wrong costs significant time/money/credibility — call Fusion to pressure-test the plan.

The general rule: **a conclusion that hasn't been through Fusion is a draft.** Surface the Fusion verdict (or a faithful summary) to the user before committing to it.

## When NOT to use Fusion
- Simple tasks you can answer directly: single-file lookups, obvious one-liners, factual recall already in context.
- Pure routine execution with no analysis or conclusion to validate (e.g. "list files in X", "what time is it").
- Trivial conversational replies.
- Do not call it just to "be safe" when there is genuinely no conclusion or plan to validate — that wastes a round.

## Multi-round consultation
Fusion is not limited to a single call:
- If the judge says information is insufficient, it will name exactly what to investigate next.
- After gathering that information (reading files, searching code), re-invoke Fusion with an updated query.
- Each call automatically includes the latest session context.

## Behavior
1. Session context (full conversation history) is **automatically attached** — do not duplicate conversation history in the query. Focus only on stating the question clearly.
2. Distributes to all configured panel members in parallel (single round per call, text only — no tool execution).
3. A judge model synthesizes the final verdict with fact-checking against the session context.
4. Returns: judge verdict + each expert's raw analysis (mode: both).

## How to phrase the query
State the conclusion/plan you want validated and ask a concrete question. Good forms:
- "I plan to do X to fix Y. Is this the right approach, and what am I missing?"
- "My investigation concludes Z. Is this correct, and are there gaps before I act on it?"
- "Here is my proposed plan [summary]. Stress-test it: what would fail, and is there a better option?"
Bad forms: vague "what should I do?" with no proposed direction; pasting the whole conversation (context is auto-attached).

## Parameters
- query: (required) The specific question or conclusion to consult on. State your current conclusion/plan AND what you want validated. Do not include conversation history — it is attached automatically.
- mode: (optional) Override result mode: "judge" (verdict only), "raw" (expert opinions only), "both" (verdict + opinions). Defaults to the configured mode (both).`
}

func (t *FusionTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "The conclusion, plan, or question to consult on. State your current conclusion/plan AND what you want validated (e.g. 'I plan X to fix Y; is this right and what am I missing?'). Session history is attached automatically — do not paste it here.",
			},
			"mode": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"judge", "raw", "both"},
				"description": "Optional. Override result mode: judge=verdict only, raw=expert opinions only, both=verdict+opinions. Defaults to configured mode.",
			},
		},
		"required": []string{"query"},
	}
}

func (t *FusionTool) Execute(args map[string]interface{}) (string, error) {
	return "", fmt.Errorf("fusion requires user context")
}

func (t *FusionTool) ExecuteWithUserContext(ctx context.Context, args map[string]interface{}, role models.Role, userID string, userIsolationEnabled bool, workspace string) (string, error) {
	query, _ := args["query"].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("fusion requires a non-empty 'query' parameter")
	}

	mode := t.resolveMode(args)
	timeout := t.parseTimeout()

	logger.Info("Fusion roundtable started", logger.Fields{
		"query_preview": truncatePreview(query, 120),
		"mode":          mode,
		"panel_size":    len(t.cfg.Panel),
		"timeout":       timeout.String(),
	})

	// 构建完整上下文 transcript
	transcript := t.buildTranscript(ctx)
	panelMessages := buildPanelMessages(transcript, query)

	// Fan-out: 并发调用所有圆桌成员
	panelResults := t.fanOut(ctx, panelMessages, timeout)

	// 根据 mode 格式化返回
	switch mode {
	case "raw":
		result := formatRaw(query, panelResults)
		logger.Info("Fusion roundtable completed (raw mode)", logger.Fields{
			"success_count": countSuccess(panelResults),
			"total":         len(panelResults),
		})
		return result, nil

	case "judge":
		verdict, err := t.runJudge(ctx, query, transcript, panelResults, timeout)
		if err != nil {
			logger.Warn("Fusion judge failed, falling back to raw", logger.Fields{"error": err.Error()})
			return formatRaw(query, panelResults), nil
		}
		logger.Info("Fusion roundtable completed (judge mode)", logger.Fields{
			"success_count": countSuccess(panelResults),
			"total":         len(panelResults),
		})
		return verdict, nil

	default: // "both"
		verdict, err := t.runJudge(ctx, query, transcript, panelResults, timeout)
		if err != nil {
			logger.Warn("Fusion judge failed, returning raw only", logger.Fields{"error": err.Error()})
			return formatRaw(query, panelResults), nil
		}
		result := formatBoth(query, verdict, panelResults)
		logger.Info("Fusion roundtable completed (both mode)", logger.Fields{
			"success_count": countSuccess(panelResults),
			"total":         len(panelResults),
		})
		return result, nil
	}
}

// resolveMode 解析返回模式
func (t *FusionTool) resolveMode(args map[string]interface{}) string {
	if m, ok := args["mode"].(string); ok {
		m = strings.TrimSpace(strings.ToLower(m))
		switch m {
		case "judge", "raw", "both":
			return m
		}
	}
	m := strings.TrimSpace(strings.ToLower(t.cfg.Mode))
	if m == "judge" || m == "raw" || m == "both" {
		return m
	}
	return "both"
}

// parseTimeout 解析超时配置
func (t *FusionTool) parseTimeout() time.Duration {
	if t.cfg.Timeout != "" {
		if d, err := time.ParseDuration(t.cfg.Timeout); err == nil && d > 0 {
			return d
		}
	}
	return 300 * time.Second
}

// fanOut 并发调用所有圆桌成员
func (t *FusionTool) fanOut(ctx context.Context, messages []models.Message, timeout time.Duration) []panelResult {
	results := make([]panelResult, len(t.cfg.Panel))
	var wg sync.WaitGroup

	for i, member := range t.cfg.Panel {
		wg.Add(1)
		go func(idx int, m config.FusionMember) {
			defer wg.Done()
			results[idx] = t.callPanelMember(ctx, m, messages, timeout)
		}(i, member)
	}

	wg.Wait()
	return results
}

// callPanelMember 调用单个圆桌成员
func (t *FusionTool) callPanelMember(ctx context.Context, member config.FusionMember, messages []models.Message, timeout time.Duration) panelResult {
	start := time.Now()
	label := member.Label
	if label == "" {
		label = fmt.Sprintf("%s/%s", member.Provider, member.Model)
	}
	result := panelResult{
		Label:    label,
		Provider: member.Provider,
		Model:    member.Model,
	}

	resolvedProvider, resolvedModel, err := t.llmReg.ResolveProviderModel(member.Provider, member.Model)
	if err != nil {
		result.Error = fmt.Sprintf("resolve provider/model failed: %v", err)
		result.Duration = time.Since(start)
		logger.Warn("Fusion panel member resolve failed", logger.Fields{
			"member": label,
			"error":  result.Error,
		})
		return result
	}

	provider, err := t.llmReg.Get(resolvedProvider)
	if err != nil {
		result.Error = fmt.Sprintf("provider not found: %s", resolvedProvider)
		result.Duration = time.Since(start)
		return result
	}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	opts := llm.ChatOptions{Model: resolvedModel}
	content, err := t.streamChatCollect(callCtx, provider, messages, opts)
	result.Duration = time.Since(start)

	if err != nil {
		result.Error = err.Error()
		logger.Warn("Fusion panel member call failed", logger.Fields{
			"member":  label,
			"model":   resolvedModel,
			"error":   err.Error(),
			"elapsed": result.Duration.String(),
		})
		return result
	}

	result.Content = content
	logger.Debug("Fusion panel member completed", logger.Fields{
		"member":  label,
		"model":   resolvedModel,
		"elapsed": result.Duration.String(),
		"chars":   len(content),
	})
	return result
}

// runJudge 调用裁判模型综合裁决
func (t *FusionTool) runJudge(ctx context.Context, query string, transcript string, results []panelResult, timeout time.Duration) (string, error) {
	if t.cfg.Judge == nil {
		return "", fmt.Errorf("no judge model configured")
	}

	// 构建裁判输入：上下文 + 问题 + 各专家意见
	var sb strings.Builder
	if transcript != "" {
		sb.WriteString("[当前会话完整上下文]\n\n")
		sb.WriteString(transcript)
		sb.WriteString("\n\n---\n\n")
	}
	sb.WriteString("[会诊问题]\n")
	sb.WriteString(query)
	sb.WriteString("\n\n---\n\n[各专家意见]\n\n")
	for i, r := range results {
		if r.Error != "" {
			sb.WriteString(fmt.Sprintf("### 专家 %d: %s（调用失败）\n%s\n\n", i+1, r.Label, r.Error))
		} else {
			sb.WriteString(fmt.Sprintf("### 专家 %d: %s\n%s\n\n", i+1, r.Label, r.Content))
		}
	}

	messages := []models.Message{
		{Role: "system", Content: judgeSystemPrompt},
		{Role: "user", Content: sb.String()},
	}

	resolvedProvider, resolvedModel, err := t.llmReg.ResolveProviderModel(t.cfg.Judge.Provider, t.cfg.Judge.Model)
	if err != nil {
		return "", fmt.Errorf("judge resolve failed: %w", err)
	}

	provider, err := t.llmReg.Get(resolvedProvider)
	if err != nil {
		return "", fmt.Errorf("judge provider not found: %s", resolvedProvider)
	}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	judgeStart := time.Now()
	opts := llm.ChatOptions{Model: resolvedModel}
	content, err := t.streamChatCollect(callCtx, provider, messages, opts)
	if err != nil {
		return "", fmt.Errorf("judge call failed: %w", err)
	}

	logger.Info("Fusion judge completed", logger.Fields{
		"model":   resolvedModel,
		"elapsed": time.Since(judgeStart).String(),
		"chars":   len(content),
	})
	return content, nil
}

// streamWithOptionsProvider 是支持带 options 流式调用的 provider 扩展接口。
// openai-compatible 已实现 ChatStreamWithOptions，其他 provider 走 fallback。
type streamWithOptionsProvider interface {
	ChatStreamWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, opts llm.ChatOptions) (<-chan models.StreamEvent, error)
}

// streamChatCollect 使用流式调用收集完整响应文本。
// 流式靠 SSE 首字节保活，可绕过非流式 HTTP client 的 TTFB 超时（GLM 等慢首字节模型会被切断）。
// 优先用 ChatStreamWithOptions（不修改共享 defaultOptions，并发安全）；其他 provider 走 SetDefaultOptions fallback。
func (t *FusionTool) streamChatCollect(ctx context.Context, provider llm.Provider, messages []models.Message, opts llm.ChatOptions) (string, error) {
	if swop, ok := provider.(streamWithOptionsProvider); ok {
		streamCh, err := swop.ChatStreamWithOptions(ctx, messages, nil, opts)
		if err != nil {
			return "", fmt.Errorf("stream open: %w", err)
		}
		return collectStreamContent(ctx, streamCh)
	}
	// fallback：非并发安全，仅用于未实现 ChatStreamWithOptions 的 provider
	provider.SetDefaultOptions(opts)
	streamCh, err := provider.ChatStream(ctx, messages, nil)
	if err != nil {
		return "", fmt.Errorf("stream open: %w", err)
	}
	return collectStreamContent(ctx, streamCh)
}

// collectStreamContent 从流式 channel 收集 content 文本
func collectStreamContent(ctx context.Context, streamCh <-chan models.StreamEvent) (string, error) {
	var sb strings.Builder
	for event := range streamCh {
		switch event.Type {
		case "content":
			sb.WriteString(event.Content)
		case "error":
			if event.Error != nil {
				return "", event.Error
			}
		case "cancelled":
			return "", ctx.Err()
		}
	}
	return sb.String(), nil
}

// buildTranscript 从当前 session 提取完整上下文为可读文本
func (t *FusionTool) buildTranscript(ctx context.Context) string {
	if t.sessionGW == nil {
		return ""
	}
	sessionKey := SessionKeyFromContext(ctx)
	if sessionKey == "" {
		return ""
	}
	gw := t.sessionGW()
	if gw == nil {
		return ""
	}
	sess, err := gw.Get(sessionKey)
	if err != nil || sess == nil {
		return ""
	}
	messages := sess.GetMessages()
	if len(messages) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, msg := range messages {
		switch msg.Role {
		case "system":
			// 跳过 system prompt，由 panelSystemPrompt 替代
			continue
		case "user":
			sb.WriteString(fmt.Sprintf("[用户] %s\n\n", msg.Content))
		case "assistant":
			content := msg.Content
			if len(msg.ToolCalls) > 0 {
				var calls []string
				for _, tc := range msg.ToolCalls {
					calls = append(calls, fmt.Sprintf("%s(%s)", tc.Function.Name, truncatePreview(tc.Function.Arguments, 200)))
				}
				if content != "" {
					sb.WriteString(fmt.Sprintf("[助手] %s\n", content))
				}
				sb.WriteString(fmt.Sprintf("[工具调用] %s\n\n", strings.Join(calls, "; ")))
			} else if content != "" {
				sb.WriteString(fmt.Sprintf("[助手] %s\n\n", content))
			}
		case "tool":
			sb.WriteString(fmt.Sprintf("[工具结果] %s\n\n", msg.Content))
		}
	}
	return strings.TrimSpace(sb.String())
}

// buildPanelMessages 构建圆桌成员的消息列表
func buildPanelMessages(transcript, query string) []models.Message {
	var userContent strings.Builder
	if transcript != "" {
		userContent.WriteString("[当前会话完整上下文]\n\n")
		userContent.WriteString(transcript)
		userContent.WriteString("\n\n---\n\n")
	}
	userContent.WriteString("[会诊问题]\n")
	userContent.WriteString(query)

	return []models.Message{
		{Role: "system", Content: panelSystemPrompt},
		{Role: "user", Content: userContent.String()},
	}
}

// panelResult 单个圆桌成员的调用结果
type panelResult struct {
	Label    string
	Provider string
	Model    string
	Content  string
	Error    string
	Duration time.Duration
}

// formatRaw 格式化原始意见返回
func formatRaw(query string, results []panelResult) string {
	var sb strings.Builder
	sb.WriteString("# Fusion 圆桌会诊结果（专家原始意见）\n\n")
	sb.WriteString(fmt.Sprintf("**会诊问题**: %s\n\n", query))
	sb.WriteString("---\n\n")
	for i, r := range results {
		if r.Error != "" {
			sb.WriteString(fmt.Sprintf("## 专家 %d: %s ❌\n模型: %s/%s | 耗时: %s\n\n**错误**: %s\n\n",
				i+1, r.Label, r.Provider, r.Model, r.Duration.Round(time.Millisecond), r.Error))
		} else {
			sb.WriteString(fmt.Sprintf("## 专家 %d: %s\n模型: %s/%s | 耗时: %s\n\n%s\n\n",
				i+1, r.Label, r.Provider, r.Model, r.Duration.Round(time.Millisecond), r.Content))
		}
	}
	return sb.String()
}

// formatBoth 格式化裁决+原始意见返回
func formatBoth(query, verdict string, results []panelResult) string {
	var sb strings.Builder
	sb.WriteString("# Fusion 圆桌会诊结果\n\n")
	sb.WriteString(fmt.Sprintf("**会诊问题**: %s\n\n", query))
	sb.WriteString("---\n\n")
	sb.WriteString("## 🏛️ 裁决结论\n\n")
	sb.WriteString(verdict)
	sb.WriteString("\n\n---\n\n")
	sb.WriteString("## 📋 各专家原始意见\n\n")
	for _, r := range results {
		if r.Error != "" {
			sb.WriteString(fmt.Sprintf("### %s ❌ (%s/%s)\n%s\n\n", r.Label, r.Provider, r.Model, r.Error))
		} else {
			sb.WriteString(fmt.Sprintf("### %s (%s/%s)\n%s\n\n", r.Label, r.Provider, r.Model, r.Content))
		}
	}
	return sb.String()
}

// countSuccess 统计成功的成员数
func countSuccess(results []panelResult) int {
	n := 0
	for _, r := range results {
		if r.Error == "" {
			n++
		}
	}
	return n
}

// truncatePreview 截取预览文本
func truncatePreview(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}
