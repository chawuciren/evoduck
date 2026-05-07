package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chawuciren/evoduck/internal/subagent"
	"github.com/chawuciren/evoduck/pkg/models"
)

type SubagentGateway interface {
	CreateInternalSubagent(req subagent.StartInternalRequest) (*subagent.Record, error)
	CreateExternalSubagent(req subagent.StartExternalRequest) (*subagent.Record, error)
	ListSubagents(agentID, userID string) []subagent.Record
	GetSubagent(agentID, userID, id string) (*subagent.Record, error)
	CancelSubagent(agentID, userID, id string) (*subagent.Record, error)
}

type SubagentGatewayProvider func() SubagentGateway

type SubagentStartInternalTool struct {
	agentID string
	gateway SubagentGatewayProvider
}

type SubagentStartExternalTool struct {
	agentID string
	gateway SubagentGatewayProvider
}

func NewSubagentStartInternalTool(agentID string, gateway SubagentGatewayProvider) *SubagentStartInternalTool {
	return &SubagentStartInternalTool{agentID: agentID, gateway: gateway}
}

func NewSubagentStartExternalTool(agentID string, gateway SubagentGatewayProvider) *SubagentStartExternalTool {
	return &SubagentStartExternalTool{agentID: agentID, gateway: gateway}
}

func (t *SubagentStartInternalTool) Name() string { return "subagent_start_internal" }
func (t *SubagentStartInternalTool) Description() string {
	return "Start an asynchronous internal subagent task and automatically create its watcher schedule. Use for long-running work that should not block the parent session."
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

func (t *SubagentStartExternalTool) Name() string { return "subagent_start_external" }
func (t *SubagentStartExternalTool) Description() string {
	return "Start an authorized external subagent process and automatically create its watcher schedule. Use for external coding agents such as OpenCode or long-running shell-driven work."
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

type SubagentListTool struct {
	agentID string
	gateway SubagentGatewayProvider
}
type SubagentStatusTool struct {
	agentID string
	gateway SubagentGatewayProvider
}
type SubagentResultTool struct {
	agentID string
	gateway SubagentGatewayProvider
}
type SubagentCancelTool struct {
	agentID string
	gateway SubagentGatewayProvider
}

func NewSubagentListTool(agentID string, gateway SubagentGatewayProvider) *SubagentListTool {
	return &SubagentListTool{agentID: agentID, gateway: gateway}
}
func NewSubagentStatusTool(agentID string, gateway SubagentGatewayProvider) *SubagentStatusTool {
	return &SubagentStatusTool{agentID: agentID, gateway: gateway}
}
func NewSubagentResultTool(agentID string, gateway SubagentGatewayProvider) *SubagentResultTool {
	return &SubagentResultTool{agentID: agentID, gateway: gateway}
}
func NewSubagentCancelTool(agentID string, gateway SubagentGatewayProvider) *SubagentCancelTool {
	return &SubagentCancelTool{agentID: agentID, gateway: gateway}
}

func (t *SubagentListTool) Name() string { return "subagent_list" }
func (t *SubagentListTool) Description() string {
	return "List asynchronous subagent tasks visible to the current user and agent."
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

func (t *SubagentStatusTool) Name() string { return "subagent_status" }
func (t *SubagentStatusTool) Description() string {
	return "Get status and metadata for an asynchronous subagent task."
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

func (t *SubagentResultTool) Name() string { return "subagent_result" }
func (t *SubagentResultTool) Description() string {
	return "Read the final result summary for a subagent task. The parent session must still verify the result."
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

func (t *SubagentCancelTool) Name() string { return "subagent_cancel" }
func (t *SubagentCancelTool) Description() string {
	return "Request cancellation of a visible asynchronous subagent task."
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

func idParamSchema() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string", "description": "Subagent task ID"}}, "required": []string{"id"}}
}

func (t *SubagentStartInternalTool) resolveGateway() (SubagentGateway, error) {
	return resolveSubagentGateway(t.gateway)
}
func (t *SubagentStartExternalTool) resolveGateway() (SubagentGateway, error) {
	return resolveSubagentGateway(t.gateway)
}
func (t *SubagentListTool) resolveGateway() (SubagentGateway, error) {
	return resolveSubagentGateway(t.gateway)
}
func (t *SubagentStatusTool) resolveGateway() (SubagentGateway, error) {
	return resolveSubagentGateway(t.gateway)
}
func (t *SubagentResultTool) resolveGateway() (SubagentGateway, error) {
	return resolveSubagentGateway(t.gateway)
}
func (t *SubagentCancelTool) resolveGateway() (SubagentGateway, error) {
	return resolveSubagentGateway(t.gateway)
}

func resolveSubagentGateway(provider SubagentGatewayProvider) (SubagentGateway, error) {
	if provider == nil {
		return nil, fmt.Errorf("subagent gateway unavailable")
	}
	gw := provider()
	if gw == nil {
		return nil, fmt.Errorf("subagent gateway unavailable")
	}
	return gw, nil
}
