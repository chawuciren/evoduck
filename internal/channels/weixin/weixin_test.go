package weixin

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/chawuciren/evoduck/pkg/models"
)

type recordingTypingAPI struct {
	configs      map[string]*GetConfigResponse
	statuses     []int
	messages     [][]MessageItem
	downloadBody []byte
	contentType  string
}

func (r *recordingTypingAPI) SendMessage(_ context.Context, _ string, _ string, items []MessageItem) error {
	r.messages = append(r.messages, items)
	return nil
}

func (r *recordingTypingAPI) GetConfig(_ context.Context, _ string, contextToken string) (*GetConfigResponse, error) {
	cfg := r.configs[contextToken]
	if cfg == nil {
		return nil, fmt.Errorf("missing config for %s", contextToken)
	}
	return cfg, nil
}

func (r *recordingTypingAPI) SendTyping(_ context.Context, _ string, _ string, status int) error {
	r.statuses = append(r.statuses, status)
	return nil
}

func (r *recordingTypingAPI) SetAuth(string, string) {}

func (r *recordingTypingAPI) GetUpdates(context.Context, string) (*GetUpdatesResponse, error) {
	return nil, nil
}

func (r *recordingTypingAPI) GetUploadURL(context.Context, *GetUploadURLRequest) (*GetUploadURLResponse, error) {
	return nil, nil
}

func (r *recordingTypingAPI) UploadEncryptedMedia(context.Context, string, []byte) (string, error) {
	return "", nil
}

func (r *recordingTypingAPI) DownloadMedia(context.Context, string) ([]byte, string, error) {
	return r.downloadBody, r.contentType, nil
}

func TestBuildOutgoingItemsWithMedia(t *testing.T) {
	bridge := &WeixinBridge{}
	items, err := bridge.buildOutgoingItems(context.Background(), nil, "", &models.OutgoingMessage{
		Content: "hello",
		Media: []models.OutgoingMedia{
			{
				Type:              "image",
				EncryptQueryParam: "enc=image",
				AESKey:            "aes-image",
			},
			{
				Type:              "audio",
				EncryptQueryParam: "enc=audio",
				AESKey:            "aes-audio",
			},
			{
				Type:              "video",
				EncryptQueryParam: "enc=video",
				AESKey:            "aes-video",
			},
		},
	})
	if err != nil {
		t.Fatalf("build outgoing items: %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("expected 4 items, got %d", len(items))
	}
	if items[0].Type != 1 || items[0].TextItem == nil || items[0].TextItem.Text != "hello" {
		t.Fatalf("unexpected text item: %#v", items[0])
	}
	if items[1].Type != 2 || items[1].ImageItem == nil || items[1].ImageItem.Media == nil || items[1].ImageItem.Media.EncryptQueryParam != "enc=image" {
		t.Fatalf("unexpected image item: %#v", items[1])
	}
	if items[1].ImageItem.MidSize != EncryptedMediaSize(0) {
		t.Fatalf("expected image item mid_size to use ciphertext size semantics, got %#v", items[1].ImageItem)
	}
	if items[2].Type != 3 || items[2].VoiceItem == nil || items[2].VoiceItem.Media == nil || items[2].VoiceItem.Media.AESKey != "aes-audio" {
		t.Fatalf("unexpected audio item: %#v", items[2])
	}
	if items[3].Type != 5 || items[3].VideoItem == nil || items[3].VideoItem.Media == nil || items[3].VideoItem.Media.AESKey != "aes-video" {
		t.Fatalf("unexpected video item: %#v", items[3])
	}
}

func TestBuildOutgoingItemsRequireUploadClientForRawMedia(t *testing.T) {
	bridge := &WeixinBridge{}
	_, err := bridge.buildOutgoingItems(context.Background(), nil, "", &models.OutgoingMessage{
		Media: []models.OutgoingMedia{{Type: "image", Data: "raw-bytes"}},
	})
	if err == nil {
		t.Fatal("expected error when upload client is missing")
	}
}

func TestEncodeMediaAESKeyForImageUsesBase64OfHex(t *testing.T) {
	key := []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x10, 0x32, 0x54, 0x76, 0x98, 0xba, 0xdc, 0xfe}
	got := EncodeMediaAESKey(1, key)
	want := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef1032547698badcfe"))
	if got != want {
		t.Fatalf("expected image aes key %q, got %q", want, got)
	}
}

func TestNormalizeMessageMapsImageItemsToMedia(t *testing.T) {
	bridge := New(WeixinConfig{}, nil)
	bridge.SetChannelConfig("weixin-cs", "bot-user", models.RoleEmployee)
	msg := bridge.normalizeMessage(WeixinMessage{
		MessageType:  1,
		SessionID:    "session-1",
		ContextToken: "ctx-1",
		ItemList: []MessageItem{{
			Type: 2,
			ImageItem: &ImageItem{
				Media: &CDNMedia{
					EncryptQueryParam: "enc=image",
					AESKey:            "aes-image",
					FullURL:           "https://example.com/image.jpg",
				},
				MidSize: 1234,
			},
		}},
	})
	if msg == nil {
		t.Fatal("expected normalized message")
	}
	if msg.Content != "" {
		t.Fatalf("expected empty text content, got %q", msg.Content)
	}
	if len(msg.Media) != 1 {
		t.Fatalf("expected one media item, got %#v", msg.Media)
	}
	if msg.Media[0].Type != "image" || msg.Media[0].URL != "https://example.com/image.jpg" {
		t.Fatalf("unexpected media payload: %#v", msg.Media[0])
	}
	if msg.Media[0].EncryptQueryParam != "enc=image" || msg.Media[0].AESKey != "aes-image" {
		t.Fatalf("expected encrypted media fields, got %#v", msg.Media[0])
	}
	if msg.Media[0].FileSize != 1234 {
		t.Fatalf("expected image size metadata, got %#v", msg.Media[0])
	}
}

func TestResolveIncomingMediaDownloadsAndDecrypts(t *testing.T) {
	key, err := GenerateMediaAESKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	plain := []byte("hello inbound image")
	encrypted, err := EncryptMediaForUpload(plain, key)
	if err != nil {
		t.Fatalf("encrypt media: %v", err)
	}
	api := &recordingTypingAPI{downloadBody: encrypted, contentType: "image/jpeg"}
	bridge := New(WeixinConfig{}, nil)
	bridge.api = api
	media := []models.OutgoingMedia{{
		Type:              "image",
		URL:               "https://example.com/media",
		EncryptQueryParam: "enc=image",
		AESKey:            EncodeMediaAESKey(1, key),
	}}
	if err := bridge.resolveIncomingMedia(context.Background(), media); err != nil {
		t.Fatalf("resolve incoming media: %v", err)
	}
	if media[0].Data != base64.StdEncoding.EncodeToString(plain) {
		t.Fatalf("unexpected decoded media data: %#v", media[0])
	}
	if media[0].MimeType != "image/jpeg" {
		t.Fatalf("expected mime type image/jpeg, got %q", media[0].MimeType)
	}
	if media[0].FileSize != int64(len(plain)) {
		t.Fatalf("expected file size %d, got %d", len(plain), media[0].FileSize)
	}
}

func TestHandleEventIgnoresIntermediateUpdates(t *testing.T) {
	bridge := New(WeixinConfig{}, nil)
	bridge.running = true
	bridge.userID = "bot-user"
	api := &recordingTypingAPI{}
	bridge.api = api
	target := &models.NormalizedMessage{
		Channel:   "weixin",
		AccountID: "weixin-cs",
		SenderID:  "user-1",
		ThreadID:  "thread-1",
	}
	if err := bridge.HandleEvent(context.Background(), target, &models.ChannelEvent{Type: models.ChannelEventPlan, Plan: &models.TaskPlan{Intent: "do work"}}); err != nil {
		t.Fatalf("buffer plan: %v", err)
	}
	if err := bridge.HandleEvent(context.Background(), target, &models.ChannelEvent{Type: models.ChannelEventToolEnd, ToolName: "search"}); err != nil {
		t.Fatalf("ignore tool_end: %v", err)
	}
	if len(api.messages) != 0 {
		t.Fatalf("expected no intermediate sends, got %d", len(api.messages))
	}
}

func TestHandleEventControlsTypingLifecycle(t *testing.T) {
	bridge := New(WeixinConfig{}, nil)
	bridge.running = true
	bridge.userID = "bot-user"
	api := &recordingTypingAPI{configs: map[string]*GetConfigResponse{
		"ctx-1": {TypingTicket: "ticket-1"},
	}}
	bridge.api = api
	target := &models.NormalizedMessage{
		Channel:      "weixin",
		AccountID:    "weixin-cs",
		SenderID:     "user-1",
		ThreadID:     "thread-1",
		ContextToken: "ctx-1",
	}
	if err := bridge.HandleEvent(context.Background(), target, &models.ChannelEvent{Type: models.ChannelEventRunStart}); err != nil {
		t.Fatalf("run_start: %v", err)
	}
	if err := bridge.HandleEvent(context.Background(), target, &models.ChannelEvent{Type: models.ChannelEventFinal, Content: "done"}); err != nil {
		t.Fatalf("final: %v", err)
	}
	if len(api.statuses) != 2 {
		t.Fatalf("expected 2 typing updates, got %d", len(api.statuses))
	}
	if api.statuses[0] != 1 || api.statuses[1] != 0 {
		t.Fatalf("expected typing states [1 0], got %#v", api.statuses)
	}
}

func TestHandleEventSendsOnlyFinalMessage(t *testing.T) {
	bridge := New(WeixinConfig{}, nil)
	bridge.running = true
	bridge.userID = "bot-user"
	api := &recordingTypingAPI{configs: map[string]*GetConfigResponse{
		"ctx-1": {TypingTicket: "ticket-1"},
	}}
	bridge.api = api
	target := &models.NormalizedMessage{
		Channel:      "weixin",
		AccountID:    "weixin-cs",
		SenderID:     "user-1",
		ThreadID:     "thread-1",
		ContextToken: "ctx-1",
	}
	if err := bridge.HandleEvent(context.Background(), target, &models.ChannelEvent{Type: models.ChannelEventPlanUpdate, Plan: &models.TaskPlan{Intent: "Investigate", SubTasks: []models.SubTask{{Name: "Check logs", Status: "running"}}}}); err != nil {
		t.Fatalf("plan_update: %v", err)
	}
	if err := bridge.HandleEvent(context.Background(), target, &models.ChannelEvent{Type: models.ChannelEventToolEnd, ToolName: "search"}); err != nil {
		t.Fatalf("tool_end: %v", err)
	}
	if err := bridge.HandleEvent(context.Background(), target, &models.ChannelEvent{Type: models.ChannelEventFinal, Content: "final answer"}); err != nil {
		t.Fatalf("final: %v", err)
	}
	if len(api.messages) != 1 {
		t.Fatalf("expected only final send, got %d", len(api.messages))
	}
	if len(api.messages[0]) != 1 || api.messages[0][0].TextItem == nil {
		t.Fatalf("unexpected final message payload: %#v", api.messages[0])
	}
	if got := api.messages[0][0].TextItem.Text; got != "final answer" {
		t.Fatalf("unexpected final message: %q", got)
	}
}
