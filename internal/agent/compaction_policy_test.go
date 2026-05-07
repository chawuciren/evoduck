package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/internal/skill"
	"github.com/chawuciren/evoduck/internal/tools"
	"github.com/chawuciren/evoduck/pkg/models"
)

type compactionPolicyProvider struct{}

type compactionFlushReportProvider struct {
	calls [][]models.Message
}

type compactionNoopProvider struct{}

func (p *compactionPolicyProvider) Name() string { return "stub" }

func (p *compactionPolicyProvider) Chat(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (*models.Response, error) {
	return &models.Response{
		Content: "compressed important context",
		ToolCalls: []models.ToolCall{{
			ID:   "flush_1",
			Type: "function",
			Function: models.ToolCallFunction{
				Name:      "file_edit",
				Arguments: `{"path":"MEMORY.md","operation":"append","content":"should not be written"}`,
			},
		}},
	}, nil
}

func (p *compactionPolicyProvider) ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, _ llm.ChatOptions) (*models.Response, error) {
	return p.Chat(ctx, messages, tools)
}

func (p *compactionPolicyProvider) ChatStream(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	ch := make(chan models.StreamEvent)
	close(ch)
	return ch, nil
}

func (p *compactionPolicyProvider) SetDefaultOptions(_ llm.ChatOptions) {}
func (p *compactionPolicyProvider) GetMaxContextTokens() int            { return 8192 }
func (p *compactionPolicyProvider) BuiltinModels() []llm.ProviderModel  { return nil }
func (p *compactionPolicyProvider) FetchModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}
func (p *compactionPolicyProvider) ListModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}

func (p *compactionFlushReportProvider) Name() string { return "stub" }

func (p *compactionFlushReportProvider) Chat(_ context.Context, messages []models.Message, tools []models.ToolDefinition) (*models.Response, error) {
	p.calls = append(p.calls, append([]models.Message(nil), messages...))
	return &models.Response{Content: "summary saw flush report"}, nil
}

func (p *compactionFlushReportProvider) ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, _ llm.ChatOptions) (*models.Response, error) {
	return p.Chat(ctx, messages, tools)
}

func (p *compactionFlushReportProvider) ChatStream(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	ch := make(chan models.StreamEvent)
	close(ch)
	return ch, nil
}

func (p *compactionFlushReportProvider) SetDefaultOptions(_ llm.ChatOptions) {}
func (p *compactionFlushReportProvider) GetMaxContextTokens() int            { return 8192 }
func (p *compactionFlushReportProvider) BuiltinModels() []llm.ProviderModel  { return nil }
func (p *compactionFlushReportProvider) FetchModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}
func (p *compactionFlushReportProvider) ListModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}

func (p *compactionNoopProvider) Name() string { return "stub" }

func (p *compactionNoopProvider) Chat(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (*models.Response, error) {
	return &models.Response{Content: "compressed important context"}, nil
}

func (p *compactionNoopProvider) ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, _ llm.ChatOptions) (*models.Response, error) {
	return p.Chat(ctx, messages, tools)
}

func (p *compactionNoopProvider) ChatStream(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	ch := make(chan models.StreamEvent)
	close(ch)
	return ch, nil
}

func (p *compactionNoopProvider) SetDefaultOptions(_ llm.ChatOptions) {}
func (p *compactionNoopProvider) GetMaxContextTokens() int            { return 8192 }
func (p *compactionNoopProvider) BuiltinModels() []llm.ProviderModel  { return nil }
func (p *compactionNoopProvider) FetchModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}
func (p *compactionNoopProvider) ListModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}

func TestCompactionSkipsFlushForIgnoredMemoryPolicyButKeepsSummary(t *testing.T) {
	sess := session.NewSession("agent:experience-curator:schedule:system:memory-curation", "session-1", nil)
	sess.SetMetadataValue("session_kind", "schedule")
	sess.SetMetadataValue("memory_policy", "ignore")
	for i := 0; i < 5; i++ {
		sess.Append(models.Message{Role: "user", Content: "old message"})
	}

	compactor := NewCompactor(&compactionPolicyProvider{}, CompactorConfig{
		MaxMessages:        2,
		KeepRecent:         1,
		FlushBeforeCompact: true,
		Workspace:          t.TempDir(),
	})

	if err := compactor.Compact(context.Background(), sess); err != nil {
		t.Fatalf("compact: %v", err)
	}
	msgs := sess.GetMessages()
	if len(msgs) != 2 {
		t.Fatalf("expected summary plus recent message, got %d messages", len(msgs))
	}
	if msgs[0].Role != "system" || !strings.HasPrefix(msgs[0].Content, "Previous conversation summary: ") {
		t.Fatalf("expected first message to be summary, got %+v", msgs[0])
	}
	if !strings.Contains(msgs[0].Content, "compressed important context") {
		t.Fatalf("expected generated summary to be preserved, got %q", msgs[0].Content)
	}
}

func TestCompactionCallsPreCompactCuratorCallback(t *testing.T) {
	sess := session.NewSession("agent:test", "session-1", nil)
	for i := 0; i < 5; i++ {
		sess.Append(models.Message{Role: "user", Content: "important preference"})
	}

	provider := &compactionFlushReportProvider{}
	compactor := NewCompactor(provider, CompactorConfig{
		MaxMessages:        2,
		KeepRecent:         1,
		FlushBeforeCompact: true,
		Workspace:          t.TempDir(),
	})
	called := false
	compactor.SetPreCompactCurator(func(_ context.Context, source *session.Session, msgs []models.Message) (string, error) {
		called = true
		if source != sess {
			t.Fatalf("expected source session passed to curator")
		}
		if len(msgs) == 0 {
			t.Fatalf("expected old messages passed to curator")
		}
		return "curator saved MEMORY.md", nil
	})

	if err := compactor.Compact(context.Background(), sess); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if !called {
		t.Fatalf("expected pre-compact curator callback to be called")
	}
	if len(provider.calls) == 0 || !strings.Contains(provider.calls[0][1].Content, "curator saved MEMORY.md") {
		t.Fatalf("expected curator report in summary prompt")
	}
}

func TestCompactionInjectsFlushReportIntoSummaryPrompt(t *testing.T) {
	sess := session.NewSession("agent:test:user:alice", "session-1", nil)
	for i := 0; i < 5; i++ {
		sess.Append(models.Message{Role: "user", Content: "remember my durable preference"})
	}

	provider := &compactionFlushReportProvider{}
	compactor := NewCompactor(provider, CompactorConfig{
		MaxMessages:        2,
		KeepRecent:         1,
		FlushBeforeCompact: true,
		Workspace:          t.TempDir(),
	})
	compactor.SetPreCompactCurator(func(context.Context, *session.Session, []models.Message) (string, error) {
		return "Model report:\n{\n  \"saved\": [\"MEMORY.md\"],\n  \"notes\": \"captured durable preference\"\n}\n\nWrote via file_edit to MEMORY.md: ok", nil
	})

	if err := compactor.Compact(context.Background(), sess); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if len(provider.calls) < 1 {
		t.Fatalf("expected summary call, got %d", len(provider.calls))
	}
	summaryPrompt := provider.calls[0][1].Content
	if !strings.Contains(summaryPrompt, "Pre-compaction memory flush report") {
		t.Fatalf("expected flush report in summary prompt:\n%s", summaryPrompt)
	}
	if !strings.Contains(summaryPrompt, "MEMORY.md") || !strings.Contains(summaryPrompt, "captured durable preference") {
		t.Fatalf("expected parsed JSON report details in summary prompt:\n%s", summaryPrompt)
	}
	if !strings.Contains(summaryPrompt, "Wrote via file_edit to MEMORY.md") {
		t.Fatalf("expected tool execution result in summary prompt:\n%s", summaryPrompt)
	}
}

func TestBuildSummaryPromptDoesNotTruncateMessageContent(t *testing.T) {
	compactor := NewCompactor(&compactionNoopProvider{}, CompactorConfig{})
	longContent := strings.Repeat("x", 1200)

	prompt := compactor.buildSummaryPrompt([]models.Message{{Role: "user", Content: longContent}}, "")
	if !strings.Contains(prompt, longContent) {
		t.Fatalf("expected full message content in summary prompt")
	}
	if strings.Contains(prompt, "...") {
		t.Fatalf("expected no truncation marker in summary prompt: %q", prompt)
	}
}

func TestEphemeralSessionSkipsRuntimeCompaction(t *testing.T) {
	sess := session.NewSession("", "ephemeral-test", nil)
	sess.SetMetadataValue("ephemeral", "true")
	for i := 0; i < 3; i++ {
		sess.Append(models.Message{Role: "user", Content: "message"})
	}
	runtime := &Runtime{
		agentID:       "agent-test",
		llmProvider:   &compactionNoopProvider{},
		toolRegistry:  tools.NewRegistry(),
		promptBuilder: NewPromptBuilder(t.TempDir(), "agent-test", t.TempDir(), tools.NewRegistry(), skill.NewLoader(t.TempDir(), t.TempDir())),
		compactor: NewCompactor(&compactionNoopProvider{}, CompactorConfig{
			MaxMessages: 1,
			KeepRecent:  1,
		}),
	}
	if err := runtime.Run(context.Background(), sess, "new message"); err != nil {
		t.Fatalf("runtime run: %v", err)
	}
	if got := sess.GetMessages()[0].Role; got == "system" {
		t.Fatalf("ephemeral session should not be compacted")
	}
}

func TestReplayProtectedStartKeepsToolWithParentAssistant(t *testing.T) {
	// Scenario: assistant(tool_calls) is just before the split point,
	// its tool responses are in the "keep recent" region.
	// The split point should move back to include the parent assistant.
	msgs := []models.Message{
		{Role: "user", Content: "old user msg 1"},
		{Role: "user", Content: "old user msg 2"},
		{Role: "user", Content: "old user msg 3"},
		{Role: "user", Content: "old user msg 4"},
		{Role: "assistant", ToolCalls: []models.ToolCall{{ID: "tc_1", Type: "function", Function: models.ToolCallFunction{Name: "read_file", Arguments: `{"path":"test.txt"}`}}}},
		{Role: "tool", ToolCallID: "tc_1", Content: "file content"},
		{Role: "user", Content: "recent user msg"},
	}

	// If keepRecent=2, split would be at index 5 (between assistant and tool)
	// Without fix: oldMsgs=[0..4], recentMsgs=[5..6] -> orphaned tool msg
	// With fix: split should move back to index 4 to include the assistant
	start := len(msgs) - 2 // = 5
	got := replayProtectedStart(msgs, start)
	if got != 4 {
		t.Fatalf("expected start=4 (include parent assistant), got %d", got)
	}
}

func TestReplayProtectedStartMultipleToolCalls(t *testing.T) {
	// Scenario: assistant with multiple tool calls, some tool responses in recent region
	msgs := []models.Message{
		{Role: "user", Content: "old user msg"},
		{Role: "assistant", ToolCalls: []models.ToolCall{
			{ID: "tc_1", Type: "function", Function: models.ToolCallFunction{Name: "read_file", Arguments: `{}`}},
			{ID: "tc_2", Type: "function", Function: models.ToolCallFunction{Name: "search", Arguments: `{}`}},
		}},
		{Role: "tool", ToolCallID: "tc_1", Content: "result 1"},
		{Role: "tool", ToolCallID: "tc_2", Content: "result 2"},
		{Role: "user", Content: "follow up"},
	}

	// keepRecent=2 -> split at index 3 (first tool msg)
	// Should move back to index 1 (assistant)
	start := len(msgs) - 2 // = 3
	got := replayProtectedStart(msgs, start)
	if got != 1 {
		t.Fatalf("expected start=1 (include parent assistant with all tool calls), got %d", got)
	}
}

func TestReplayProtectedStartKeepsAssistantWithAllFollowingToolResults(t *testing.T) {
	msgs := []models.Message{
		{Role: "user", Content: "old user msg 1"},
		{Role: "user", Content: "old user msg 2"},
		{Role: "assistant", ToolCalls: []models.ToolCall{
			{ID: "tc_1", Type: "function", Function: models.ToolCallFunction{Name: "read_file", Arguments: `{}`}},
			{ID: "tc_2", Type: "function", Function: models.ToolCallFunction{Name: "search", Arguments: `{}`}},
		}},
		{Role: "tool", ToolCallID: "tc_1", Content: "result 1"},
		{Role: "tool", ToolCallID: "tc_2", Content: "result 2"},
		{Role: "user", Content: "recent follow up"},
	}

	// keepRecent=3 would split at the first tool result. The protected start must
	// move back to the assistant so the full assistant/tool block remains intact.
	start := len(msgs) - 3 // = 3
	got := replayProtectedStart(msgs, start)
	if got != 2 {
		t.Fatalf("expected start=2 (include assistant and all tool results), got %d", got)
	}
}

func TestReplayProtectedStartKeepsChainedToolBlocksTogether(t *testing.T) {
	msgs := []models.Message{
		{Role: "user", Content: "old user msg"},
		{Role: "assistant", ToolCalls: []models.ToolCall{{ID: "tc_1", Type: "function", Function: models.ToolCallFunction{Name: "read_file", Arguments: `{}`}}}},
		{Role: "tool", ToolCallID: "tc_1", Content: "result 1"},
		{Role: "assistant", ToolCalls: []models.ToolCall{{ID: "tc_2", Type: "function", Function: models.ToolCallFunction{Name: "search", Arguments: `{}`}}}},
		{Role: "tool", ToolCallID: "tc_2", Content: "result 2"},
		{Role: "user", Content: "recent follow up"},
	}

	// keepRecent=2 starts at the second tool result. The second assistant is pulled
	// in first, then the preceding tool/protected assistant chain is pulled in too.
	start := len(msgs) - 2 // = 4
	got := replayProtectedStart(msgs, start)
	if got != 1 {
		t.Fatalf("expected start=1 (include chained tool blocks), got %d", got)
	}
}

func TestReplayProtectedStartNoOrphanedTools(t *testing.T) {
	// Scenario: tool message in recent region but its parent assistant is in old region
	// This is the core bug: tool messages with ToolCallID should never be orphaned
	msgs := []models.Message{
		{Role: "user", Content: "old 1"},
		{Role: "user", Content: "old 2"},
		{Role: "user", Content: "old 3"},
		{Role: "assistant", ToolCalls: []models.ToolCall{{ID: "call_abc", Type: "function", Function: models.ToolCallFunction{Name: "bash", Arguments: `{"command":"ls"}`}}}},
		{Role: "tool", ToolCallID: "call_abc", Content: "file1\nfile2"},
		{Role: "assistant", Content: "Here are the files"},
		{Role: "user", Content: "thanks"},
	}

	// keepRecent=2 -> split at index 5 (assistant "Here are the files")
	// The tool msg at index 4 references assistant at index 3
	// Since tool msg is NOT in recent region (index 4 < 5), no adjustment needed
	start := len(msgs) - 2 // = 5
	got := replayProtectedStart(msgs, start)
	// The assistant at index 5 has no tool calls, no reasoning, no thinking -> not protected
	// Should stay at 5
	if got != 5 {
		t.Fatalf("expected start=5, got %d", got)
	}

	// Now test where tool IS in recent region
	// keepRecent=3 -> split at index 4 (tool msg)
	start = len(msgs) - 3 // = 4
	got = replayProtectedStart(msgs, start)
	// Tool at index 4 references assistant at index 3 -> should move back to 3
	if got != 3 {
		t.Fatalf("expected start=3 (include parent assistant), got %d", got)
	}
}

func TestCompactionKeepsAssistantToolPairsTogether(t *testing.T) {
	// End-to-end test: compaction should not orphan tool messages
	sess := session.NewSession("agent:test", "session-1", nil)

	// Build messages where assistant(tool_calls) is near the boundary
	sess.Append(models.Message{Role: "user", Content: "old message 1"})
	sess.Append(models.Message{Role: "user", Content: "old message 2"})
	sess.Append(models.Message{Role: "user", Content: "old message 3"})
	sess.Append(models.Message{Role: "assistant", ToolCalls: []models.ToolCall{
		{ID: "tc_end", Type: "function", Function: models.ToolCallFunction{Name: "read_file", Arguments: `{"path":"important.txt"}`}},
	}})
	sess.Append(models.Message{Role: "tool", ToolCallID: "tc_end", Content: "critical file content"})
	sess.Append(models.Message{Role: "user", Content: "recent message"})

	compactor := NewCompactor(&compactionNoopProvider{}, CompactorConfig{
		MaxMessages: 3,
		KeepRecent:  2,
	})

	if err := compactor.Compact(context.Background(), sess); err != nil {
		t.Fatalf("compact: %v", err)
	}

	msgs := sess.GetMessages()
	// Should have: summary + assistant(tc_end) + tool(tc_end) + user(recent)
	// At minimum: assistant and tool with tc_end must both exist (not orphaned)
	hasAssistantWithToolCalls := false
	hasOrphanedTool := false
	for _, m := range msgs {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			hasAssistantWithToolCalls = true
		}
		if m.Role == "tool" && m.ToolCallID == "tc_end" {
			if !hasAssistantWithToolCalls {
				// Check if any assistant in msgs has this tool call ID
				found := false
				for _, m2 := range msgs {
					if m2.Role == "assistant" {
						for _, tc := range m2.ToolCalls {
							if tc.ID == "tc_end" {
								found = true
								break
							}
						}
					}
					if found {
						break
					}
				}
				if !found {
					hasOrphanedTool = true
				}
			}
		}
	}

	if hasOrphanedTool {
		t.Fatalf("tool message with tc_end is orphaned - its parent assistant was compacted away")
	}
}
