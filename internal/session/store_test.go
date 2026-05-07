package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chawuciren/evoduck/pkg/models"
)

func TestSessionMetadataPersistsSeparately(t *testing.T) {
	store, err := NewJSONLStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	sess := NewSession("agent:agent-test:user:admin:schedule:task-1", "session-1", store)
	sess.SetMetadataValue("session_kind", "schedule")
	sess.SetMetadataValue("memory_policy", "ignore")

	reloaded := NewSession("agent:agent-test:user:admin:schedule:task-1", "session-1", store)
	if got := reloaded.GetMetadataValue("session_kind"); got != "schedule" {
		t.Fatalf("expected session_kind schedule, got %q", got)
	}
	if got := reloaded.GetMetadataValue("memory_policy"); got != "ignore" {
		t.Fatalf("expected memory_policy ignore, got %q", got)
	}
}

func TestDeleteRemovesMetadataWithoutMessageFile(t *testing.T) {
	store, err := NewJSONLStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	key := "agent:agent-test:user:admin:schedule:task-2"
	if err := store.SaveMetadata(key, map[string]string{"session_kind": "schedule"}); err != nil {
		t.Fatalf("save metadata: %v", err)
	}
	if err := store.Delete(key); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	metadata, err := store.LoadMetadata(key)
	if err != nil {
		t.Fatalf("load metadata after delete: %v", err)
	}
	if len(metadata) != 0 {
		t.Fatalf("expected metadata to be deleted, got %#v", metadata)
	}
}

func TestManagerListIncludesPersistedSessionsNotYetLoaded(t *testing.T) {
	store, err := NewJSONLStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	persisted := NewSession("wecom:alice", "wecom:alice", store)
	persisted.Append(models.Message{Role: "user", Content: "hello"})
	persisted.SetMetadataValue("channel", "wecom")

	mgr := NewManager(store, 7*24*time.Hour)
	items := mgr.List()
	if len(items) != 1 {
		t.Fatalf("expected 1 listed session, got %d", len(items))
	}
	if items[0].Key != "wecom:alice" {
		t.Fatalf("expected persisted wecom session to be listed, got %q", items[0].Key)
	}
	if items[0].MessageCount != 1 {
		t.Fatalf("expected message count 1, got %d", items[0].MessageCount)
	}
}

func TestLoadHydratesStoredMediaPathFromURL(t *testing.T) {
	baseDir := t.TempDir()
	store, err := NewJSONLStore(filepath.Join(baseDir, "sessions"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	mediaDir := filepath.Join(baseDir, "media")
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		t.Fatalf("mkdir media: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mediaDir, "med_test.png"), []byte("png-bytes"), 0o644); err != nil {
		t.Fatalf("write media bytes: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mediaDir, "med_test.json"), []byte(`{"id":"med_test","name":"upload-test.png","mime_type":"image/png","size":9,"stored_name":"med_test.png"}`), 0o644); err != nil {
		t.Fatalf("write media metadata: %v", err)
	}
	message := models.Message{
		Role:    "user",
		Content: "see image",
		Media: []models.OutgoingMedia{{
			Type: "image",
			Name: "upload-test.png",
			URL:  "/media/med_test",
		}},
	}
	if err := store.Append("wecom:alice", message); err != nil {
		t.Fatalf("append message: %v", err)
	}

	loaded, err := store.Load("wecom:alice")
	if err != nil {
		t.Fatalf("load messages: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 message, got %d", len(loaded))
	}
	if got := loaded[0].Media[0].Path; got != filepath.Join(mediaDir, "med_test.png") {
		t.Fatalf("expected hydrated media path, got %q", got)
	}
	if got := loaded[0].Media[0].MimeType; got != "image/png" {
		t.Fatalf("expected hydrated mime type, got %q", got)
	}
	if got := loaded[0].Media[0].FileSize; got != 9 {
		t.Fatalf("expected hydrated file size, got %d", got)
	}
}
