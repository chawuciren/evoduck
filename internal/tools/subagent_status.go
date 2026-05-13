package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chawuciren/evoduck/pkg/models"
)

type SubagentStatusTool struct {
	agentID string
	gateway SubagentGatewayProvider
}

func NewSubagentStatusTool(agentID string, gateway SubagentGatewayProvider) *SubagentStatusTool {
	return &SubagentStatusTool{agentID: agentID, gateway: gateway}
}

func (t *SubagentStatusTool) Name() string { return "subagent_status" }

func (t *SubagentStatusTool) Description() string {
	return `Read status and metadata for one specific subagent task.

## When to Use
- Use when you already know the subagent task ID and want its current state.
- Use to inspect progress, timestamps, ownership, and other task metadata.
- Use after subagent_list when you need detail about a specific record.

## When NOT to Use
- Do not use this to discover available tasks when you do not know the ID; use subagent_list first.
- Do not use this to read the final summary if the task is done; use subagent_result.
- Do not use this to stop a task; use subagent_cancel.

## What It Returns
- Returns the full visible task record as JSON.
- This is the progress-inspection tool in the subagent workflow.
- Use it when you need progress now; do not keep polling in the same turn after dispatching a background task unless there is a real immediate need.
- If you only need the final output summary, prefer subagent_result.

## Timing Guidance
- Background subagents are paired with a periodic checker.
- After dispatching a task, the normal path is to end the current turn and let the checker wake the session later.
- Reach for this tool when you intentionally want a manual progress check before that wake-up happens.

## Parameters
- id: Subagent task ID.

## Typical Workflow
1. Get an ID from subagent_list or a previous start call.
2. Use this tool to inspect progress.
3. Switch to subagent_result when you need the final summary.`
}

func (t *SubagentStatusTool) Parameters() map[string]interface{} { return idParamSchema() }

func (t *SubagentStatusTool) Execute(args map[string]interface{}) (string, error) {
	return "", fmt.Errorf("subagent_status requires user context")
}

func (t *SubagentStatusTool) ExecuteWithUserContext(ctx context.Context, args map[string]interface{}, role models.Role, userID string, userIsolationEnabled bool, workspace string) (string, error) {
	gw, err := t.resolveGateway()
	if err != nil {
		return "", err
	}
	id, _ := args["id"].(string)
	record, err := gw.GetSubagent(t.agentID, userID, strings.TrimSpace(id))
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (t *SubagentStatusTool) resolveGateway() (SubagentGateway, error) {
	return resolveSubagentGateway(t.gateway)
}
