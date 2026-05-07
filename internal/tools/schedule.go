package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chawuciren/evoduck/internal/command"
	"github.com/chawuciren/evoduck/internal/scheduler"
	"github.com/chawuciren/evoduck/pkg/models"
)

type ScheduleManager interface {
	GetDefaultAgentID() string
	ListSchedules(agentID, userID string) []command.ScheduleInfo
	CreateSchedule(agentID, userID string, role models.Role, req command.CreateScheduleRequest) (*command.ScheduleInfo, error)
	SetScheduleEnabled(agentID, userID, id string, enabled bool) error
	DeleteSchedule(agentID, userID, id string) error
	TriggerSchedule(agentID, userID, id string, source scheduler.TriggerSource) error
}

type ScheduleManagerProvider func() ScheduleManager

type ScheduleListTool struct {
	agentID  string
	manager  ScheduleManagerProvider
}

func NewScheduleListTool(agentID string, manager ScheduleManagerProvider) *ScheduleListTool {
	return &ScheduleListTool{agentID: agentID, manager: manager}
}

func (t *ScheduleListTool) Name() string { return "schedule_list" }

func (t *ScheduleListTool) Description() string {
	return "List schedules for the current user under the current agent."
}

func (t *ScheduleListTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *ScheduleListTool) Execute(args map[string]interface{}) (string, error) {
	return "", fmt.Errorf("schedule_list requires user context")
}

func (t *ScheduleListTool) ExecuteWithUserContext(ctx context.Context, args map[string]interface{}, role models.Role, userID string, userIsolationEnabled bool, workspace string) (string, error) {
	mgr, agentID, err := t.resolveManagerAndAgent(userID)
	if err != nil {
		return "", err
	}
	schedules := mgr.ListSchedules(agentID, userID)
	if len(schedules) == 0 {
		return "No schedules found.", nil
	}
	data, err := json.MarshalIndent(schedules, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

type ScheduleCreateTool struct {
	agentID string
	manager ScheduleManagerProvider
}

func NewScheduleCreateTool(agentID string, manager ScheduleManagerProvider) *ScheduleCreateTool {
	return &ScheduleCreateTool{agentID: agentID, manager: manager}
}

func (t *ScheduleCreateTool) Name() string { return "schedule_create" }

func (t *ScheduleCreateTool) Description() string {
	return "Create a schedule for the current user under the current agent."
}

func (t *ScheduleCreateTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string", "description": "Short task name"},
			"description": map[string]interface{}{"type": "string", "description": "Optional task description"},
			"schedule": map[string]interface{}{"type": "string", "description": "Cron expression"},
			"prompt": map[string]interface{}{"type": "string", "description": "Prompt to execute on each run"},
			"enabled": map[string]interface{}{"type": "boolean", "description": "Whether the task starts enabled, default true"},
			"channel": map[string]interface{}{"type": "string", "description": "Optional dedicated session key override"},
		},
		"required": []string{"name", "schedule", "prompt"},
	}
}

func (t *ScheduleCreateTool) Execute(args map[string]interface{}) (string, error) {
	return "", fmt.Errorf("schedule_create requires user context")
}

func (t *ScheduleCreateTool) ExecuteWithUserContext(ctx context.Context, args map[string]interface{}, role models.Role, userID string, userIsolationEnabled bool, workspace string) (string, error) {
	mgr, agentID, err := t.resolveManagerAndAgent(userID)
	if err != nil {
		return "", err
	}
	name, _ := args["name"].(string)
	schedule, _ := args["schedule"].(string)
	prompt, _ := args["prompt"].(string)
	description, _ := args["description"].(string)
	channel, _ := args["channel"].(string)
	var enabled *bool
	if raw, ok := args["enabled"].(bool); ok {
		enabled = &raw
	}
	created, err := mgr.CreateSchedule(agentID, userID, role, command.CreateScheduleRequest{
		Name:        strings.TrimSpace(name),
		Description: strings.TrimSpace(description),
		Schedule:    strings.TrimSpace(schedule),
		Prompt:      strings.TrimSpace(prompt),
		Enabled:     enabled,
		Channel:     strings.TrimSpace(channel),
		OriginSessionKey: strings.TrimSpace(SessionKeyFromContext(ctx)),
	})
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(created, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

type ScheduleEnableTool struct {
	agentID string
	manager ScheduleManagerProvider
	enabled bool
	name    string
}

func NewScheduleEnableTool(agentID string, manager ScheduleManagerProvider, enabled bool) *ScheduleEnableTool {
	name := "schedule_disable"
	if enabled {
		name = "schedule_enable"
	}
	return &ScheduleEnableTool{agentID: agentID, manager: manager, enabled: enabled, name: name}
}

func (t *ScheduleEnableTool) Name() string { return t.name }

func (t *ScheduleEnableTool) Description() string {
		if t.enabled {
		return "Enable a schedule belonging to the current user."
	}
	return "Disable a schedule belonging to the current user."
}

func (t *ScheduleEnableTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id": map[string]interface{}{"type": "string", "description": "Schedule ID"},
		},
		"required": []string{"id"},
	}
}

func (t *ScheduleEnableTool) Execute(args map[string]interface{}) (string, error) {
	return "", fmt.Errorf("%s requires user context", t.name)
}

func (t *ScheduleEnableTool) ExecuteWithUserContext(ctx context.Context, args map[string]interface{}, role models.Role, userID string, userIsolationEnabled bool, workspace string) (string, error) {
	mgr, agentID, err := t.resolveManagerAndAgent(userID)
	if err != nil {
		return "", err
	}
	id, _ := args["id"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("id is required")
	}
	if err := mgr.SetScheduleEnabled(agentID, userID, id, t.enabled); err != nil {
		return "", err
	}
	if t.enabled {
		return fmt.Sprintf("Enabled schedule: %s", id), nil
	}
	return fmt.Sprintf("Disabled schedule: %s", id), nil
}

type ScheduleDeleteTool struct {
	agentID string
	manager ScheduleManagerProvider
}

type ScheduleTriggerTool struct {
	agentID string
	manager ScheduleManagerProvider
}

func NewScheduleDeleteTool(agentID string, manager ScheduleManagerProvider) *ScheduleDeleteTool {
	return &ScheduleDeleteTool{agentID: agentID, manager: manager}
}

func NewScheduleTriggerTool(agentID string, manager ScheduleManagerProvider) *ScheduleTriggerTool {
	return &ScheduleTriggerTool{agentID: agentID, manager: manager}
}

func (t *ScheduleDeleteTool) Name() string { return "schedule_delete" }

func (t *ScheduleDeleteTool) Description() string {
	return "Delete a schedule belonging to the current user."
}

func (t *ScheduleDeleteTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id": map[string]interface{}{"type": "string", "description": "Schedule ID"},
		},
		"required": []string{"id"},
	}
}

func (t *ScheduleDeleteTool) Execute(args map[string]interface{}) (string, error) {
	return "", fmt.Errorf("schedule_delete requires user context")
}

func (t *ScheduleDeleteTool) ExecuteWithUserContext(ctx context.Context, args map[string]interface{}, role models.Role, userID string, userIsolationEnabled bool, workspace string) (string, error) {
	mgr, agentID, err := t.resolveManagerAndAgent(userID)
	if err != nil {
		return "", err
	}
	id, _ := args["id"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("id is required")
	}
	if err := mgr.DeleteSchedule(agentID, userID, id); err != nil {
		return "", err
	}
	return fmt.Sprintf("Deleted schedule: %s", id), nil
}

func (t *ScheduleTriggerTool) Name() string { return "schedule_trigger" }

func (t *ScheduleTriggerTool) Description() string {
	return "Trigger a schedule immediately for the current user using its existing execution session."
}

func (t *ScheduleTriggerTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id": map[string]interface{}{"type": "string", "description": "Schedule ID"},
		},
		"required": []string{"id"},
	}
}

func (t *ScheduleTriggerTool) Execute(args map[string]interface{}) (string, error) {
	return "", fmt.Errorf("schedule_trigger requires user context")
}

func (t *ScheduleTriggerTool) ExecuteWithUserContext(ctx context.Context, args map[string]interface{}, role models.Role, userID string, userIsolationEnabled bool, workspace string) (string, error) {
	mgr, agentID, err := t.resolveManagerAndAgent(userID)
	if err != nil {
		return "", err
	}
	id, _ := args["id"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("id is required")
	}
	if err := mgr.TriggerSchedule(agentID, userID, id, scheduler.TriggerSourceTool); err != nil {
		return "", err
	}
	return fmt.Sprintf("Triggered schedule: %s", id), nil
}

func (t *ScheduleListTool) resolveManagerAndAgent(userID string) (ScheduleManager, string, error) {
	return resolveScheduleManager(t.agentID, t.manager, userID)
}

func (t *ScheduleCreateTool) resolveManagerAndAgent(userID string) (ScheduleManager, string, error) {
	return resolveScheduleManager(t.agentID, t.manager, userID)
}

func (t *ScheduleEnableTool) resolveManagerAndAgent(userID string) (ScheduleManager, string, error) {
	return resolveScheduleManager(t.agentID, t.manager, userID)
}

func (t *ScheduleDeleteTool) resolveManagerAndAgent(userID string) (ScheduleManager, string, error) {
	return resolveScheduleManager(t.agentID, t.manager, userID)
}

func (t *ScheduleTriggerTool) resolveManagerAndAgent(userID string) (ScheduleManager, string, error) {
	return resolveScheduleManager(t.agentID, t.manager, userID)
}

func resolveScheduleManager(agentID string, provider ScheduleManagerProvider, userID string) (ScheduleManager, string, error) {
	if userID == "" {
		return nil, "", fmt.Errorf("user context is required")
	}
	if provider == nil {
		return nil, "", fmt.Errorf("schedule manager unavailable")
	}
	mgr := provider()
	if mgr == nil {
		return nil, "", fmt.Errorf("schedule manager unavailable")
	}
	resolvedAgentID := strings.TrimSpace(agentID)
	if resolvedAgentID == "" {
		resolvedAgentID = strings.TrimSpace(mgr.GetDefaultAgentID())
	}
	if resolvedAgentID == "" {
		return nil, "", fmt.Errorf("agent id is required")
	}
	return mgr, resolvedAgentID, nil
}
