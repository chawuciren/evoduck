package plugin

import (
	"context"
	"fmt"

	"github.com/chawuciren/evoduck/internal/tools"
	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/google/uuid"
)

type ToolAdapter struct {
	pluginID     string
	capabilityID string
	name         string
	description  string
	parameters   map[string]interface{}
	manager      *Manager
}

func NewToolAdapter(manager *Manager, pluginID string, capability Capability) *ToolAdapter {
	return &ToolAdapter{
		pluginID:     pluginID,
		capabilityID: capability.CapabilityID,
		name:         capability.ToolName,
		description:  capability.Description,
		parameters:   capability.Parameters,
		manager:      manager,
	}
}

func (t *ToolAdapter) Name() string {
	return t.name
}

func (t *ToolAdapter) Description() string {
	return t.description
}

func (t *ToolAdapter) Parameters() map[string]interface{} {
	return t.parameters
}

func (t *ToolAdapter) Execute(args map[string]interface{}) (string, error) {
	return t.ExecuteWithRole(context.Background(), args, "")
}

func (t *ToolAdapter) ExecuteWithRole(ctx context.Context, args map[string]interface{}, _ models.Role) (string, error) {
	if t.manager == nil {
		return "", fmt.Errorf("plugin manager is nil")
	}

	result, err := t.manager.ExecuteTool(ctx, t.pluginID, t.capabilityID, t.name, args, uuid.NewString())
	if err != nil {
		return "", err
	}
	return result, nil
}

var _ tools.ToolWithContext = (*ToolAdapter)(nil)
