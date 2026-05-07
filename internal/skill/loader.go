package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
	"gopkg.in/yaml.v3"
)

type Loader struct {
	mu        sync.RWMutex
	agentDir  string
	sharedDir string
	cache     map[string]*Skill
	metaCache map[string]*Skill
}

func NewLoader(agentDir, sharedDir string) *Loader {
	return &Loader{
		agentDir:  agentDir,
		sharedDir: sharedDir,
		cache:     make(map[string]*Skill),
		metaCache: make(map[string]*Skill),
	}
}

type skillFrontmatter struct {
	Name          string                 `yaml:"name"`
	Description   string                 `yaml:"description"`
	License       string                 `yaml:"license"`
	Compatibility interface{}            `yaml:"compatibility"`
	Metadata      map[string]interface{} `yaml:"metadata"`
	Requires      struct {
		Role string `yaml:"role"`
	} `yaml:"requires"`
	Tags       []string         `yaml:"tags"`
	Parameters []SkillParameter `yaml:"parameters"`
	Extends    []string         `yaml:"extends"` // 预留：skill 继承
}

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func (l *Loader) LoadAll() error {
	cache := make(map[string]*Skill)
	metaCache := make(map[string]*Skill)

	dirs := []string{
		filepath.Join(l.agentDir, "skills"),
		l.sharedDir,
	}

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			skillPath := filepath.Join(dir, entry.Name(), "SKILL.md")
			if _, err := os.Stat(skillPath); err != nil {
				continue
			}

			data, err := os.ReadFile(skillPath)
			if err != nil {
				continue
			}

			skill, err := l.parseSkill(entry.Name(), skillPath, string(data))
			if err != nil {
				logger.Warn("Failed to parse skill", logger.Fields{
					"skill": entry.Name(),
					"error": err.Error(),
					"path":  skillPath,
				})
				continue
			}

			cache[skill.Name] = skill
			metaCache[skill.Name] = &Skill{
				Name:             skill.Name,
				Description:      skill.Description,
				Location:         skill.Location,
				License:          skill.License,
				Compatibility:    skill.Compatibility,
				Metadata:         skill.Metadata,
				Role:             skill.Role,
				Tags:             skill.Tags,
				Parameters:       skill.Parameters,
				DeprecatedFields: skill.DeprecatedFields,
			}

			logger.Debug("Skill loaded", logger.Fields{
				"name":         skill.Name,
				"location":     skill.Location,
				"deprecated":   skill.DeprecatedSummary(),
				"has_metadata": len(skill.Metadata) > 0,
			})
		}
	}

	l.mu.Lock()
	l.cache = cache
	l.metaCache = metaCache
	l.mu.Unlock()

	return nil
}

func (l *Loader) Get(name string) (*Skill, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	skill, ok := l.cache[name]
	if !ok {
		return nil, nil
	}
	return skill, nil
}

func (l *Loader) List() []*Skill {
	l.mu.RLock()
	defer l.mu.RUnlock()

	skills := make([]*Skill, 0, len(l.metaCache))
	for _, s := range l.metaCache {
		skills = append(skills, s)
	}
	return skills
}

func (l *Loader) CheckRole(name string, role models.Role) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	skill, ok := l.metaCache[name]
	if !ok {
		return false
	}

	if skill.Role == "" {
		return true
	}

	switch skill.Role {
	case models.RoleAdmin:
		return role == models.RoleAdmin
	case models.RoleEmployee:
		return role == models.RoleAdmin || role == models.RoleEmployee
	case models.RoleCustomer:
		return true
	default:
		return role == skill.Role
	}
}

func (l *Loader) parseSkill(name, location, content string) (*Skill, error) {
	var fm skillFrontmatter

	// 解析 YAML frontmatter
	parts := strings.SplitN(content, "---", 3)
	if len(parts) >= 3 {
		if err := yaml.Unmarshal([]byte(parts[1]), &fm); err != nil {
			return nil, fmt.Errorf("parse frontmatter: %w", err)
		}
	}

	if fm.Name == "" {
		fm.Name = name
	}
	if err := validateSkillName(name, fm.Name); err != nil {
		return nil, err
	}

	deprecatedFields := deprecatedFrontmatterFields(fm)
	if len(deprecatedFields) > 0 {
		logger.Warn("Skill uses deprecated frontmatter fields", logger.Fields{
			"skill":  fm.Name,
			"fields": strings.Join(deprecatedFields, ", "),
			"path":   location,
		})
	}

	// 提取模板内容（frontmatter 之后的部分）
	templateContent := ""
	if len(parts) >= 3 {
		templateContent = strings.TrimSpace(parts[2])
	}
	if containsLegacyTemplateSyntax(templateContent) {
		deprecatedFields = appendDeprecatedField(deprecatedFields, "template-syntax")
		logger.Warn("Skill content uses deprecated template syntax", logger.Fields{
			"skill": fm.Name,
			"path":  location,
		})
	}

	role, tags := roleAndTagsFromMetadata(fm)
	compatibility := normalizeCompatibility(fm.Compatibility)

	return &Skill{
		Name:             fm.Name,
		Description:      fm.Description,
		Location:         location,
		License:          fm.License,
		Compatibility:    compatibility,
		Metadata:         fm.Metadata,
		Role:             role,
		Tags:             tags,
		Parameters:       fm.Parameters,
		DeprecatedFields: deprecatedFields,
		Content:          templateContent,
	}, nil
}

func validateSkillName(dirName string, skillName string) error {
	if !skillNamePattern.MatchString(skillName) || len(skillName) > 64 {
		return fmt.Errorf("invalid skill name %q: use kebab-case lowercase letters, digits, and single hyphens", skillName)
	}
	if dirName != skillName {
		return fmt.Errorf("skill name %q must match directory name %q", skillName, dirName)
	}
	return nil
}

func deprecatedFrontmatterFields(fm skillFrontmatter) []string {
	var fields []string
	if fm.Requires.Role != "" {
		fields = append(fields, "requires.role")
	}
	if len(fm.Tags) > 0 {
		fields = append(fields, "tags")
	}
	if len(fm.Parameters) > 0 {
		fields = append(fields, "parameters")
	}
	return fields
}

func appendDeprecatedField(fields []string, field string) []string {
	for _, existing := range fields {
		if existing == field {
			return fields
		}
	}
	return append(fields, field)
}

func containsLegacyTemplateSyntax(content string) bool {
	return strings.Contains(content, "{{") && strings.Contains(content, "}}")
}

func normalizeCompatibility(raw interface{}) []string {
	switch v := raw.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []string{strings.TrimSpace(v)}
	case []interface{}:
		values := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				values = append(values, strings.TrimSpace(s))
			}
		}
		return values
	case []string:
		values := make([]string, 0, len(v))
		for _, item := range v {
			if strings.TrimSpace(item) != "" {
				values = append(values, strings.TrimSpace(item))
			}
		}
		return values
	default:
		return nil
	}
}

func roleAndTagsFromMetadata(fm skillFrontmatter) (models.Role, []string) {
	role := models.Role(fm.Requires.Role)
	tags := fm.Tags
	evoduck, ok := nestedMap(fm.Metadata, "evoduck")
	if !ok {
		return role, tags
	}
	if v, ok := evoduck["role"].(string); ok && strings.TrimSpace(v) != "" {
		role = models.Role(strings.TrimSpace(v))
	}
	if parsedTags := stringSlice(evoduck["tags"]); len(parsedTags) > 0 {
		tags = parsedTags
	}
	return role, tags
}

func nestedMap(source map[string]interface{}, key string) (map[string]interface{}, bool) {
	if source == nil {
		return nil, false
	}
	value, ok := source[key]
	if !ok {
		return nil, false
	}
	result, ok := value.(map[string]interface{})
	return result, ok
}

func stringSlice(raw interface{}) []string {
	switch v := raw.(type) {
	case []interface{}:
		values := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				values = append(values, strings.TrimSpace(s))
			}
		}
		return values
	case []string:
		values := make([]string, 0, len(v))
		for _, item := range v {
			if strings.TrimSpace(item) != "" {
				values = append(values, strings.TrimSpace(item))
			}
		}
		return values
	default:
		return nil
	}
}
