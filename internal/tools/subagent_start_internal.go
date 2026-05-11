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

func (t *SubagentStartInternalTool) Description() string {
	return `Start an asynchronous internal subagent task in the current system.

## When to Use
- Delegate long-running or multi-step work that should continue without blocking the current session.
- Use when another internal agent should work independently and report back later.
- Use when the parent session may move on and come back later to inspect progress or fetch the result.

## When NOT to Use
- Do not use for quick work you can finish with a direct tool call in the current turn.
- Do not use when you only need to read a file, search code, or perform a short one-shot action.
- Do not use this for external processes or shell-driven jobs; use subagent_start_external instead.

## Behavior
- Starts a new internal subagent record tied to the current user and parent session.
- Automatically creates the watcher schedule for follow-up checks.
- Returns the created subagent record as JSON.
- After starting the task, use subagent_status to inspect progress and subagent_result to read the final summary.

## Parameters
- target_agent_id: Internal agent ID to run as the subagent. Defaults to the current agent.
- description: Short task label.
- prompt: Full task instructions for the internal subagent.
- checker_prompt: Prompt used by the watcher schedule to check progress and wake the parent session.
- checker_schedule: Optional cron schedule for watcher checks. Defaults to every 3 minutes.

## Typical Workflow
1. Start the background internal task here.
2. Use subagent_status if you need progress or metadata.
3. Use subagent_result when the task has finished and you want the result summary.
4. Use subagent_cancel only if the task should stop entirely.`
}

func (t *SubagentStartInternalTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"target_agent_id":  map[string]interface{}{"type": "string", "description": "Agent ID to run as the internal subagent. Defaults to the current agent."},
			"description":      map[string]interface{}{"type": "string", "description": "Short task name."},
			"prompt":           map[string]interface{}{"type": "string", "description": "Detailed task prompt for the subagent."},
			"checker_prompt":   map[string]interface{}{"type": "string", "description": "Prompt for the watcher schedule that checks progress and wakes the parent session."},
			"checker_schedule": map[string]interface{}{"type": "string", "description": "Cron schedule for watcher checks, default every 3 minutes."},
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
