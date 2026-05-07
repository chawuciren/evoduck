package tools

import (
	"fmt"
	"sort"
	"strings"

	"github.com/chawuciren/evoduck/internal/skill"
	"github.com/chawuciren/evoduck/pkg/models"
)

type skillAccess struct {
	loader *skill.Loader
	role   models.Role
}

func skillRoleAllowed(required models.Role, actual models.Role) bool {
	if required == "" {
		return true
	}

	switch required {
	case models.RoleAdmin:
		return actual == models.RoleAdmin
	case models.RoleEmployee:
		return actual == models.RoleAdmin || actual == models.RoleEmployee
	case models.RoleCustomer:
		return true
	default:
		return actual == required
	}
}

func newSkillAccess(loader *skill.Loader, role models.Role) skillAccess {
	return skillAccess{loader: loader, role: role}
}

func (a skillAccess) visibleSkills() []*skill.Skill {
	skills := a.loader.List()
	visible := make([]*skill.Skill, 0, len(skills))
	for _, s := range skills {
		if !skillRoleAllowed(s.Role, a.role) {
			continue
		}
		visible = append(visible, s)
	}
	sort.Slice(visible, func(i, j int) bool {
		return visible[i].Name < visible[j].Name
	})
	return visible
}

func (a skillAccess) getVisibleSkill(name string) (*skill.Skill, error) {
	s, err := a.loader.Get(name)
	if err != nil {
		return nil, fmt.Errorf("get skill: %w", err)
	}
	if s == nil {
		return nil, fmt.Errorf("skill not found: %s", name)
	}
	if !skillRoleAllowed(s.Role, a.role) {
		return nil, fmt.Errorf("skill '%s' requires role '%s', you have '%s'", name, s.Role, a.role)
	}
	return s, nil
}

func skillSummaryLine(s *skill.Skill) string {
	desc := strings.TrimSpace(s.Description)
	if desc == "" {
		desc = "No description"
	}
	return fmt.Sprintf("- `%s`: %s", s.Name, desc)
}

func skillTagsLine(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	return fmt.Sprintf("Tags: %s", strings.Join(tags, ", "))
}

func renderSkillList(skills []*skill.Skill) string {
	if len(skills) == 0 {
		return "No skills available."
	}

	var b strings.Builder
	b.WriteString("Available skills:\n\n")
	for _, s := range skills {
		b.WriteString(skillSummaryLine(s))
		b.WriteString("\n")
		if tags := skillTagsLine(s.Tags); tags != "" {
			b.WriteString(tags)
			b.WriteString("\n")
		}
		if s.Role != "" {
			b.WriteString(fmt.Sprintf("Role: %s\n", s.Role))
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func renderSkillDetail(s *skill.Skill) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# Skill: %s\n\n", s.Name))

	if strings.TrimSpace(s.Description) != "" {
		b.WriteString("## Summary\n")
		b.WriteString(s.Description)
		b.WriteString("\n\n")
	}

	if s.Role != "" || len(s.Tags) > 0 || s.Location != "" || s.License != "" || len(s.Compatibility) > 0 || len(s.Metadata) > 0 {
		b.WriteString("## Metadata\n")
		if s.License != "" {
			b.WriteString(fmt.Sprintf("- License: %s\n", s.License))
		}
		if len(s.Compatibility) > 0 {
			b.WriteString(fmt.Sprintf("- Compatibility: %s\n", strings.Join(s.Compatibility, ", ")))
		}
		if s.Role != "" {
			b.WriteString(fmt.Sprintf("- Role: %s\n", s.Role))
		}
		if len(s.Tags) > 0 {
			b.WriteString(fmt.Sprintf("- Tags: %s\n", strings.Join(s.Tags, ", ")))
		}
		if metadata := renderMetadataSummary(s.Metadata); metadata != "" {
			b.WriteString(fmt.Sprintf("- Metadata: %s\n", metadata))
		}
		if s.Location != "" {
			b.WriteString(fmt.Sprintf("- Location: %s\n", s.Location))
		}
		if baseDir := s.BaseDir(); baseDir != "" {
			b.WriteString(fmt.Sprintf("- BaseDir: %s\n", baseDir))
		}
		b.WriteString("\n")
	}

	if deprecated := s.DeprecatedSummary(); deprecated != "" {
		b.WriteString("## Deprecation Warnings\n")
		b.WriteString(fmt.Sprintf("This skill uses deprecated frontmatter fields: %s. Prefer standard fields and `metadata.evoduck` extensions.\n\n", deprecated))
	}

	b.WriteString("## Usage\n")
	b.WriteString("Use `skill_use` with this skill name to load the full instructions. Inspect details first when you are not sure the skill applies.\n")

	if strings.TrimSpace(s.Content) != "" {
		b.WriteString("\n## Content Preview\n\n")
		b.WriteString(s.Content)
	}

	return strings.TrimSpace(b.String())
}

func renderMetadataSummary(metadata map[string]interface{}) string {
	if len(metadata) == 0 {
		return ""
	}
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", key, metadata[key]))
	}
	return strings.Join(parts, ", ")
}

func renderSkillUsage(s *skill.Skill) (string, error) {
	content := s.Render()
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# Skill: %s\n\n", s.Name))

	if s.Description != "" {
		b.WriteString(fmt.Sprintf("## Description\n%s\n\n", s.Description))
	}

	if deprecated := s.DeprecatedSummary(); deprecated != "" {
		b.WriteString(fmt.Sprintf("## Deprecation Warnings\nThis skill uses deprecated frontmatter fields: %s.\n\n", deprecated))
	}

	b.WriteString("## Instructions\n\n")
	b.WriteString(content)
	return b.String(), nil
}

type SkillListTool struct {
	access skillAccess
}

func NewSkillListTool(loader *skill.Loader, role models.Role) *SkillListTool {
	return &SkillListTool{access: newSkillAccess(loader, role)}
}

func (t *SkillListTool) Name() string {
	return "skill_list"
}

func (t *SkillListTool) Description() string {
	return "List available skills with short summaries. Use this first when you need a reusable procedure, playbook, or task-specific instruction set."
}

func (t *SkillListTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *SkillListTool) Execute(_ map[string]interface{}) (string, error) {
	return renderSkillList(t.access.visibleSkills()), nil
}

type SkillDetailTool struct {
	access skillAccess
}

func NewSkillDetailTool(loader *skill.Loader, role models.Role) *SkillDetailTool {
	return &SkillDetailTool{access: newSkillAccess(loader, role)}
}

func (t *SkillDetailTool) Name() string {
	return "skill_detail"
}

func (t *SkillDetailTool) Description() string {
	return "Show detailed information for one skill, including metadata, role limits, and content preview. Use this before skill_use when you need to confirm the skill applies."
}

func (t *SkillDetailTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type":        "string",
				"description": "The skill name to inspect",
			},
		},
		"required": []string{"name"},
	}
}

func (t *SkillDetailTool) Execute(args map[string]interface{}) (string, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return "", fmt.Errorf("skill name is required")
	}

	s, err := t.access.getVisibleSkill(name)
	if err != nil {
		return "", err
	}
	return renderSkillDetail(s), nil
}

type SkillUseTool struct {
	access skillAccess
}

func NewSkillUseTool(loader *skill.Loader, role models.Role) *SkillUseTool {
	return &SkillUseTool{access: newSkillAccess(loader, role)}
}

func (t *SkillUseTool) Name() string {
	return "skill_use"
}

func (t *SkillUseTool) Description() string {
	return "Load a skill's full instructions. Use this only after you identify a relevant skill to apply."
}

func (t *SkillUseTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type":        "string",
				"description": "The skill name to use",
			},
		},
		"required": []string{"name"},
	}
}

func (t *SkillUseTool) Execute(args map[string]interface{}) (string, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return "", fmt.Errorf("skill name is required")
	}

	s, err := t.access.getVisibleSkill(name)
	if err != nil {
		return "", err
	}
	return renderSkillUsage(s)
}

// SkillTool 保留为兼容入口，行为等同于 skill_use。
type SkillTool struct {
	access skillAccess
}

func NewSkillTool(loader *skill.Loader, role models.Role) *SkillTool {
	return &SkillTool{access: newSkillAccess(loader, role)}
}

func (t *SkillTool) Name() string {
	return "skill"
}

func (t *SkillTool) Description() string {
	return "Compatibility alias for skill_use. Prefer skill_list to discover skills and skill_detail to inspect one before applying it."
}

func (t *SkillTool) Parameters() map[string]interface{} {
	return NewSkillUseTool(t.access.loader, t.access.role).Parameters()
}

func (t *SkillTool) Execute(args map[string]interface{}) (string, error) {
	return NewSkillUseTool(t.access.loader, t.access.role).Execute(args)
}

// SetRole 设置角色（用于动态更新权限）
func (t *SkillTool) SetRole(role models.Role) {
	t.access.role = role
}
