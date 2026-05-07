package webchat

import (
	"encoding/json"
	"testing"

	"github.com/chawuciren/evoduck/pkg/models"
)

func TestSummarizeWebChatMessageWithMedia(t *testing.T) {
	got := summarizeWebChatMessage("see attachment", []models.OutgoingMedia{{Type: "image", Name: "demo.png"}, {Type: "audio"}})
	if got != "see attachment\n[image: demo.png] [audio]" {
		t.Fatalf("unexpected summary: %q", got)
	}
}

func TestAddHistoryStoresMedia(t *testing.T) {
	chat := New(WebChatConfig{})
	chat.addHistory("assistant", "hello\n[image: demo.png]", []models.OutgoingMedia{{Type: "image", Name: "demo.png"}})
	if len(chat.history.messages) != 1 {
		t.Fatalf("expected 1 history item, got %d", len(chat.history.messages))
	}
	msg := chat.history.messages[0]
	if msg.Content != "hello\n[image: demo.png]" {
		t.Fatalf("unexpected history content: %q", msg.Content)
	}
	if len(msg.Media) != 1 || msg.Media[0].Type != "image" {
		t.Fatalf("unexpected history media: %#v", msg.Media)
	}
}

func TestWebChatMessageMarshalsMedia(t *testing.T) {
	wsMsg := WebChatMessage{
		Type:    "message",
		Content: "hello",
		Media:   []models.OutgoingMedia{{Type: "image", Name: "demo.png", URL: "https://example.com/demo.png"}},
	}
	buf, err := json.Marshal(wsMsg)
	if err != nil {
		t.Fatalf("marshal webchat message: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(buf, &decoded); err != nil {
		t.Fatalf("unmarshal webchat message: %v", err)
	}
	media, ok := decoded["media"].([]interface{})
	if !ok || len(media) != 1 {
		t.Fatalf("expected media array in payload, got %#v", decoded["media"])
	}
	first, _ := media[0].(map[string]interface{})
	if got := first["type"]; got != "image" {
		t.Fatalf("unexpected media type: %#v", got)
	}
	if got := first["name"]; got != "demo.png" {
		t.Fatalf("unexpected media name: %#v", got)
	}
}

func TestFormatWebChatPlan(t *testing.T) {
	got := formatWebChatPlan(&models.TaskPlan{
		Intent:   "Investigate issue",
		SubTasks: []models.SubTask{{Name: "Check logs", Status: "running"}, {Name: "Send reply", Status: "pending"}},
	})
	if got != "Investigate issue\n1. Check logs [running]\n2. Send reply [pending]" {
		t.Fatalf("unexpected plan rendering: %q", got)
	}
}

func TestBuildWebChatEventMessageUsesPlanRendering(t *testing.T) {
	msg := buildWebChatEventMessage(&models.NormalizedMessage{ThreadID: "thread-1"}, &models.ChannelEvent{
		Type: models.ChannelEventPlanUpdate,
		Plan: &models.TaskPlan{Intent: "Investigate issue", SubTasks: []models.SubTask{{Name: "Check logs", Status: "running"}}},
	})
	if msg.Type != models.ChannelEventPlanUpdate {
		t.Fatalf("unexpected type: %q", msg.Type)
	}
	if msg.SessionID != "thread-1" {
		t.Fatalf("unexpected session id: %q", msg.SessionID)
	}
	if msg.Content != "Investigate issue\n1. Check logs [running]" {
		t.Fatalf("unexpected event content: %q", msg.Content)
	}
}

func TestBuildWebChatEventMessageMapsRunStartToTyping(t *testing.T) {
	msg := buildWebChatEventMessage(&models.NormalizedMessage{ThreadID: "thread-1"}, &models.ChannelEvent{
		Type: models.ChannelEventRunStart,
	})
	if msg.Type != "typing" {
		t.Fatalf("unexpected type: %q", msg.Type)
	}
}
