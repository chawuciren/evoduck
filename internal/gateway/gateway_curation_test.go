package gateway

import (
	"strings"
	"testing"

	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/pkg/models"
)

func TestBuildSessionResetCurationPromptPrefersMemoryTools(t *testing.T) {
	sess := session.NewSession("agent:source:user:alice:ws", "session-1", nil)
	sess.SetUserID("alice")
	sess.SetMetadataValue("actor_user_id", "alice")
	sess.Append(models.Message{Role: "user", Content: "Remember that I prefer concise replies."})

	prompt := buildSessionResetCurationPrompt("source", sess, "alice")
	for _, want := range []string{
		"Prefer memory_search, memory_read, memory_write, and memory_edit",
		"Use file tools only as a fallback",
		"workspace and authorized directories",
		"target_user_id: alice",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected prompt to contain %q\nfull prompt:\n%s", want, prompt)
		}
	}
}
