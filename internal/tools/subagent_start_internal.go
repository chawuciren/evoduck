package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chawuciren/evoduck/internal/subagent"
	"github.com/chawuciren/evoduck/pkg/models"
)

type SubagentStartInternalTool struct {
	agentID string
	gateway SubagentGatewayProvider
}

func NewSubagentStartInternalTool(agentID string, gateway SubagentGatewayProvider) *SubagentStartInternalTool {
	return &SubagentStartInternalTool{agentID: agentID, gateway: gateway}
}

func (t *SubagentStartInternalTool) Name() string { return "subagent_start_internal" }

// IsTimeoutExempt 仅创建异步任务记录后立即返回，不阻塞，豁免 Registry 全局兜底
func (t *SubagentStartInternalTool) IsTimeoutExempt() bool {
	return true
}

func (t *SubagentStartInternalTool) Description() string {
	return `Start an asynchronous internal subagent task in the current system.

## When to Use
- Delegate long-running or multi-step work that should keep running after the current turn ends.
- Use when another internal agent should work independently and report back later.
- Use when the parent session does not need to wait synchronously for the result.

## When NOT to Use
- Do not use for quick work you can finish with direct tools in the current turn.
- Do not use when you only need to read files, search code, or do a short one-shot action.
- Do not use this for external processes or shell-driven jobs; use subagent_start_external instead.

## Behavior
- Starts a new internal subagent record tied to the current user and parent session.
- Automatically creates a periodic checker schedule.
- After you dispatch the task, the current turn can end; you do not need to keep polling in the same turn.
- The checker will review progress on schedule, send brief status when needed, and use the session tool to wake the parent session with a summary when the subagent is done, failed, blocked, or stale.
- Returns the created subagent record as JSON.

## Parameters
- target_agent_id: Internal agent ID to run as the subagent. Defaults to the current agent.
- description: Short task label.
- prompt: Full task instructions for the internal subagent.
- checker_prompt: Prompt used by the periodic checker to inspect progress and decide what to report back.
- checker_schedule: Optional cron schedule for checker runs. Use 5 minutes as the normal default, then adjust to the expected task duration. Typical choices range from 1 minute, 5 minutes, 10 minutes, 30 minutes, and 1 hour up to day-level intervals when truly needed, though very long gaps are uncommon.

## Typical Workflow
1. Start the background internal task.
2. End the current turn if there is no more immediate work.
3. Use subagent_status only when you need progress or metadata now.
4. Use subagent_result when the session is woken or the task has finished and you want the final summary.
5. Use subagent_cancel only if the task should stop entirely.`
}

func (t *SubagentStartInternalTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"target_agent_id":  map[string]interface{}{"type": "string", "description": "Agent ID to run as the internal subagent. Defaults to the current agent."},
			"description":      map[string]interface{}{"type": "string", "description": "Short task name."},
			"prompt":           map[string]interface{}{"type": "string", "description": "Detailed task prompt for the subagent."},
			"checker_prompt":   map[string]interface{}{"type": "string", "description": "Prompt for the watcher schedule that checks progress and wakes the parent session."},
			"checker_schedule": map[string]interface{}{"type": "string", "description": "Cron schedule for checker runs. Use 5 minutes as the normal default, then shorten or lengthen it based on the expected task duration."},
		},
		"required": []string{"description", "prompt", "checker_prompt"},
	}
}

func (t *SubagentStartInternalTool) Execute(args map[string]interface{}) (string, error) {
	return "", fmt.Errorf("subagent_start_internal requires user context")
}

func (t *SubagentStartInternalTool) ExecuteWithUserContext(ctx context.Context, args map[string]interface{}, role models.Role, userID string, userIsolationEnabled bool, workspace string) (string, error) {
	gw, err := t.resolveGateway()
	if err != nil {
		return "", err
	}
	description, _ := args["description"].(string)
	prompt, _ := args["prompt"].(string)
	checkerPrompt, _ := args["checker_prompt"].(string)
	checkerSchedule, _ := args["checker_schedule"].(string)
	targetAgentID, _ := args["target_agent_id"].(string)
	record, err := gw.CreateInternalSubagent(subagent.StartInternalRequest{
		CallerAgentID:    t.agentID,
		TargetAgentID:    strings.TrimSpace(targetAgentID),
		UserID:           userID,
		Role:             string(role),
		ParentSessionKey: SessionKeyFromContext(ctx),
		Description:      strings.TrimSpace(description),
		Prompt:           strings.TrimSpace(prompt),
		CheckerPrompt:    strings.TrimSpace(checkerPrompt),
		CheckerSchedule:  strings.TrimSpace(checkerSchedule),
	})
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (t *SubagentStartInternalTool) resolveGateway() (SubagentGateway, error) {
	return resolveSubagentGateway(t.gateway)
}
