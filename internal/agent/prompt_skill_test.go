package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chawuciren/evoduck/internal/skill"
	"github.com/chawuciren/evoduck/internal/tools"
)

func TestBuildSkillListUsesLightweightIndex(t *testing.T) {
	tempDir := t.TempDir()
	skillsRoot := filepath.Join(tempDir, "skills")
	if err := os.MkdirAll(filepath.Join(skillsRoot, "reviewer"), 0o755); err != nil {
		t.Fatalf("mkdir skills root: %v", err)
	}
	content := `---
name: reviewer
description: Review code changes for bugs and regressions
---
Detailed instructions here.`
	if err := os.WriteFile(filepath.Join(skillsRoot, "reviewer", "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}

	loader := skill.NewLoader(tempDir, filepath.Join(tempDir, "shared-skills"))
	if err := loader.LoadAll(); err != nil {
		t.Fatalf("load skills: %v", err)
	}

	pb := NewPromptBuilder(tempDir, "agent-test", tempDir, tools.NewRegistry(), loader)
	section := pb.buildSkillList()

	if !strings.Contains(section, "skill_list") || !strings.Contains(section, "skill_detail") || !strings.Contains(section, "skill_use") {
		t.Fatalf("expected lightweight skill tool guidance, got: %s", section)
	}
	if !strings.Contains(section, "- `reviewer`: Review code changes for bugs and regressions") {
		t.Fatalf("expected skill summary entry, got: %s", section)
	}
	if strings.Contains(section, "Location:") || strings.Contains(section, "Detailed instructions here") {
		t.Fatalf("expected no detailed skill injection, got: %s", section)
	}
}
