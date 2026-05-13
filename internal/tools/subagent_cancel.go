package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chawuciren/evoduck/pkg/models"
)

type SubagentCancelTool struct {
	agentID string
	gateway SubagentGatewayProvider
}

func NewSubagentCancelTool(agentID string, gateway SubagentGatewayProvider) *SubagentCancelTool {
	return &SubagentCancelTool{agentID: agentID, gateway: gateway}
}

func (t *SubagentCancelTool) Name() string { return "subagent_cancel" }

func (t *SubagentCancelTool) Description() string {
	return `Request cancellation of one visible subagent task.

## When to Use
- Use when a running or queued task is no longer needed.
- Use when the task was started with the wrong direction, wrong scope, or is wasting resources.
- Use when the parent session has decided the task should stop entirely.

## When NOT to Use
- Do not use this as the first step when you only want progress; use subagent_status instead.
- Do not use this to read the final output; use subagent_result.
- Do not use this as a routine retry mechanism unless you have decided the current task should be abandoned.

## Behavior
- Sends a cancellation request for the specified visible task.
- Returns the updated task record as JSON.
- If you are unsure whether cancellation is necessary, inspect the task with subagent_status first.
- Prefer letting the periodic checker wake the session for normal completion flow; use cancellation only when you have decided the task should stop.

## Parameters
- id: Subagent task ID.`
}

func (t *SubagentCancelTool) Parameters() map[string]interface{} { return idParamSchema() }

func (t *SubagentCancelTool) Execute(args map[string]interface{}) (string, error) {
	return "", fmt.Errorf("subagent_cancel requires user context")
}

func (t *SubagentCancelTool) ExecuteWithUserContext(ctx context.Context, args map[string]interface{}, role models.Role, userID string, userIsolationEnabled bool, workspace string) (string, error) {
	gw, err := t.resolveGateway()
	if err != nil {
		return "", err
	}
	id, _ := args["id"].(string)
	record, err := gw.CancelSubagent(t.agentID, userID, strings.TrimSpace(id))
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (t *SubagentCancelTool) resolveGateway() (SubagentGateway, error) {
	return resolveSubagentGateway(t.gateway)
}
