package agent

import "github.com/chawuciren/evoduck/pkg/models"

func effectiveKeepRecentForReplay(msgs []models.Message, keepRecent int) int {
	if keepRecent <= 0 {
		keepRecent = 1
	}
	if len(msgs) <= keepRecent {
		return len(msgs)
	}

	start := len(msgs) - keepRecent
	protectedStart := replayProtectedStart(msgs, start)
	if protectedStart < start {
		return len(msgs) - protectedStart
	}
	return keepRecent
}

func replayProtectedStart(msgs []models.Message, start int) int {
	if start <= 0 || start >= len(msgs) {
		return start
	}

	for {
		protectedStart := start

		// Pass 1: For each tool message in recent region, find its parent assistant.
		// If the parent is before start, pull start back to include the parent.
		for i := protectedStart; i < len(msgs); i++ {
			if msgs[i].Role != "tool" || msgs[i].ToolCallID == "" {
				continue
			}
			if parent := findParentAssistantToolCall(msgs, i, msgs[i].ToolCallID); parent >= 0 && parent < protectedStart {
				protectedStart = parent
			}
		}

		// Pass 2: For each assistant(tool_calls) in recent region, ensure all of its
		// tool results are also in recent region by pulling start back to the parent
		// assistant when any result would otherwise be compacted away.
		for i := protectedStart; i < len(msgs); i++ {
			if msgs[i].Role != "assistant" || len(msgs[i].ToolCalls) == 0 {
				continue
			}
			if !assistantToolResultsContainedInRecent(msgs, i, protectedStart) && i < protectedStart {
				protectedStart = i
			}
			if !assistantToolResultsContainedInRecent(msgs, i, protectedStart) {
				protectedStart = i
			}
		}

		// Pass 3: Walk backwards from any protected assistant in the recent region
		// to include preceding tool messages and protected assistants.
		for i := protectedStart; i < len(msgs); i++ {
			if !messageNeedsReplayProtection(msgs[i]) {
				continue
			}
			for i > 0 {
				prev := msgs[i-1]
				if prev.Role == "tool" || messageNeedsReplayProtection(prev) {
					i--
					continue
				}
				break
			}
			protectedStart = i
			break
		}

		if protectedStart == start {
			break
		}
		start = protectedStart
	}
	return start
}

func findParentAssistantToolCall(msgs []models.Message, before int, toolCallID string) int {
	for j := before - 1; j >= 0; j-- {
		if msgs[j].Role != "assistant" {
			continue
		}
		for _, tc := range msgs[j].ToolCalls {
			if tc.ID == toolCallID {
				return j
			}
		}
	}
	return -1
}

func assistantToolResultsContainedInRecent(msgs []models.Message, assistantIndex, start int) bool {
	for _, tc := range msgs[assistantIndex].ToolCalls {
		if tc.ID == "" {
			continue
		}
		found := false
		for i := assistantIndex + 1; i < len(msgs); i++ {
			if msgs[i].Role == "assistant" && len(msgs[i].ToolCalls) > 0 {
				break
			}
			if msgs[i].Role == "tool" && msgs[i].ToolCallID == tc.ID {
				found = i >= start
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func messageNeedsReplayProtection(msg models.Message) bool {
	if msg.Role != "assistant" {
		return false
	}
	if len(msg.ToolCalls) > 0 {
		return true
	}
	if msg.ReasoningMetadata != nil && msg.ReasoningMetadata.HasData() {
		return true
	}
	return msg.ThinkingContent != ""
}
