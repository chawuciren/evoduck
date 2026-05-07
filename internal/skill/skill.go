package skill

import (
	"path/filepath"
	"strings"

	"github.com/chawuciren/evoduck/pkg/models"
)

// SkillParameter Skill 参数定义
type SkillParameter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
	Default     string `yaml:"default"`
}

// Skill 技能定义
type Skill struct {
	Name             string
	Description      string
	Location         string
	License          string
	Compatibility    []string
	Metadata         map[string]interface{}
	Role             models.Role
	Tags             []string
	Content          string           // 原始内容
	Parameters       []SkillParameter // Deprecated: legacy template parameters.
	DeprecatedFields []string
}

func (s *Skill) HasDeprecatedField(name string) bool {
	for _, field := range s.DeprecatedFields {
		if field == name {
			return true
		}
	}
	return false
}

func (s *Skill) DeprecatedSummary() string {
	if len(s.DeprecatedFields) == 0 {
		return ""
	}
	return strings.Join(s.DeprecatedFields, ", ")
}

func (s *Skill) Render() string {
	return strings.ReplaceAll(s.Content, "{baseDir}", s.BaseDir())
}

func (s *Skill) BaseDir() string {
	if s.Location == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(filepath.Dir(s.Location)))
}
