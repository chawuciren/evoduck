package wecom

import (
	"crypto/aes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chawuciren/evoduck/pkg/models"
)

func TestNameReturnsChannelID(t *testing.T) {
	bridge := New(WeComConfig{}, nil)
	bridge.SetChannelConfig("wecom-hr", models.RoleEmployee)
	if bridge.Name() != "wecom-hr" {
		t.Fatalf("expected channel id name, got %q", bridge.Name())
	}
}

func TestNewWithConfig(t *testing.T) {
	config := WeComConfig{
		BotID:  "test-bot-id",
		Secret: "test-secret",
	}
	bridge := New(config, nil)
	if bridge == nil {
		t.Fatal("expected bridge to be created")
	}
}

func TestConnectWithoutConfig(t *testing.T) {
	bridge := New(WeComConfig{}, nil)
	bridge.SetChannelConfig("test-channel", models.RoleEmployee)
	err := bridge.Connect(nil)
	if err == nil {
		t.Fatal("expected error when connecting without bot_id/secret")
	}
}

func TestBuildOutgoingMediaPayloadUsesExistingMediaID(t *testing.T) {
	bridge := New(WeComConfig{}, nil)
	payload, err := bridge.buildOutgoingMediaPayload(nil, nil, models.OutgoingMedia{Type: "image", URL: "media-id-1"})
	if err != nil {
		t.Fatalf("build outgoing media payload: %v", err)
	}
	if payload.MsgType != "image" {
		t.Fatalf("expected image msg type, got %q", payload.MsgType)
	}
	if got := payload.Content["media_id"]; got != "media-id-1" {
		t.Fatalf("expected media_id media-id-1, got %#v", got)
	}
}

func TestBuildOutgoingMediaPayloadMapsAudioToVoice(t *testing.T) {
	bridge := New(WeComConfig{}, nil)
	payload, err := bridge.buildOutgoingMediaPayload(nil, nil, models.OutgoingMedia{Type: "audio", URL: "media-id-2"})
	if err != nil {
		t.Fatalf("build outgoing media payload: %v", err)
	}
	if payload.MsgType != "voice" {
		t.Fatalf("expected voice msg type, got %q", payload.MsgType)
	}
}

func TestSendProactiveMediaMessagesBuildsMediaAndTextFrames(t *testing.T) {
	bridge := New(WeComConfig{}, nil)
	conn := &capturingJSONConn{}
	err := bridge.sendProactiveMediaMessages(nil, conn, &models.OutgoingMessage{
		TargetID: "user-1",
		Content:  "hello",
		Media: []models.OutgoingMedia{
			{Type: "image", URL: "media-image-1"},
			{Type: "file", URL: "media-file-1"},
		},
	})
	if err != nil {
		t.Fatalf("send proactive media messages: %v", err)
	}
	if len(conn.frames) != 3 {
		t.Fatalf("expected 3 frames, got %d", len(conn.frames))
	}
	firstBody := decodeBodyMap(t, conn.frames[0])
	if got := firstBody["msgtype"]; got != "image" {
		t.Fatalf("expected first frame image, got %#v", got)
	}
	if got := firstBody["chat_id"]; got != "user-1" {
		t.Fatalf("expected first frame chat_id user-1, got %#v", got)
	}
	if got := firstBody["chatid"]; got != nil {
		t.Fatalf("expected legacy chatid field to be absent, got %#v", got)
	}
	secondBody := decodeBodyMap(t, conn.frames[1])
	if got := secondBody["msgtype"]; got != "file" {
		t.Fatalf("expected second frame file, got %#v", got)
	}
	thirdBody := decodeBodyMap(t, conn.frames[2])
	if got := thirdBody["msgtype"]; got != "text" {
		t.Fatalf("expected final frame text, got %#v", got)
	}
	textObj, _ := thirdBody["text"].(map[string]interface{})
	if got := textObj["content"]; got != "hello" {
		t.Fatalf("expected final text content hello, got %#v", got)
	}
}

func TestSendReplyMediaMessagesBuildsRespondFrames(t *testing.T) {
	bridge := New(WeComConfig{}, nil)
	conn := &capturingJSONConn{}
	err := bridge.sendReplyMediaMessages(nil, conn, &models.OutgoingMessage{
		ContextToken: "req-1",
		Content:      "done",
		Media: []models.OutgoingMedia{
			{Type: "image", URL: "media-image-1"},
			{Type: "video", URL: "media-video-1", Name: "clip.mp4", MimeType: "summary"},
		},
	})
	if err != nil {
		t.Fatalf("send reply media messages: %v", err)
	}
	if len(conn.frames) != 3 {
		t.Fatalf("expected 3 frames, got %d", len(conn.frames))
	}
	for i, frame := range conn.frames {
		if got := frame["cmd"]; got != "aibot_respond_msg" {
			t.Fatalf("expected respond cmd on frame %d, got %#v", i, got)
		}
		headers, _ := frame["headers"].(map[string]interface{})
		if got := headers["req_id"]; got != "req-1" {
			t.Fatalf("expected req_id req-1 on frame %d, got %#v", i, got)
		}
	}
	firstBody := decodeBodyMap(t, conn.frames[0])
	if got := firstBody["msgtype"]; got != "image" {
		t.Fatalf("expected first reply frame image, got %#v", got)
	}
	secondBody := decodeBodyMap(t, conn.frames[1])
	if got := secondBody["msgtype"]; got != "video" {
		t.Fatalf("expected second reply frame video, got %#v", got)
	}
	videoObj, _ := secondBody["video"].(map[string]interface{})
	if got := videoObj["title"]; got != "clip.mp4" {
		t.Fatalf("expected video title clip.mp4, got %#v", got)
	}
	thirdBody := decodeBodyMap(t, conn.frames[2])
	if got := thirdBody["msgtype"]; got != "text" {
		t.Fatalf("expected final reply frame text, got %#v", got)
	}
}

func TestHandleEventSendsPlaceholderThenFinalStream(t *testing.T) {
	bridge := New(WeComConfig{}, nil)
	conn := &capturingJSONConn{}
	bridge.conn = nil
	bridge.connected = true
	target := &models.NormalizedMessage{
		Channel:      "wecom",
		AccountID:    "wecom-1",
		SenderID:     "user-1",
		ThreadID:     "chat-1",
		ContextToken: "req-1",
	}
	stateKey := wecomEventKey(target)
	state := bridge.getOrCreateEventState(stateKey, "stream-1")
	if state.streamID != "stream-1" {
		t.Fatalf("expected state stream id to be preserved, got %q", state.streamID)
	}
	if err := bridge.sendStreamResponse(conn, &models.OutgoingMessage{ContextToken: "req-1", Content: "Working on it...", StreamID: state.streamID, StreamDone: false}); err != nil {
		t.Fatalf("send placeholder: %v", err)
	}
	bridge.markPlaceholderSent(stateKey)
	if err := bridge.sendStreamResponse(conn, &models.OutgoingMessage{ContextToken: "req-1", Content: "final answer", StreamID: state.streamID, StreamDone: true}); err != nil {
		t.Fatalf("send final: %v", err)
	}
	if len(conn.frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(conn.frames))
	}
	firstBody := decodeBodyMap(t, conn.frames[0])
	firstStream, _ := firstBody["stream"].(map[string]interface{})
	if got := firstStream["content"]; got != "Working on it..." {
		t.Fatalf("expected placeholder content, got %#v", got)
	}
	if got := firstStream["finish"]; got != false {
		t.Fatalf("expected placeholder finish=false, got %#v", got)
	}
	secondBody := decodeBodyMap(t, conn.frames[1])
	secondStream, _ := secondBody["stream"].(map[string]interface{})
	if got := secondStream["content"]; got != "final answer" {
		t.Fatalf("expected final content, got %#v", got)
	}
	if got := secondStream["finish"]; got != true {
		t.Fatalf("expected final finish=true, got %#v", got)
	}
}

func TestHandleEventRunStartSendsPlaceholder(t *testing.T) {
	bridge := New(WeComConfig{}, nil)
	conn := &capturingJSONConn{}
	bridge.writer = conn
	target := &models.NormalizedMessage{
		Channel:      "wecom",
		AccountID:    "wecom-1",
		SenderID:     "user-1",
		ThreadID:     "chat-1",
		ContextToken: "req-1",
	}
	stateKey := wecomEventKey(target)
	if err := bridge.HandleEvent(nil, target, &models.ChannelEvent{Type: models.ChannelEventRunStart, StreamID: "stream-1"}); err != nil {
		t.Fatalf("handle run_start: %v", err)
	}
	if len(conn.frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(conn.frames))
	}
	assertWeComStreamContent(t, conn.frames[0], "<think>thinking...</think>", false)
	state := bridge.getOrCreateEventState(stateKey, "stream-1")
	if !state.placeholderSent {
		t.Fatal("expected run_start to mark placeholder sent")
	}
}

func TestHandleEventSendsPlanThinkingAndToolMessagesImmediately(t *testing.T) {
	bridge := New(WeComConfig{}, nil)
	conn := &capturingJSONConn{}
	bridge.writer = conn
	target := &models.NormalizedMessage{
		Channel:      "wecom",
		AccountID:    "wecom-1",
		SenderID:     "user-1",
		ThreadID:     "chat-1",
		ContextToken: "req-1",
	}
	events := []*models.ChannelEvent{
		{Type: models.ChannelEventPlanUpdate, Plan: &models.TaskPlan{Intent: "Investigate issue", SubTasks: []models.SubTask{{Name: "Check logs", Status: "running"}}}, StreamID: "stream-1"},
		{Type: models.ChannelEventThinking, Content: "Inspecting logs", StreamID: "stream-1"},
		{Type: models.ChannelEventToolStart, ToolName: "search", StreamID: "stream-1"},
		{Type: models.ChannelEventContentChunk, Content: "first token", StreamID: "stream-1"},
		{Type: models.ChannelEventToolEnd, ToolName: "search", StreamID: "stream-1"},
	}
	for _, event := range events {
		if err := bridge.HandleEvent(nil, target, event); err != nil {
			t.Fatalf("handle %s: %v", event.Type, err)
		}
	}
	if len(conn.frames) != 3 {
		t.Fatalf("expected 3 frames, got %d", len(conn.frames))
	}
	assertWeComStreamContent(t, conn.frames[0], "<think>Plan: Investigate issue\n1. Check logs [running]</think>", false)
	assertWeComStreamContent(t, conn.frames[1], "<think>Using tool: search</think>", false)
	assertWeComStreamContent(t, conn.frames[2], "<think>Tool completed: search</think>", false)
}

func TestHandleEventSendsPlanThenToolEndMessage(t *testing.T) {
	bridge := New(WeComConfig{}, nil)
	conn := &capturingJSONConn{}
	bridge.writer = conn
	target := &models.NormalizedMessage{
		Channel:      "wecom",
		AccountID:    "wecom-1",
		SenderID:     "user-1",
		ThreadID:     "chat-1",
		ContextToken: "req-1",
	}
	if err := bridge.HandleEvent(nil, target, &models.ChannelEvent{Type: models.ChannelEventPlanUpdate, Plan: &models.TaskPlan{Intent: "Investigate issue", SubTasks: []models.SubTask{{Name: "Check logs", Status: "running"}}}, StreamID: "stream-1"}); err != nil {
		t.Fatalf("plan_update: %v", err)
	}
	if err := bridge.HandleEvent(nil, target, &models.ChannelEvent{Type: models.ChannelEventToolEnd, ToolName: "search", StreamID: "stream-1"}); err != nil {
		t.Fatalf("tool_end: %v", err)
	}
	if len(conn.frames) != 2 {
		t.Fatalf("expected plan and tool_end frames, got %d", len(conn.frames))
	}
	assertWeComStreamContent(t, conn.frames[0], "<think>Plan: Investigate issue\n1. Check logs [running]</think>", false)
	assertWeComStreamContent(t, conn.frames[1], "<think>Tool completed: search</think>", false)
}

func TestHandleEventSendsPlanThenFinal(t *testing.T) {
	bridge := New(WeComConfig{}, nil)
	conn := &capturingJSONConn{}
	bridge.writer = conn
	target := &models.NormalizedMessage{
		Channel:      "wecom",
		AccountID:    "wecom-1",
		SenderID:     "user-1",
		ThreadID:     "chat-1",
		ContextToken: "req-1",
	}
	if err := bridge.HandleEvent(nil, target, &models.ChannelEvent{Type: models.ChannelEventPlanUpdate, Plan: &models.TaskPlan{Intent: "Investigate issue"}, StreamID: "stream-1"}); err != nil {
		t.Fatalf("plan_update: %v", err)
	}
	if err := bridge.HandleEvent(nil, target, &models.ChannelEvent{Type: models.ChannelEventFinal, Content: "final answer", StreamID: "stream-1"}); err != nil {
		t.Fatalf("final: %v", err)
	}
	if len(conn.frames) != 2 {
		t.Fatalf("expected plan and final, got %d frames", len(conn.frames))
	}
	assertWeComStreamContent(t, conn.frames[0], "<think>Plan: Investigate issue</think>", false)
	assertWeComStreamContent(t, conn.frames[1], "final answer", true)
}

func TestHandleCommandPrioritizesMsgCallbackOverPendingResolution(t *testing.T) {
	bridge := New(WeComConfig{}, nil)
	got := make(chan *models.NormalizedMessage, 1)
	bridge.SetChannelConfig("wecom-1", models.RoleEmployee)
	bridge.OnMessage(func(msg *models.NormalizedMessage) {
		got <- msg
	})
	bridge.pending["req-1"] = make(chan map[string]interface{}, 1)
	bridge.handleCommand("aibot_msg_callback", map[string]interface{}{
		"cmd": "aibot_msg_callback",
		"headers": map[string]interface{}{
			"req_id": "req-1",
		},
		"body": map[string]interface{}{
			"msgtype": "text",
			"req_id":  "req-1",
			"from": map[string]interface{}{
				"userid": "alice",
			},
			"text": map[string]interface{}{
				"content": "hello",
			},
		},
	})
	select {
	case msg := <-got:
		if msg.Content != "hello" {
			t.Fatalf("unexpected content: %q", msg.Content)
		}
		if msg.ContextToken != "req-1" {
			t.Fatalf("unexpected context token: %q", msg.ContextToken)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message callback")
	}
	select {
	case <-bridge.pending["req-1"]:
		t.Fatal("expected pending req_id not to consume callback message")
	default:
	}
}

func TestHandleCommandMapsImageCallbackToMedia(t *testing.T) {
	bridge := New(WeComConfig{}, nil)
	got := make(chan *models.NormalizedMessage, 1)
	bridge.SetChannelConfig("wecom-1", models.RoleEmployee)
	bridge.OnMessage(func(msg *models.NormalizedMessage) {
		got <- msg
	})
	bridge.handleCommand("aibot_msg_callback", map[string]interface{}{
		"cmd": "aibot_msg_callback",
		"headers": map[string]interface{}{
			"req_id": "req-image-1",
		},
		"body": map[string]interface{}{
			"msgtype": "image",
			"req_id":  "req-image-1",
			"from": map[string]interface{}{
				"userid": "alice",
			},
			"image": map[string]interface{}{
				"media_id": "media-image-1",
			},
		},
	})
	select {
	case msg := <-got:
		if msg.Content != "" {
			t.Fatalf("expected empty text content, got %q", msg.Content)
		}
		if len(msg.Media) != 1 {
			t.Fatalf("expected one media item, got %#v", msg.Media)
		}
		if msg.Media[0].Type != "image" || msg.Media[0].URL != "media-image-1" {
			t.Fatalf("unexpected media payload: %#v", msg.Media[0])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for image callback")
	}
}

func TestHandleCommandDownloadsAndDecryptsRemoteImage(t *testing.T) {
	key := []byte("1234567890abcdef")
	plain := []byte("wecom inbound image")
	encrypted := encryptECBForTest(t, plain, key)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(encrypted)
	}))
	defer server.Close()

	bridge := New(WeComConfig{}, nil)
	got := make(chan *models.NormalizedMessage, 1)
	bridge.SetChannelConfig("wecom-1", models.RoleEmployee)
	bridge.OnMessage(func(msg *models.NormalizedMessage) {
		got <- msg
	})
	bridge.handleCommand("aibot_msg_callback", map[string]interface{}{
		"cmd": "aibot_msg_callback",
		"headers": map[string]interface{}{
			"req_id": "req-image-2",
		},
		"body": map[string]interface{}{
			"msgtype": "image",
			"req_id":  "req-image-2",
			"from": map[string]interface{}{
				"userid": "alice",
			},
			"image": map[string]interface{}{
				"url":    server.URL + "/encrypted.jpg",
				"aeskey": base64.StdEncoding.EncodeToString(key),
			},
		},
	})
	select {
	case msg := <-got:
		if len(msg.Media) != 1 {
			t.Fatalf("expected one media item, got %#v", msg.Media)
		}
		if gotData := msg.Media[0].Data; gotData != base64.StdEncoding.EncodeToString(plain) {
			t.Fatalf("unexpected decoded media data: %#v", msg.Media[0])
		}
		if msg.Media[0].MimeType != "image/jpeg" {
			t.Fatalf("expected mime type image/jpeg, got %q", msg.Media[0].MimeType)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for remote image callback")
	}
}

type capturingJSONConn struct {
	frames []map[string]interface{}
}

func (c *capturingJSONConn) WriteJSON(v interface{}) error {
	buf, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(buf, &decoded); err != nil {
		return err
	}
	c.frames = append(c.frames, decoded)
	return nil
}

func decodeBodyMap(t *testing.T, frame map[string]interface{}) map[string]interface{} {
	t.Helper()
	body, _ := frame["body"].(map[string]interface{})
	if body == nil {
		t.Fatalf("expected body map in frame: %#v", frame)
	}
	return body
}

func assertWeComStreamContent(t *testing.T, frame map[string]interface{}, want string, wantFinish bool) {
	t.Helper()
	body := decodeBodyMap(t, frame)
	stream, _ := body["stream"].(map[string]interface{})
	if got := stream["content"]; got != want {
		t.Fatalf("expected stream content %q, got %#v", want, got)
	}
	if got := stream["finish"]; got != wantFinish {
		t.Fatalf("expected stream finish=%v, got %#v", wantFinish, got)
	}
}

func encryptECBForTest(t *testing.T, plain []byte, key []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("create aes cipher: %v", err)
	}
	padding := aes.BlockSize - (len(plain) % aes.BlockSize)
	if padding == 0 {
		padding = aes.BlockSize
	}
	padded := append(append([]byte{}, plain...), make([]byte, padding)...)
	for i := len(plain); i < len(padded); i++ {
		padded[i] = byte(padding)
	}
	encrypted := make([]byte, len(padded))
	for start := 0; start < len(padded); start += aes.BlockSize {
		block.Encrypt(encrypted[start:start+aes.BlockSize], padded[start:start+aes.BlockSize])
	}
	return encrypted
}
