package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chawuciren/evoduck/internal/subagent"
	"github.com/chawuciren/evoduck/pkg/models"
)

type SubagentStartExternalTool struct {
	agentID string
	gateway SubagentGatewayProvider
}

func NewSubagentStartExternalTool(agentID string, gateway SubagentGatewayProvider) *SubagentStartExternalTool {
	return &SubagentStartExternalTool{agentID: agentID, gateway: gateway}
}

func (t *SubagentStartExternalTool) Name() string { return "subagent_start_external" }

func (t *SubagentStartExternalTool) Description() string {
	return `Start an asynchronous external subagent process through an authorized provider.

## When to Use
- Delegate long-running work to an external coding agent, shell-driven worker, or provider-backed process.
- Use when the task should run outside the current in-process tool execution path.
- Use when the parent session should continue while the external task runs independently.

## When NOT to Use
- Do not use for quick work you can complete directly in the current session.
- Do not use for internal agent-to-agent delegation; use subagent_start_internal instead.
- Do not use this just to inspect existing subagents; use subagent_list or subagent_status.

## Behavior
- Starts an external subagent record tied to the current user and parent session.
- Automatically creates the watcher schedule for follow-up checks.
- Returns the created subagent record as JSON.
- After starting the task, use subagent_status to inspect progress and subagent_result to read the final summary.

## Parameters
- provider: External provider name, such as opencode. Defaults to process.
- description: Short task label.
- command: Command used to launch the external process.
- working_directory: Optional working directory for the process. Defaults to the current agent workspace.
- checker_prompt: Prompt used by the watcher schedule to check progress and wake the parent session.
- checker_schedule: Optional cron schedule for watcher checks. Defaults to every 3 minutes.

## Safety / Constraints
- This starts an external provider or process, not an internal agent inside the current runtime.
- If you only need to monitor an existing task, do not start a new one; use subagent_status first.`
}

func (t *SubagentStartExternalTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"provider":          map[string]interface{}{"type": "string", "description": "External subagent provider name, such as opencode. Defaults to process."},
			"description":       map[string]interface{}{"type": "string", "description": "Short task name."},
			"command":           map[string]interface{}{"type": "string", "description": "Command to start the external process."},
			"working_directory": map[string]interface{}{"type": "string", "description": "Working directory for the process. Defaults to current agent workspace."},
			"checker_prompt":    map[string]interface{}{"type": "string", "description": "Prompt for the watcher schedule that checks progress and wakes the parent session."},
			"checker_schedule":  map[string]interface{}{"type": "string", "description": "Cron schedule for watcher checks, default every 3 minutes."},
		},
		"required": []string{"description", "command", "checker_prompt"},
	}
}

func (t *SubagentStartExternalTool) Execute(args map[string]interface{}) (string, error) {
	return "", fmt.Errorf("subagent_start_external requires user context")
}

func (t *SubagentStartExternalTool) ExecuteWithUserContext(ctx context.Context, args map[string]interface{}, role models.Role, userID string, userIsolationEnabled bool, workspace string) (string, error) {
	gw, err := t.resolveGateway()
	if err != nil {
		return "", err
	}
	provider, _ := args["provider"].(string)
	description, _ := args["description"].(string)
	command, _ := args["command"].(string)
	workdir, _ := args["working_directory"].(string)
	checkerPrompt, _ := args["checker_prompt"].(string)
	checkerSchedule, _ := args["checker_schedule"].(string)
	record, err := gw.CreateExternalSubagent(subagent.StartExternalRequest{
		CallerAgentID:    t.agentID,
		UserID:           userID,
		Role:             string(role),
		ParentSessionKey: SessionKeyFromContext(ctx),
		Provider:         strings.TrimSpace(provider),
		Description:      strings.TrimSpace(description),
		Command:          strings.TrimSpace(command),
		WorkingDirectory: strings.TrimSpace(workdir),
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

func (t *SubagentStartExternalTool) resolveGateway() (SubagentGateway, error) {
	return resolveSubagentGateway(t.gateway)
}
