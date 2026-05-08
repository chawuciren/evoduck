package profile

import (
	"strings"
	"testing"
)

func TestDefaultHourlyMemoryCurationPromptIncludesArtifactRules(t *testing.T) {
	prompt := DefaultHourlyMemoryCurationPrompt()
	if strings.TrimSpace(prompt) == "" {
		t.Fatal("expected memory-curation prompt to be non-empty")
	}
	for _, needle := range []string{
		"User-side files and their roles:",
		"Source-agent bootstrap files and their roles:",
		"memory/YYYY-MM-DD.md",
		"MEMORY.md",
		"USER.md",
		"AGENTS.md",
		"SOUL.md",
		"TOOLS.md",
		"IDENTITY.md",
		"HEARTBEAT.md",
		"BOOTSTRAP.md",
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("expected memory-curation prompt to contain %q", needle)
		}
	}
	if strings.Contains(prompt, "Shared artifacts:") {
		t.Fatal("expected memory-curation prompt not to advertise shared artifact section")
	}
	for _, needle := range []string{
		"Prefer memory_search, memory_read, memory_write, and memory_edit",
		"Use file tools only as a fallback",
		"workspace and authorized directories",
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("expected memory-curation prompt to contain %q", needle)
		}
	}
}

func TestDefaultDailyExperienceCurationPromptIncludesArtifactRules(t *testing.T) {
	prompt := DefaultDailyExperienceCurationPrompt()
	if strings.TrimSpace(prompt) == "" {
		t.Fatal("expected daily prompt to be non-empty")
	}
	for _, needle := range []string{
		"User-side files and their roles:",
		"Source-agent bootstrap files and their roles:",
		"Shared artifacts:",
		"Shared knowledge:",
		"Shared skills:",
		"SKILL.md",
		"Prefer memory_search, memory_read, memory_write, and memory_edit",
		"Use file tools only as a fallback",
		"workspace and authorized directories",
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("expected daily prompt to contain %q", needle)
		}
	}
}
