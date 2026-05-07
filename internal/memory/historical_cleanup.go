package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
)

const historicalCleanupMaxChars = 20000

var cleanupLog = logger.NewModuleLogger("memory-cleanup")

type Cleaner struct {
	llm llm.Provider
}

func NewCleaner(provider llm.Provider) *Cleaner {
	return &Cleaner{llm: provider}
}

func floatPtr(f float64) *float64 {
	return &f
}

func (c *Cleaner) CleanupMediumFile(ctx context.Context, path string) (string, bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	cleaned, changed, err := c.cleanupContent(ctx, "medium", string(content), filepath.Base(path))
	if err != nil {
		return "", false, err
	}
	return cleaned, changed, nil
}

func (c *Cleaner) CleanupLongterm(ctx context.Context, content, scope string) (string, bool, error) {
	return c.cleanupContent(ctx, "longterm", content, scope)
}

func (c *Cleaner) cleanupContent(ctx context.Context, memType, content, scope string) (string, bool, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return content, false, nil
	}
	if c.llm == nil {
		return content, false, nil
	}

	promptContent := trimmed
	if len(promptContent) > historicalCleanupMaxChars {
		promptContent = promptContent[len(promptContent)-historicalCleanupMaxChars:]
	}

	resp, err := c.llm.ChatWithOptions(ctx, []models.Message{{
		Role:    "system",
		Content: getHistoricalCleanupSystemPrompt(memType),
	}, {
		Role:    "user",
		Content: getHistoricalCleanupUserPrompt(memType, scope, promptContent),
	}}, nil, llm.ChatOptions{Temperature: floatPtr(0.1), MaxTokens: 4000})
	if err != nil {
		cleanupLog.Warn("Historical cleanup LLM failed, keep original", logger.Fields{"type": memType, "scope": scope, "error": err.Error()})
		return content, false, nil
	}

	cleaned := strings.TrimSpace(resp.Content)
	if cleaned == "" {
		return content, false, nil
	}
	if cleaned == trimmed {
		return content, false, nil
	}

	return cleaned + "\n", true, nil
}

func getHistoricalCleanupSystemPrompt(memType string) string {
	return fmt.Sprintf(`You are a historical %s memory cleanup assistant.

Your job is to normalize existing stored memory content, not to summarize away important information.

Goals:
- Merge duplicate or heavily overlapping entries
- Remove repeated report-style wording
- Keep canonical, high-value memory items
- Preserve important technical details exactly
- Keep the original layer semantics intact

Rules:
- Do not invent new facts
- Do not remove durable user preferences, decisions, constraints, or key lessons unless they are clearly duplicated
- Preserve markdown structure
- For medium memory, keep the content useful for recent-session recall
- For longterm memory, keep the content stable, compact, and durable
- Output only the final cleaned markdown content`, memType)
}

func getHistoricalCleanupUserPrompt(memType, scope, content string) string {
	return fmt.Sprintf(`Memory type: %s
Scope: %s

Clean and normalize this stored memory content. Keep it concise, non-redundant, and still directly loadable by the system.

Stored content:
%s

Final cleaned content:`, memType, scope, content)
}
