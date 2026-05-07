package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/chawuciren/evoduck/pkg/models"
)

type SystemReloader interface {
	ReloadSystem(ctx context.Context, scope string) (string, error)
}

type SystemReloadTool struct {
	reloader SystemReloader
}

func NewSystemReloadTool(reloader SystemReloader) *SystemReloadTool {
	return &SystemReloadTool{reloader: reloader}
}

func (t *SystemReloadTool) Name() string {
	return "system_reload"
}

func (t *SystemReloadTool) Description() string {
	return "Reload system runtime state after changing files that affect configuration or skills. Use scope=\"skills\" after creating or updating SKILL.md files so skill_list and skill_detail can see the changes. Admin only."
}

func (t *SystemReloadTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"scope": map[string]interface{}{
				"type":        "string",
				"description": "Reload scope. Use skills after editing skill files. all currently includes skills and may include more runtime state later.",
				"enum":        []string{"skills", "config", "all"},
				"default":     "all",
			},
		},
	}
}

func (t *SystemReloadTool) Execute(args map[string]interface{}) (string, error) {
	return t.ExecuteWithRole(context.Background(), args, "")
}

func (t *SystemReloadTool) ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error) {
	if role != models.RoleAdmin {
		return "", fmt.Errorf("system_reload requires admin role")
	}
	if t.reloader == nil {
		return "", fmt.Errorf("system reload is not available")
	}

	scope := "all"
	if raw, ok := args["scope"]; ok {
		scope = strings.TrimSpace(fmt.Sprintf("%v", raw))
	}
	if scope == "" {
		scope = "all"
	}
	switch scope {
	case "skills", "config", "all":
		return t.reloader.ReloadSystem(ctx, scope)
	default:
		return "", fmt.Errorf("unsupported reload scope: %s", scope)
	}
}
