package builtin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureBuiltinSkillsCopiesAllEmbeddedSkills(t *testing.T) {
	dir := t.TempDir()

	if err := EnsureBuiltinSkills(dir); err != nil {
		t.Fatalf("EnsureBuiltinSkills returned error: %v", err)
	}

	for _, name := range []string{"skill-creator", "evoduck-self-configuration"} {
		path := filepath.Join(dir, name, "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected builtin skill %s to be copied: %v", name, err)
		}
		if !strings.Contains(string(data), "name: "+name) {
			t.Fatalf("expected copied skill %s to contain matching frontmatter", name)
		}
		frontmatter := string(data)
		if idx := strings.Index(frontmatter, "---\n"); idx >= 0 {
			frontmatter = frontmatter[idx+4:]
		}
		if idx := strings.Index(frontmatter, "---"); idx >= 0 {
			frontmatter = frontmatter[:idx]
		}
		if strings.Contains(frontmatter, "parameters:") || strings.Contains(frontmatter, "requires:") {
			t.Fatalf("expected copied skill %s to use standardized frontmatter", name)
		}

		manifestPath := filepath.Join(dir, name, "skill.json")
		manifest, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatalf("expected builtin skill %s manifest to be copied: %v", name, err)
		}
		if !strings.Contains(string(manifest), "\"name\": \""+name+"\"") {
			t.Fatalf("expected copied skill %s manifest to contain matching name", name)
		}
	}
}

func TestEnsureBuiltinSkillsDoesNotOverwriteExistingFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "evoduck-self-configuration", "SKILL.md")
	custom := []byte("custom user skill")

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(path, custom, 0o644); err != nil {
		t.Fatalf("write custom skill: %v", err)
	}

	if err := EnsureBuiltinSkills(dir); err != nil {
		t.Fatalf("EnsureBuiltinSkills returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read custom skill: %v", err)
	}
	if string(data) != string(custom) {
		t.Fatalf("expected existing skill to be preserved, got %q", string(data))
	}
}
