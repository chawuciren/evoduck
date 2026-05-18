package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/internal/skill"
	"github.com/chawuciren/evoduck/internal/tools"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
)

func TestBuildIncludesCommandExecutionRouting(t *testing.T) {
	workspace := t.TempDir()
	loader := skill.NewLoader(workspace, filepath.Join(workspace, "shared-skills"))
	registry := tools.NewRegistry()
	registry.Register(tools.NewExecTool(tools.NewAgentPermissions(models.RoleAdmin, workspace, config.AgentPermissionConfig{}), nil))
	registry.Register(tools.NewProcessTool(tools.NewAgentPermissions(models.RoleAdmin, workspace, config.AgentPermissionConfig{}), nil))
	registry.Register(tools.NewSleepTool())

	pb := NewPromptBuilder(workspace, "agent-test", workspace, registry, loader)
	sess := session.NewSession("agent:agent-test:user:alice:ws", "session-1", nil)
	sess.Append(models.Message{Role: "user", Content: "run something"})

	messages, err := pb.Build(context.Background(), sess, "run something")
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	if len(messages) == 0 {
		t.Fatal("expected prompt messages")
	}
	systemContent := messages[0].Content
	for _, want := range []string{
		"## Command Execution Routing",
		"Use exec only for very short, one-shot, non-interactive commands",
		"Use process for long-running, blocking, background, or interactive commands",
		"If a command may take noticeable time, might timeout, may ask for follow-up input, or you may need to inspect logs later, prefer process over exec",
		"If the task itself is longer-running, more expensive, needs broader research, or involves multiple independent time-consuming tasks, prefer a subagent instead of keeping all work in the current agent",
		"For multiple independent long-running tasks, prefer launching subagents in parallel rather than serializing them in one agent",
		"Use sleep for explicit delays between tool calls",
		"### exec",
		"### process",
		"### sleep",
	} {
		if !strings.Contains(systemContent, want) {
			t.Fatalf("expected prompt to include %q", want)
		}
	}
}
