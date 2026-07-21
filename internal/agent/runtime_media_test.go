package agent

import (
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chawuciren/evoduck/internal/mediautil"
	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/pkg/models"
)

func TestNormalizeToolResultStoresBrowserScreenshotMedia(t *testing.T) {
	runtime := NewRuntime("agent-media-test", t.TempDir(), nil, nil, nil, models.RoleAdmin, nil, true, nil)
	store, err := mediautil.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new media store: %v", err)
	}
	runtime.SetMediaStore(store)

	payload, err := json.Marshal(browserScreenshotToolResult{
		Summary: "Full-page screenshot captured (11 bytes)",
		Media: []models.OutgoingMedia{{
			Type:     "image",
			Name:     "browser-fullpage-screenshot.png",
			MimeType: "image/png",
			Data:     base64.StdEncoding.EncodeToString([]byte("png-bytes")),
			FileSize: int64(len("png-bytes")),
		}},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	env, err := runtime.normalizeToolResult("browser_screenshot", string(payload))
	if err != nil {
		t.Fatalf("normalize tool result: %v", err)
	}
	if !strings.Contains(env.Content, "Full-page screenshot captured (11 bytes)") {
		t.Fatalf("unexpected summary: %q", env.Content)
	}
	if !strings.Contains(env.Content, "Path: ") || !strings.Contains(env.Content, "Media URL: ") {
		t.Fatalf("expected screenshot metadata in summary, got %q", env.Content)
	}
	if !strings.Contains(env.Content, "Original size: 9 bytes") || !strings.Contains(env.Content, "Final size: 9 bytes") {
		t.Fatalf("expected screenshot size metadata in summary, got %q", env.Content)
	}
	if !strings.Contains(env.Content, "Compressed: false") {
		t.Fatalf("expected screenshot compression flag in summary, got %q", env.Content)
	}
	if len(env.Media) != 1 {
		t.Fatalf("expected one media item, got %d", len(env.Media))
	}
	if env.Media[0].Data != "" {
		t.Fatal("expected screenshot data to be cleared after storage")
	}
	if !strings.HasPrefix(env.Media[0].URL, "/media/") {
		t.Fatalf("expected stored media url, got %q", env.Media[0].URL)
	}
	if env.Media[0].Path == "" {
		t.Fatal("expected stored media path to be populated")
	}
	if env.Media[0].FileSize != int64(len("png-bytes")) {
		t.Fatalf("expected stored file size, got %d", env.Media[0].FileSize)
	}
}

func TestAppendToolResultMessagePersistsToolMedia(t *testing.T) {
	runtime := NewRuntime("agent-media-test", t.TempDir(), nil, nil, nil, models.RoleAdmin, nil, true, nil)
	storeBase := t.TempDir()
	store, err := mediautil.NewStore(storeBase)
	if err != nil {
		t.Fatalf("new media store: %v", err)
	}
	runtime.SetMediaStore(store)
	sess := session.NewSession("webchat:test-user", "tool-media-session", nil)

	payload, err := json.Marshal(browserScreenshotToolResult{
		Summary: "Screenshot captured (9 bytes)",
		Media: []models.OutgoingMedia{{
			Type:     "image",
			Name:     "browser-screenshot.png",
			MimeType: "image/png",
			Data:     base64.StdEncoding.EncodeToString([]byte("png-data")),
			FileSize: int64(len("png-data")),
		}},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	if err := runtime.appendToolResultMessage(context.Background(), sess, models.ToolCall{
		ID:       "tool-call-1",
		Function: models.ToolCallFunction{Name: "browser_screenshot"},
	}, string(payload)); err != nil {
		t.Fatalf("append tool result message: %v", err)
	}

	messages := sess.GetMessages()
	if len(messages) != 1 {
		t.Fatalf("expected one persisted message, got %d", len(messages))
	}
	msg := messages[0]
	if msg.Role != "tool" {
		t.Fatalf("expected tool role, got %q", msg.Role)
	}
	if !strings.Contains(msg.Content, "Screenshot captured (9 bytes)") {
		t.Fatalf("unexpected tool content: %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "Path: ") || !strings.Contains(msg.Content, "Media URL: ") {
		t.Fatalf("expected screenshot metadata in tool content, got %q", msg.Content)
	}
	if len(msg.Media) != 1 {
		t.Fatalf("expected one media item, got %d", len(msg.Media))
	}
	if msg.Media[0].Data != "" {
		t.Fatal("expected persisted tool media data to be empty")
	}
	if msg.Media[0].Path == "" || !strings.HasPrefix(msg.Media[0].URL, "/media/") {
		t.Fatalf("expected stored media reference, got %+v", msg.Media[0])
	}
	if filepath.Dir(msg.Media[0].Path) != filepath.Join(storeBase, "media") {
		t.Fatalf("expected media path under shared store, got %q", msg.Media[0].Path)
	}
}

func TestToolResultObserverTextSanitizesBrowserScreenshotPayload(t *testing.T) {
	runtime := NewRuntime("agent-media-test", t.TempDir(), nil, nil, nil, models.RoleAdmin, nil, true, nil)
	payload, err := json.Marshal(browserScreenshotToolResult{
		Summary: "Element screenshot captured for #app (8 bytes)",
		Media: []models.OutgoingMedia{{
			Type:     "image",
			Name:     "browser-element-screenshot.png",
			MimeType: "image/png",
			Data:     base64.StdEncoding.EncodeToString([]byte("png-data")),
		}},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	got := runtime.toolResultObserverText("browser_screenshot", string(payload))
	if !strings.Contains(got, "Element screenshot captured for #app (8 bytes)") {
		t.Fatalf("unexpected observer text: %q", got)
	}
	if strings.Contains(got, "png-data") {
		t.Fatalf("expected sanitized observer text, got %q", got)
	}
	if !strings.Contains(got, "Compressed: false") {
		t.Fatalf("expected screenshot metadata in observer text, got %q", got)
	}
}

func TestAppendToolResultMessageCondensesOversizedContent(t *testing.T) {
	runtime := NewRuntime("agent-media-test", t.TempDir(), nil, nil, nil, models.RoleAdmin, nil, true, nil)
	runtime.SetToolResultCondenseLimit(64)
	sess := session.NewSession("webchat:test-user", "tool-condense-session", nil)
	large := strings.Repeat("abcdef", 40)

	if err := runtime.appendToolResultMessage(context.Background(), sess, models.ToolCall{
		ID:       "tool-call-oversized",
		Function: models.ToolCallFunction{Name: "file_read"},
	}, large); err != nil {
		t.Fatalf("append tool result message: %v", err)
	}

	messages := sess.GetMessages()
	if len(messages) != 1 {
		t.Fatalf("expected one persisted message, got %d", len(messages))
	}
	msg := messages[0]
	if !strings.Contains(msg.Content, "[tool output condensed]") {
		t.Fatalf("expected condensed marker, got %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "Original size: 240 bytes") {
		t.Fatalf("expected original size in condensed content, got %q", msg.Content)
	}
	if strings.Contains(msg.Content, large) {
		t.Fatal("expected oversized raw tool result to be omitted")
	}
}

func TestToolResultObserverTextCondensesOversizedContent(t *testing.T) {
	runtime := NewRuntime("agent-media-test", t.TempDir(), nil, nil, nil, models.RoleAdmin, nil, true, nil)
	runtime.SetToolResultCondenseLimit(64)
	large := strings.Repeat("abcdef", 40)

	got := runtime.toolResultObserverText("file_read", large)
	if !strings.Contains(got, "[tool output condensed]") {
		t.Fatalf("expected condensed observer text, got %q", got)
	}
	if !strings.Contains(got, "Preview:") {
		t.Fatalf("expected preview section, got %q", got)
	}
}
