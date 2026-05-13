package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/chawuciren/evoduck/pkg/models"
)

type SubagentResultTool struct {
	agentID string
	gateway SubagentGatewayProvider
}

func NewSubagentResultTool(agentID string, gateway SubagentGatewayProvider) *SubagentResultTool {
	return &SubagentResultTool{agentID: agentID, gateway: gateway}
}

func (t *SubagentResultTool) Name() string { return "subagent_result" }

func (t *SubagentResultTool) Description() string {
	return `Read the final result summary for one specific subagent task.

## When to Use
- Use when you already know the task ID and want the result summary rather than raw status metadata.
- Use after subagent_status indicates the task is complete, or when the periodic checker wakes the parent session.
- Use when the parent session needs the subagent's summarized outcome.

## When NOT to Use
- Do not use this to discover tasks; use subagent_list.
- Do not use this when you primarily need progress or metadata; use subagent_status.
- Do not assume an empty result means failure; the task may still be running or may not have written a summary yet.

## What It Returns
- Returns the task's result summary text when available.
- Returns a friendly "not available yet" message when no summary has been recorded.
- The parent session must still verify important conclusions before acting on them.

## Parameters
- id: Subagent task ID.`
}

func (t *SubagentResultTool) Parameters() map[string]interface{} { return idParamSchema() }

func (t *SubagentResultTool) Execute(args map[string]interface{}) (string, error) {
	return "", fmt.Errorf("subagent_result requires user context")
}

func (t *SubagentResultTool) ExecuteWithUserContext(ctx context.Context, args map[string]interface{}, role models.Role, userID string, userIsolationEnabled bool, workspace string) (string, error) {
	gw, err := t.resolveGateway()
	if err != nil {
		return "", err
	}
	id, _ := args["id"].(string)
	record, err := gw.GetSubagent(t.agentID, userID, strings.TrimSpace(id))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(record.ResultSummary) == "" {
		return "No result summary is available yet.", nil
	}
	return record.ResultSummary, nil
}

func (t *SubagentResultTool) resolveGateway() (SubagentGateway, error) {
	return resolveSubagentGateway(t.gateway)
}
