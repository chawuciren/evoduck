package profile

import (
	"strings"
	"testing"
)

func TestDefaultHourlyMemoryCurationPromptIncludesArtifactRules(t *testing.T) {
	prompt := DefaultHourlyMemoryCurationPrompt()
	if strings.TrimSpace(prompt) == "" {
		t.Fatal("expected hourly prompt to be non-empty")
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
			t.Fatalf("expected hourly prompt to contain %q", needle)
		}
	}
	if strings.Contains(prompt, "Shared artifacts:") {
		t.Fatal("expected hourly prompt not to advertise shared artifact section")
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
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("expected daily prompt to contain %q", needle)
		}
	}
}
