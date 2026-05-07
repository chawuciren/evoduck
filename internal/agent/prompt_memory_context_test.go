package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/internal/skill"
	"github.com/chawuciren/evoduck/internal/tools"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
)

func TestPromptIncludesMemoryInventoryAndFileContext(t *testing.T) {
	workspace := t.TempDir()
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("agent rules"), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	userDir := filepath.Join(dataDir, "users", "agent-test_user_alice")
	if err := os.MkdirAll(filepath.Join(userDir, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir user memory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "USER.md"), []byte("Alice profile"), 0o644); err != nil {
		t.Fatalf("write USER.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "MEMORY.md"), []byte("Alice prefers concise answers"), 0o644); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}
	dailyName := time.Now().Format("2006-01-02") + ".md"
	if err := os.WriteFile(filepath.Join(userDir, "memory", dailyName), []byte("daily detail"), 0o644); err != nil {
		t.Fatalf("write daily memory: %v", err)
	}

	pb := NewPromptBuilder(workspace, "agent-test", dataDir, tools.NewRegistry(), skill.NewLoader(workspace, workspace))
	pb.SetUserIsolation(config.UserIsolationConfig{AutoCreate: false})
	sess := session.NewSession("web:alice", "session-1", nil)
	sess.Append(models.Message{Role: "user", Content: "hello"})

	messages, err := pb.Build(context.Background(), sess, "hello")
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	system := messages[0].Content
	for _, want := range []string{"<!-- MEMORY_INVENTORY", "Agent memory files:", "User memory files:", "Daily notes:", "<!-- FILE_CONTEXT scope=\"agent\" path=\"AGENTS.md\"", "full_path=", "lines=\"1-1\"", "<!-- FILE_CONTEXT scope=\"user\" path=\"USER.md\"", "<!-- FILE_CONTEXT scope=\"user\" path=\"MEMORY.md\""} {
		if !strings.Contains(system, want) {
			t.Fatalf("expected system prompt to contain %q:\n%s", want, system)
		}
	}
	if !strings.Contains(system, "<!-- FILE_CONTEXT scope=\"user_daily\" path=\"memory/"+dailyName+"\"") {
		t.Fatalf("expected session start to include today daily note FILE_CONTEXT:\n%s", system)
	}
}

func TestPromptDoesNotInjectDailyNotesOnNormalTurn(t *testing.T) {
	workspace := t.TempDir()
	dataDir := t.TempDir()
	userDir := filepath.Join(dataDir, "users", "agent-test_user_alice")
	if err := os.MkdirAll(filepath.Join(userDir, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir user memory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "USER.md"), []byte("Alice profile"), 0o644); err != nil {
		t.Fatalf("write USER.md: %v", err)
	}
	dailyName := time.Now().Format("2006-01-02") + ".md"
	if err := os.WriteFile(filepath.Join(userDir, "memory", dailyName), []byte("daily detail should not be injected"), 0o644); err != nil {
		t.Fatalf("write daily memory: %v", err)
	}

	pb := NewPromptBuilder(workspace, "agent-test", dataDir, tools.NewRegistry(), skill.NewLoader(workspace, workspace))
	pb.SetUserIsolation(config.UserIsolationConfig{AutoCreate: false})
	sess := session.NewSession("web:alice", "session-1", nil)
	sess.Append(models.Message{Role: "user", Content: "first"})
	sess.Append(models.Message{Role: "assistant", Content: "reply"})
	sess.Append(models.Message{Role: "user", Content: "next"})

	messages, err := pb.Build(context.Background(), sess, "next")
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	system := messages[0].Content
	if strings.Contains(system, "daily detail should not be injected") {
		t.Fatalf("daily note body should not be injected on normal turn:\n%s", system)
	}
	if !strings.Contains(system, "Daily notes:\n- memory/"+dailyName) {
		t.Fatalf("daily note should still appear in inventory:\n%s", system)
	}
}

func TestPromptUsesActorUserIDForChannelPrivateMemory(t *testing.T) {
	workspace := t.TempDir()
	dataDir := t.TempDir()
	aliceDir := filepath.Join(dataDir, "users", "agent-test_user_alice")
	bobDir := filepath.Join(dataDir, "users", "agent-test_user_bob")
	if err := os.MkdirAll(aliceDir, 0o755); err != nil {
		t.Fatalf("mkdir alice dir: %v", err)
	}
	if err := os.MkdirAll(bobDir, 0o755); err != nil {
		t.Fatalf("mkdir bob dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(aliceDir, "USER.md"), []byte("Alice private profile"), 0o644); err != nil {
		t.Fatalf("write alice USER.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bobDir, "USER.md"), []byte("Bob private profile"), 0o644); err != nil {
		t.Fatalf("write bob USER.md: %v", err)
	}

	pb := NewPromptBuilder(workspace, "agent-test", dataDir, tools.NewRegistry(), skill.NewLoader(workspace, workspace))
	pb.SetUserIsolation(config.UserIsolationConfig{AutoCreate: false})
	sess := session.NewSession("wecom:group-thread", "session-1", nil)
	sess.SetMetadataValue("chat_type", "group")
	sess.SetMetadataValue("actor_user_id", "alice")
	sess.Append(models.Message{Role: "user", Content: "group question"})

	messages, err := pb.Build(context.Background(), sess, "group question")
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	system := messages[0].Content
	if !strings.Contains(system, "Alice private profile") {
		t.Fatalf("expected actor user's private memory to be injected:\n%s", system)
	}
	if strings.Contains(system, "Bob private profile") || strings.Contains(system, "group-thread") {
		t.Fatalf("should not inject session-key/group or other users' private memory:\n%s", system)
	}
}

func TestPromptSkipsPrivateMemoryForScheduleSession(t *testing.T) {
	workspace := t.TempDir()
	dataDir := t.TempDir()
	userDir := filepath.Join(dataDir, "users", "agent-test_user_alice")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("mkdir user dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "USER.md"), []byte("Alice private profile"), 0o644); err != nil {
		t.Fatalf("write USER.md: %v", err)
	}

	pb := NewPromptBuilder(workspace, "agent-test", dataDir, tools.NewRegistry(), skill.NewLoader(workspace, workspace))
	pb.SetUserIsolation(config.UserIsolationConfig{AutoCreate: false})
	sess := session.NewSession("agent:agent-test:user:alice:schedule:task-1", "session-1", nil)
	sess.SetMetadataValue("session_kind", "schedule")
	sess.SetMetadataValue("actor_user_id", "alice")
	sess.Append(models.Message{Role: "user", Content: "scheduled task"})

	messages, err := pb.Build(context.Background(), sess, "scheduled task")
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	if strings.Contains(messages[0].Content, "Alice private profile") {
		t.Fatalf("schedule session should not inject private user memory:\n%s", messages[0].Content)
	}
}
