package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chawuciren/evoduck/internal/skill"
	"github.com/chawuciren/evoduck/pkg/models"
)

func writeSkillFixture(t *testing.T, root, dirName, content string) {
	t.Helper()
	dir := filepath.Join(root, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}
}

func loadTestSkills(t *testing.T) *skill.Loader {
	t.Helper()
	tempDir := t.TempDir()
	agentSkillsDir := filepath.Join(tempDir, "skills")
	if err := os.MkdirAll(agentSkillsDir, 0o755); err != nil {
		t.Fatalf("mkdir agent skills dir: %v", err)
	}

	writeSkillFixture(t, agentSkillsDir, "reviewer", `---
name: reviewer
description: Review code changes for bugs and regressions
license: MIT
compatibility: evoduck
metadata:
  evoduck:
    tags: [review, quality]
---
Check the diff carefully for bugs and regressions.`)

	writeSkillFixture(t, agentSkillsDir, "admin-only", `---
name: admin-only
description: Sensitive admin workflow
metadata:
  evoduck:
    role: admin
---
Handle privileged operations only.`)

	loader := skill.NewLoader(tempDir, filepath.Join(tempDir, "shared-skills"))
	if err := loader.LoadAll(); err != nil {
		t.Fatalf("load skills: %v", err)
	}
	return loader
}

func TestSkillListToolFiltersByRole(t *testing.T) {
	loader := loadTestSkills(t)
	tool := NewSkillListTool(loader, models.RoleEmployee)

	result, err := tool.Execute(nil)
	if err != nil {
		t.Fatalf("skill_list execute: %v", err)
	}

	if !strings.Contains(result, "`reviewer`") {
		t.Fatalf("expected visible reviewer skill, got: %s", result)
	}
	if strings.Contains(result, "admin-only") {
		t.Fatalf("did not expect admin-only skill for employee role, got: %s", result)
	}
}

func TestSkillDetailAndUseTool(t *testing.T) {
	loader := loadTestSkills(t)
	detailTool := NewSkillDetailTool(loader, models.RoleEmployee)
	useTool := NewSkillUseTool(loader, models.RoleEmployee)

	detailResult, err := detailTool.Execute(map[string]interface{}{"name": "reviewer"})
	if err != nil {
		t.Fatalf("skill_detail execute: %v", err)
	}
	if !strings.Contains(detailResult, "License: MIT") || !strings.Contains(detailResult, "Compatibility: evoduck") || !strings.Contains(detailResult, "Content Preview") {
		t.Fatalf("expected detailed skill output, got: %s", detailResult)
	}

	useResult, err := useTool.Execute(map[string]interface{}{
		"name": "reviewer",
	})
	if err != nil {
		t.Fatalf("skill_use execute: %v", err)
	}
	if !strings.Contains(useResult, "Check the diff carefully") || !strings.Contains(useResult, "## Instructions") {
		t.Fatalf("expected rendered skill instructions, got: %s", useResult)
	}
}

func TestSkillDetailShowsDeprecatedFields(t *testing.T) {
	tempDir := t.TempDir()
	agentSkillsDir := filepath.Join(tempDir, "skills")
	writeSkillFixture(t, agentSkillsDir, "legacy-skill", `---
name: legacy-skill
description: Legacy workflow
requires:
  role: admin
tags: [legacy]
parameters:
  - name: focus
    description: Main focus
---
Legacy instructions.`)

	loader := skill.NewLoader(tempDir, filepath.Join(tempDir, "shared-skills"))
	if err := loader.LoadAll(); err != nil {
		t.Fatalf("load skills: %v", err)
	}

	detailTool := NewSkillDetailTool(loader, models.RoleAdmin)
	detailResult, err := detailTool.Execute(map[string]interface{}{"name": "legacy-skill"})
	if err != nil {
		t.Fatalf("skill_detail execute: %v", err)
	}
	for _, expected := range []string{"Deprecation Warnings", "requires.role", "tags", "parameters"} {
		if !strings.Contains(detailResult, expected) {
			t.Fatalf("expected %q in detail output, got: %s", expected, detailResult)
		}
	}
}

func TestSkillUseDoesNotRenderLegacyTemplates(t *testing.T) {
	tempDir := t.TempDir()
	agentSkillsDir := filepath.Join(tempDir, "skills")
	writeSkillFixture(t, agentSkillsDir, "legacy-template", `---
name: legacy-template
description: Legacy template workflow
parameters:
  - name: focus
    description: Main focus
    default: correctness
---
Focus on {{.focus}}.`)

	loader := skill.NewLoader(tempDir, filepath.Join(tempDir, "shared-skills"))
	if err := loader.LoadAll(); err != nil {
		t.Fatalf("load skills: %v", err)
	}

	useTool := NewSkillUseTool(loader, models.RoleEmployee)
	result, err := useTool.Execute(map[string]interface{}{
		"name":   "legacy-template",
		"params": map[string]interface{}{"focus": "migrations"},
	})
	if err != nil {
		t.Fatalf("skill_use execute: %v", err)
	}
	if strings.Contains(result, "migrations") {
		t.Fatalf("did not expect legacy template params to render, got: %s", result)
	}
	for _, expected := range []string{"{{.focus}}", "parameters", "template-syntax"} {
		if !strings.Contains(result, expected) {
			t.Fatalf("expected %q in skill output, got: %s", expected, result)
		}
	}
}

func TestSkillUseReplacesBaseDirPlaceholder(t *testing.T) {
	tempDir := t.TempDir()
	agentSkillsDir := filepath.Join(tempDir, "skills")
	writeSkillFixture(t, agentSkillsDir, "with-supporting-files", `---
name: with-supporting-files
description: Uses supporting files
---
Read {baseDir}/examples/report.md before writing the final report.`)

	loader := skill.NewLoader(tempDir, filepath.Join(tempDir, "shared-skills"))
	if err := loader.LoadAll(); err != nil {
		t.Fatalf("load skills: %v", err)
	}

	useTool := NewSkillUseTool(loader, models.RoleEmployee)
	result, err := useTool.Execute(map[string]interface{}{"name": "with-supporting-files"})
	if err != nil {
		t.Fatalf("skill_use execute: %v", err)
	}
	expectedBaseDir := filepath.ToSlash(filepath.Join(agentSkillsDir, "with-supporting-files"))
	if !strings.Contains(result, expectedBaseDir+"/examples/report.md") {
		t.Fatalf("expected baseDir replacement with %q, got: %s", expectedBaseDir, result)
	}
	if strings.Contains(result, "{baseDir}") {
		t.Fatalf("did not expect unresolved baseDir placeholder, got: %s", result)
	}
}

func TestSkillDetailShowsBaseDir(t *testing.T) {
	tempDir := t.TempDir()
	agentSkillsDir := filepath.Join(tempDir, "skills")
	writeSkillFixture(t, agentSkillsDir, "with-base-dir", `---
name: with-base-dir
description: Shows base dir
---
Instructions.`)

	loader := skill.NewLoader(tempDir, filepath.Join(tempDir, "shared-skills"))
	if err := loader.LoadAll(); err != nil {
		t.Fatalf("load skills: %v", err)
	}

	detailTool := NewSkillDetailTool(loader, models.RoleEmployee)
	result, err := detailTool.Execute(map[string]interface{}{"name": "with-base-dir"})
	if err != nil {
		t.Fatalf("skill_detail execute: %v", err)
	}
	if !strings.Contains(result, "BaseDir:") || !strings.Contains(result, filepath.ToSlash(filepath.Join(agentSkillsDir, "with-base-dir"))) {
		t.Fatalf("expected baseDir metadata, got: %s", result)
	}
}

func TestLegacySkillToolDelegatesToUse(t *testing.T) {
	loader := loadTestSkills(t)
	tool := NewSkillTool(loader, models.RoleEmployee)

	result, err := tool.Execute(map[string]interface{}{"name": "reviewer"})
	if err != nil {
		t.Fatalf("legacy skill execute: %v", err)
	}
	if !strings.Contains(result, "## Instructions") {
		t.Fatalf("expected skill instructions, got: %s", result)
	}
}

func TestSkillLoaderReloadRemovesDeletedSkills(t *testing.T) {
	tempDir := t.TempDir()
	agentSkillsDir := filepath.Join(tempDir, "skills")
	writeSkillFixture(t, agentSkillsDir, "temporary", `---
name: temporary
description: Temporary workflow
---
Temporary instructions.`)

	loader := skill.NewLoader(tempDir, filepath.Join(tempDir, "shared-skills"))
	if err := loader.LoadAll(); err != nil {
		t.Fatalf("initial load skills: %v", err)
	}
	if s, err := loader.Get("temporary"); err != nil || s == nil {
		t.Fatalf("expected temporary skill after initial load, skill=%v err=%v", s, err)
	}

	if err := os.RemoveAll(filepath.Join(agentSkillsDir, "temporary")); err != nil {
		t.Fatalf("remove temporary skill: %v", err)
	}
	if err := loader.LoadAll(); err != nil {
		t.Fatalf("reload skills: %v", err)
	}
	if s, err := loader.Get("temporary"); err != nil || s != nil {
		t.Fatalf("expected temporary skill to be removed after reload, skill=%v err=%v", s, err)
	}
}
