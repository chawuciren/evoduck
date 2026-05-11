package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chawuciren/evoduck/pkg/models"
)

type SubagentListTool struct {
	agentID string
	gateway SubagentGatewayProvider
}

func NewSubagentListTool(agentID string, gateway SubagentGatewayProvider) *SubagentListTool {
	return &SubagentListTool{agentID: agentID, gateway: gateway}
}

func (t *SubagentListTool) Name() string { return "subagent_list" }

func (t *SubagentListTool) Description() string {
	return `List subagent tasks visible to the current user and agent.

## When to Use
- Use this as the entry point when you need to see which subagent tasks currently exist.
- Use when you do not yet know the task ID and need to inspect visible records.
- Use for quick status triage before choosing a specific task to inspect further.

## When NOT to Use
- Do not use this when you already know the task ID and want exact progress; use subagent_status.
- Do not use this to read the final task output; use subagent_result.
- Do not use this to start a new task; use subagent_start_internal or subagent_start_external.

## What It Returns
- Returns the visible subagent records as JSON.
- This is a listing and discovery tool, not a full result reader.
- After you pick an ID from the list, use subagent_status or subagent_result for the next step.

## Parameters
- This tool takes no parameters.`
}

func (t *SubagentListTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
}

func (t *SubagentListTool) Execute(args map[string]interface{}) (string, error) {
	return "", fmt.Errorf("subagent_list requires user context")
}

func (t *SubagentListTool) ExecuteWithUserContext(ctx context.Context, args map[string]interface{}, role models.Role, userID string, userIsolationEnabled bool, workspace string) (string, error) {
	gw, err := t.resolveGateway()
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(gw.ListSubagents(t.agentID, userID), "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (t *SubagentListTool) resolveGateway() (SubagentGateway, error) {
	return resolveSubagentGateway(t.gateway)
}
