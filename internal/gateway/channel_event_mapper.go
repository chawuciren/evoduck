package gateway

import (
	"fmt"
	"strings"

	"github.com/chawuciren/evoduck/pkg/models"
)

func buildChannelEvent(event models.StreamEvent, aggregatedContent, streamID string) *models.ChannelEvent {
	switch event.Type {
	case "thinking":
		if strings.TrimSpace(event.ThinkingContent) == "" {
			return nil
		}
		return &models.ChannelEvent{Type: models.ChannelEventThinking, Content: event.ThinkingContent, StreamID: streamID}
	case models.StreamEventPlan, models.StreamEventPlanUpdate:
		if event.Plan == nil {
			return nil
		}
		return &models.ChannelEvent{Type: event.Type, Plan: event.Plan, StreamID: streamID}
	case "tool_start":
		toolName := strings.TrimSpace(event.ToolName)
		if toolName == "" {
			return nil
		}
		return &models.ChannelEvent{Type: models.ChannelEventToolStart, ToolName: toolName, ToolParams: event.ToolParams, StreamID: streamID}
	case models.StreamEventToolEnd:
		toolName := strings.TrimSpace(event.ToolName)
		if toolName == "" {
			return nil
		}
		return &models.ChannelEvent{Type: models.ChannelEventToolEnd, ToolName: toolName, StreamID: streamID}
	case models.StreamEventContent:
		if event.Content == "" {
			return nil
		}
		return &models.ChannelEvent{Type: models.ChannelEventContentChunk, Content: event.Content, StreamID: streamID}
	case models.StreamEventDone, "stop":
		finalContent := strings.TrimSpace(aggregatedContent)
		if finalContent == "" {
			return nil
		}
		return &models.ChannelEvent{Type: models.ChannelEventFinal, Content: finalContent, StreamID: streamID, Done: true}
	case models.StreamEventError:
		if event.Error == nil {
			return nil
		}
		errMsg := fmt.Sprintf("Sorry, an error occurred while processing your request: %s", event.Error.Error())
		return &models.ChannelEvent{Type: models.ChannelEventError, Content: errMsg, ErrorText: event.Error.Error(), StreamID: streamID, Done: true}
	case models.StreamEventCancelled:
		content := strings.TrimSpace(event.Content)
		if content == "" {
			content = "Request cancelled."
		}
		return &models.ChannelEvent{Type: models.ChannelEventCancelled, Content: content, StreamID: streamID, Done: true}
	default:
		return nil
	}
}
