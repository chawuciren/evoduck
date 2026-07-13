package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/pkg/models"
)

// collectChatStream 调用 ChatStream 并把事件聚合成 *models.Response。
// 供原先用非流式 Chat 的压缩兜底路径（Compactor.generateSummary 在未注入 summaryGenerator 时）使用：
// 流式靠 SSE 首字节保活，可绕过上游 router 对非流式请求施加的 TTFB/读取超时。
// 注意：Bedrock 的 ChatStream 内部仍先调 Chat 再发事件（非真流式），Bedrock 用户可能仍受上游超时影响。
func collectChatStream(ctx context.Context, provider llm.Provider, messages []models.Message, toolDefs []models.ToolDefinition) (*models.Response, error) {
	streamCh, err := provider.ChatStream(ctx, messages, toolDefs)
	if err != nil {
		return nil, fmt.Errorf("LLM stream: %w", err)
	}

	var content strings.Builder
	var reasoning strings.Builder
	var reasoningMetadata *models.ReasoningReplay
	var toolCalls []models.ToolCall
	for event := range streamCh {
		switch event.Type {
		case "content":
			content.WriteString(event.Content)
		case "thinking":
			reasoning.WriteString(event.ThinkingContent)
			reasoningMetadata = models.MergeReasoningReplay(reasoningMetadata, event.ReasoningMetadata)
		case "tool_calls":
			toolCalls = append(toolCalls, event.ToolCalls...)
		case "error":
			if event.Error != nil {
				return nil, event.Error
			}
		case "cancelled":
			return nil, ctx.Err()
		}
	}

	return &models.Response{
		Content:           content.String(),
		ReasoningContent:  reasoning.String(),
		ReasoningMetadata: reasoningMetadata,
		ToolCalls:         toolCalls,
	}, nil
}
