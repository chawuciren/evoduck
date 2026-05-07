package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/internal/skill"
	"github.com/chawuciren/evoduck/internal/tools"
	"github.com/chawuciren/evoduck/pkg/models"
)

func TestBuildIncludesScheduleExecutionContext(t *testing.T) {
	workspace := t.TempDir()
	loader := skill.NewLoader(workspace, filepath.Join(workspace, "shared-skills"))
	pb := NewPromptBuilder(workspace, "agent-test", workspace, tools.NewRegistry(), loader)
	sess := session.NewSession("agent:agent-test:user:alice:schedule:task-1", "session-1", nil)
	sess.SetMetadataValue("session_kind", "schedule")
	sess.SetMetadataValue("origin_session_key", "agent:agent-test:user:alice:ws")
	sess.SetMetadataValue("agent_id", "agent-test")
	sess.SetMetadataValue("user_id", "alice")
	sess.Append(models.Message{Role: "user", Content: "check weather"})

	messages, err := pb.Build(context.Background(), sess, "check weather")
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	if len(messages) == 0 {
		t.Fatal("expected prompt messages")
	}
	systemContent := messages[0].Content
	for _, want := range []string{
		"## Scheduled Task Context",
		"origin_session_key",
		"agent:agent-test:user:alice:ws",
		"may call `sessions_send`",
		"can optionally include a `media` array",
		"omit `session_key`; it defaults to the current session",
		"Do not send a message back unless the task itself implies user-facing delivery.",
	} {
		if !strings.Contains(systemContent, want) {
			t.Fatalf("expected schedule context to include %q", want)
		}
	}
}
