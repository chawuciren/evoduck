package wecom

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chawuciren/evoduck/internal/channels"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
	pproxy "github.com/chawuciren/evoduck/pkg/proxy"
	"github.com/gorilla/websocket"
)

const (
	wsURL             = "wss://openws.work.weixin.qq.com"
	heartbeatInterval = 30 * time.Second
	reconnectDelay    = 1 * time.Second
	maxReconnectDelay = 30 * time.Second
)

var wecomStreamCounter atomic.Uint64

type WeCom struct {
	mu        sync.RWMutex
	config    WeComConfig
	conn      *websocket.Conn
	writer    wecomJSONWriter
	handler   func(*models.NormalizedMessage)
	ctx       context.Context
	cancel    context.CancelFunc
	connected bool
	decider   *pproxy.Decider

	channelID string
	role      models.Role
	eventMu   sync.Mutex
	events    map[string]*wecomEventState

	pendingMu sync.Mutex
	pending   map[string]chan map[string]interface{}
}

type wecomEventState struct {
	placeholderSent bool
	streamID        string
	progressText    string
	lastSentText    string
	thinkOpen       bool
}

type wecomJSONWriter interface {
	WriteJSON(v interface{}) error
}

type WeComConfig struct {
	BotID  string `json:"bot_id"`
	Secret string `json:"secret"`
}

func New(config WeComConfig, decider *pproxy.Decider) *WeCom {
	return &WeCom{
		config:  config,
		pending: make(map[string]chan map[string]interface{}),
		events:  make(map[string]*wecomEventState),
		decider: decider,
	}
}

func (w *WeCom) Name() string {
	return w.channelID
}

func (w *WeCom) SetChannelConfig(channelID string, role models.Role) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.channelID = channelID
	w.role = role
}

func (w *WeCom) Connect(ctx context.Context) error {
	if w.config.BotID == "" || w.config.Secret == "" {
		logger.Warn("WeCom missing bot_id or secret", logger.Fields{
			"channel_id": w.channelID,
		})
		return fmt.Errorf("wecom: bot_id and secret are required")
	}

	w.ctx, w.cancel = context.WithCancel(ctx)

	if err := w.connectAndSubscribe(); err != nil {
		return err
	}

	go w.run()
	go w.heartbeatLoop()

	logger.Info("WeCom WebSocket connected", logger.Fields{
		"channel_id": w.channelID,
		"bot_id":     w.config.BotID,
	})

	return nil
}

func (w *WeCom) connectAndSubscribe() error {
	dialer := websocket.DefaultDialer

	// 如果配置了代理，使用自定义 dialer
	if w.decider != nil {
		decision := w.decider.ForChannel("wecom")
		if decision.UseProxy {
			switch decision.ProxyType {
			case "socks5":
				// SOCKS5 代理需要自定义 NetDialContext
				socks5Dialer := w.decider.GetProxyClient().GetSOCKS5Dialer()
				if socks5Dialer != nil {
					dialer = &websocket.Dialer{
						NetDialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
							return socks5Dialer.Dial(network, addr)
						},
					}
				}
			case "http":
				// HTTP 代理使用 ProxyURL
				httpProxyURL := w.decider.GetProxyClient().GetHTTPProxyURL()
				if httpProxyURL != nil {
					dialer = &websocket.Dialer{
						Proxy: http.ProxyURL(httpProxyURL),
					}
				}
			}
		}
	}

	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("wecom: websocket dial failed: %w", err)
	}

	w.mu.Lock()
	w.conn = conn
	w.mu.Unlock()

	subscribeCmd := map[string]interface{}{
		"cmd": "aibot_subscribe",
		"body": map[string]interface{}{
			"bot_id": w.config.BotID,
			"secret": w.config.Secret,
		},
	}

	logger.Debug("WeCom sending subscribe", logger.Fields{
		"cmd":    "aibot_subscribe",
		"bot_id": w.config.BotID,
	})

	if err := conn.WriteJSON(subscribeCmd); err != nil {
		conn.Close()
		return fmt.Errorf("wecom: subscribe failed: %w", err)
	}

	// Read raw message first for debugging
	msgType, rawMsg, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return fmt.Errorf("wecom: read subscribe response failed: %w", err)
	}

	logger.Info("WeCom subscribe raw response", logger.Fields{
		"msg_type": msgType,
		"raw_msg":  string(rawMsg),
	})

	var resp map[string]interface{}
	if err := json.Unmarshal(rawMsg, &resp); err != nil {
		conn.Close()
		return fmt.Errorf("wecom: parse subscribe response failed: %w, raw: %s", err, string(rawMsg))
	}

	cmd, _ := resp["cmd"].(string)
	if cmd == "" {
		// Try alternative field names
		cmd, _ = resp["action"].(string)
		cmd, _ = resp["type"].(string)
	}

	logger.Info("WeCom subscribe parsed response", logger.Fields{
		"cmd":  cmd,
		"resp": resp,
	})

	if cmd == "error" {
		body, _ := resp["body"].(map[string]interface{})
		errCode, _ := body["err_code"].(float64)
		errMsg, _ := body["err_msg"].(string)
		conn.Close()
		return fmt.Errorf("wecom: subscribe error: code=%d, msg=%s", int(errCode), errMsg)
	}

	// Accept empty cmd or aibot_subscribe as success
	if cmd != "" && cmd != "aibot_subscribe" {
		conn.Close()
		return fmt.Errorf("wecom: unexpected subscribe response: %s (expected aibot_subscribe or empty)", cmd)
	}

	w.mu.Lock()
	w.connected = true
	w.mu.Unlock()

	logger.Info("WeCom subscribed successfully", logger.Fields{
		"channel_id": w.channelID,
		"bot_id":     w.config.BotID,
	})

	return nil
}

func (w *WeCom) run() {
	for {
		select {
		case <-w.ctx.Done():
			logger.Debug("WeCom run loop exiting", logger.Fields{
				"channel_id": w.channelID,
			})
			return
		default:
		}

		w.mu.RLock()
		conn := w.conn
		w.mu.RUnlock()

		if conn == nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		logger.Debug("WeCom waiting for message", logger.Fields{
			"channel_id": w.channelID,
		})

		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			logger.Error("WeCom WebSocket read error", logger.Fields{
				"error":      err.Error(),
				"channel_id": w.channelID,
			})
			w.handleDisconnect()
			w.reconnect()
			continue
		}

		logger.Debug("WeCom received message", logger.Fields{
			"channel_id": w.channelID,
			"msg":        msg,
		})

		cmd, _ := msg["cmd"].(string)
		w.handleCommand(cmd, msg)
	}
}

func (w *WeCom) handleCommand(cmd string, msg map[string]interface{}) {
	body, _ := msg["body"].(map[string]interface{})
	headers, _ := msg["headers"].(map[string]interface{})
	switch cmd {
	case "aibot_msg_callback":
		w.handleMsgCallback(msg)
		return
	case "aibot_event_callback":
		w.handleMsgCallback(msg)
		return
	}
	if replyTo := extractWeComReplyTo(msg, headers, body); replyTo != "" {
		w.resolvePending(replyTo, msg)
		return
	}

	logger.Debug("WeCom handling command", logger.Fields{
		"channel_id": w.channelID,
		"cmd":        cmd,
		"body":       body,
		"headers":    headers,
	})

	switch cmd {
	case "heartbeat":
		logger.Debug("WeCom heartbeat received", logger.Fields{
			"channel_id": w.channelID,
		})
	case "":
		logger.Debug("WeCom empty command received", logger.Fields{
			"channel_id": w.channelID,
			"msg":        msg,
		})
	case "error":
		logger.Error("WeCom WebSocket error command", logger.Fields{
			"body": body,
		})
	default:
		logger.Debug("WeCom WebSocket unknown command", logger.Fields{
			"cmd":  cmd,
			"body": body,
		})
	}
}

func (w *WeCom) handleMsgCallback(msg map[string]interface{}) {
	body, _ := msg["body"].(map[string]interface{})
	headers, _ := msg["headers"].(map[string]interface{})

	// req_id 可能在不同位置，都尝试获取
	reqIDFromBody, _ := body["req_id"].(string)
	reqIDFromHeaders, _ := headers["req_id"].(string)

	// 使用找到的 req_id
	reqID := reqIDFromBody
	if reqID == "" {
		reqID = reqIDFromHeaders
	}

	logger.Info("WeCom msg_callback raw", logger.Fields{
		"channel_id":          w.channelID,
		"body_keys":           getMapKeys(body),
		"headers_keys":        getMapKeys(headers),
		"req_id_from_body":    reqIDFromBody,
		"req_id_from_headers": reqIDFromHeaders,
		"req_id_used":         reqID,
	})

	msgType, _ := body["msgtype"].(string)
	msgID, _ := body["msgid"].(string)
	chatType, _ := body["chattype"].(string)
	responseURL, _ := body["response_url"].(string)

	// 从 from.userid 获取 sender_id
	from, _ := body["from"].(map[string]interface{})
	senderID, _ := from["userid"].(string)

	// chat_id 从 body 直接获取（群聊时存在）
	chatID, _ := body["chat_id"].(string)

	logger.Info("WeCom msg_callback parsed", logger.Fields{
		"channel_id":   w.channelID,
		"msg_type":     msgType,
		"msg_id":       msgID,
		"req_id":       reqID,
		"sender_id":    senderID,
		"chat_id":      chatID,
		"chat_type":    chatType,
		"response_url": responseURL,
	})

	var content string
	switch msgType {
	case "text":
		textObj, _ := body["text"].(map[string]interface{})
		content, _ = textObj["content"].(string)
		logger.Debug("WeCom text content", logger.Fields{
			"channel_id": w.channelID,
			"content":    content,
		})
	default:
		logger.Debug("WeCom unsupported msg_type", logger.Fields{
			"channel_id": w.channelID,
			"msg_type":   msgType,
		})
		return
	}

	if content == "" {
		logger.Debug("WeCom empty content, skipping", logger.Fields{
			"channel_id": w.channelID,
		})
		return
	}

	logger.Info("WeCom message received", logger.Fields{
		"channel_id": w.channelID,
		"sender_id":  senderID,
		"chat_id":    chatID,
		"chat_type":  chatType,
		"msg_type":   msgType,
		"content":    truncateString(content, 100),
		"req_id":     reqID,
	})

	if w.handler != nil {
		threadID := chatID
		if threadID == "" {
			threadID = fmt.Sprintf("wecom:%s", senderID)
		}

		normalized := &models.NormalizedMessage{
			Channel:      "wecom",
			AccountID:    w.channelID,
			SenderID:     senderID,
			UserID:       senderID,
			Content:      content,
			ThreadID:     threadID,
			IsDM:         chatType == "single",
			Role:         w.role,
			ContextToken: reqID,
			ResponseURL:  responseURL,
		}

		logger.Debug("WeCom calling message handler", logger.Fields{
			"channel_id": w.channelID,
			"sender_id":  senderID,
			"content":    truncateString(content, 50),
			"req_id":     reqID,
		})

		go w.handler(normalized)
	} else {
		logger.Warn("WeCom no message handler registered", logger.Fields{
			"channel_id": w.channelID,
		})
	}
}

func (w *WeCom) handleEventCallback(msg map[string]interface{}) {
	body, _ := msg["body"].(map[string]interface{})
	headers, _ := msg["headers"].(map[string]interface{})

	eventType, _ := body["event_type"].(string)
	senderID, _ := body["sender_id"].(string)
	reqID, _ := headers["req_id"].(string)

	logger.Info("WeCom event received", logger.Fields{
		"channel_id": w.channelID,
		"event_type": eventType,
		"sender_id":  senderID,
		"req_id":     reqID,
	})
}

func (w *WeCom) Disconnect() error {
	if w.cancel != nil {
		w.cancel()
	}
	w.mu.Lock()
	if w.conn != nil {
		w.conn.Close()
		w.conn = nil
	}
	w.connected = false
	w.mu.Unlock()
	return nil
}

func (w *WeCom) OnMessage(handler func(*models.NormalizedMessage)) {
	w.handler = handler
}

func (w *WeCom) SupportsProactiveSend() bool {
	return true
}

func (w *WeCom) HandleEvent(ctx context.Context, target *models.NormalizedMessage, event *models.ChannelEvent) error {
	if target == nil || event == nil {
		return nil
	}
	stateKey := wecomEventKey(target)
	state := w.getOrCreateEventState(stateKey, event.StreamID)

	switch event.Type {
	case models.ChannelEventRunStart:
		return w.sendThinkingFrame(ctx, target, stateKey, state, buildWeComWaitingThinkContent(), false)
	case models.ChannelEventThinking:
		return nil
	case models.ChannelEventPlan, models.ChannelEventPlanUpdate:
		return w.sendImmediateProgress(ctx, target, stateKey, state, formatWeComPlanProgress(event.Plan))
	case models.ChannelEventToolStart:
		return w.sendImmediateProgress(ctx, target, stateKey, state, formatWeComToolProgress(event.ToolName, false))
	case models.ChannelEventToolEnd:
		return w.sendImmediateProgress(ctx, target, stateKey, state, formatWeComToolProgress(event.ToolName, true))
	case models.ChannelEventContentChunk:
		return nil
	case models.ChannelEventFinal, models.ChannelEventError, models.ChannelEventCancelled:
		defer w.clearEventState(stateKey)
		return w.Send(ctx, &models.OutgoingMessage{
			Channel:      target.Channel,
			TargetID:     target.SenderID,
			Content:      event.Content,
			ThreadID:     target.ThreadID,
			ContextToken: target.ContextToken,
			ResponseURL:  target.ResponseURL,
			StreamID:     state.streamID,
			StreamDone:   true,
		})
	default:
		return nil
	}
}

func (w *WeCom) Send(ctx context.Context, msg *models.OutgoingMessage) error {
	w.mu.RLock()
	conn := w.conn
	writer := w.writer
	w.mu.RUnlock()

	if writer != nil {
		if msg.ContextToken != "" || msg.ResponseURL != "" {
			return w.sendStreamResponse(writer, msg)
		}
		return w.sendProactiveMessage(writer, msg)
	}

	if conn == nil {
		return fmt.Errorf("wecom: not connected")
	}

	// Always use WebSocket to send response
	if msg.ContextToken != "" || msg.ResponseURL != "" {
		return w.sendStreamResponse(conn, msg)
	}

	return w.sendProactiveMessage(conn, msg)
}

func wecomEventKey(target *models.NormalizedMessage) string {
	return strings.Join([]string{
		strings.TrimSpace(target.Channel),
		strings.TrimSpace(target.AccountID),
		strings.TrimSpace(target.SenderID),
		strings.TrimSpace(target.ThreadID),
	}, "|")
}

func (w *WeCom) getOrCreateEventState(key, streamID string) *wecomEventState {
	w.eventMu.Lock()
	defer w.eventMu.Unlock()
	state := w.events[key]
	if state == nil {
		state = &wecomEventState{}
		w.events[key] = state
	}
	if strings.TrimSpace(state.streamID) == "" {
		state.streamID = strings.TrimSpace(streamID)
	}
	if strings.TrimSpace(state.streamID) == "" {
		state.streamID = generateStreamID()
	}
	return state
}

func (w *WeCom) markPlaceholderSent(key string) {
	w.eventMu.Lock()
	defer w.eventMu.Unlock()
	if state := w.events[key]; state != nil {
		state.placeholderSent = true
	}
}

func (w *WeCom) clearEventState(key string) {
	w.eventMu.Lock()
	defer w.eventMu.Unlock()
	delete(w.events, key)
}

func (w *WeCom) flushProgressUpdate(ctx context.Context, target *models.NormalizedMessage, stateKey string, state *wecomEventState, closeThink bool) error {
	snapshot, shouldSend := w.thinkingSnapshotForSend(stateKey, state, closeThink)
	if !shouldSend {
		return nil
	}
	return w.sendThinkingFrame(ctx, target, stateKey, state, snapshot, false)
}

func (w *WeCom) appendProgressUpdate(stateKey string, state *wecomEventState, content string) {
	content = normalizeWeComProgressChunk(content)
	if content == "" {
		return
	}
	w.eventMu.Lock()
	defer w.eventMu.Unlock()
	current := w.events[stateKey]
	if current == nil {
		current = state
		w.events[stateKey] = current
	}
	current.progressText += content
}

func (w *WeCom) thinkingSnapshotForSend(stateKey string, state *wecomEventState, closeThink bool) (string, bool) {
	w.eventMu.Lock()
	defer w.eventMu.Unlock()
	current := w.events[stateKey]
	if current == nil {
		current = state
		w.events[stateKey] = current
	}
	return thinkingSnapshotForSendLocked(current, closeThink)
}

func thinkingSnapshotForSendLocked(state *wecomEventState, closeThink bool) (string, bool) {
	if state == nil {
		return "", false
	}
	snapshot := strings.TrimSpace(state.progressText)
	if snapshot == "" {
		return "", false
	}
	snapshot = wrapWeComThink(snapshot, closeThink)
	if snapshot == state.lastSentText {
		return "", false
	}
	state.lastSentText = snapshot
	state.thinkOpen = !closeThink
	return snapshot, true
}

func (w *WeCom) composeFinalWithThinking(stateKey string, state *wecomEventState, finalContent string) (string, bool) {
	w.eventMu.Lock()
	defer w.eventMu.Unlock()
	current := w.events[stateKey]
	if current == nil {
		current = state
	}
	if current == nil {
		return "", false
	}
	thinking := strings.TrimSpace(current.progressText)
	if thinking == "" {
		return "", false
	}
	current.thinkOpen = false
	current.lastSentText = wrapWeComThink(thinking, true)
	return current.lastSentText + "\n" + finalContent, true
}

func (w *WeCom) sendThinkingFrame(ctx context.Context, target *models.NormalizedMessage, stateKey string, state *wecomEventState, content string, finish bool) error {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	w.setThinkOpen(stateKey, state, !strings.Contains(content, "</think>"))
	if err := w.Send(ctx, &models.OutgoingMessage{
		Channel:      target.Channel,
		TargetID:     target.SenderID,
		Content:      content,
		ThreadID:     target.ThreadID,
		ContextToken: target.ContextToken,
		ResponseURL:  target.ResponseURL,
		StreamID:     state.streamID,
		StreamDone:   finish,
	}); err != nil {
		return err
	}
	w.markPlaceholderSent(stateKey)
	return nil
}

func (w *WeCom) setThinkOpen(stateKey string, state *wecomEventState, open bool) {
	w.eventMu.Lock()
	defer w.eventMu.Unlock()
	current := w.events[stateKey]
	if current == nil {
		current = state
		w.events[stateKey] = current
	}
	current.thinkOpen = open
}

func (w *WeCom) sendImmediateProgress(ctx context.Context, target *models.NormalizedMessage, stateKey string, state *wecomEventState, content string) error {
	content = wrapWeComThink(normalizeWeComProgressChunk(content), true)
	if content == "" {
		return nil
	}
	if err := w.Send(ctx, &models.OutgoingMessage{
		Channel:      target.Channel,
		TargetID:     target.SenderID,
		Content:      content,
		ThreadID:     target.ThreadID,
		ContextToken: target.ContextToken,
		ResponseURL:  target.ResponseURL,
		StreamID:     state.streamID,
		StreamDone:   false,
	}); err != nil {
		return err
	}
	w.markPlaceholderSent(stateKey)
	return nil
}

func normalizeWeComProgressChunk(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return content
}

func buildWeComWaitingThinkContent() string {
	return "<think>thinking...</think>"
}

func wrapWeComThink(content string, closeThink bool) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	if closeThink {
		return "<think>" + content + "</think>"
	}
	return "<think>" + content
}

func formatWeComPlanProgress(plan *models.TaskPlan) string {
	if plan == nil {
		return ""
	}
	var lines []string
	if intent := strings.TrimSpace(plan.Intent); intent != "" {
		lines = append(lines, fmt.Sprintf("Plan: %s", intent))
	}
	for i, task := range plan.SubTasks {
		name := strings.TrimSpace(task.Name)
		if name == "" {
			continue
		}
		line := fmt.Sprintf("%d. %s", i+1, name)
		if status := strings.TrimSpace(task.Status); status != "" {
			line += fmt.Sprintf(" [%s]", status)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func formatWeComToolProgress(toolName string, done bool) string {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return ""
	}
	if done {
		return fmt.Sprintf("Tool completed: %s", toolName)
	}
	return fmt.Sprintf("Using tool: %s", toolName)
}

func (w *WeCom) sendStreamResponse(conn wecomJSONWriter, msg *models.OutgoingMessage) error {
	if len(msg.Media) > 0 {
		return w.sendReplyMediaMessages(context.Background(), conn, msg)
	}
	streamID := msg.StreamID
	if streamID == "" {
		streamID = generateStreamID()
	}

	logger.Info("WeCom sending response", logger.Fields{
		"channel_id":  w.channelID,
		"req_id":      msg.ContextToken,
		"stream_id":   streamID,
		"stream_done": msg.StreamDone,
		"content_len": len(msg.Content),
	})

	respCmd := map[string]interface{}{
		"cmd": "aibot_respond_msg",
		"headers": map[string]interface{}{
			"req_id": msg.ContextToken,
		},
		"body": map[string]interface{}{
			"msgtype": "stream",
			"stream": map[string]interface{}{
				"id":      streamID,
				"finish":  msg.StreamDone,
				"content": msg.Content,
			},
		},
	}

	logger.Debug("WeCom response payload", logger.Fields{
		"channel_id": w.channelID,
		"payload":    string(mustMarshal(respCmd)),
	})

	err := conn.WriteJSON(respCmd)
	if err != nil {
		logger.Error("WeCom send response failed", logger.Fields{
			"channel_id": w.channelID,
			"error":      err.Error(),
		})
		return err
	}

	logger.Info("WeCom response sent", logger.Fields{
		"channel_id": w.channelID,
		"req_id":     msg.ContextToken,
		"stream_id":  streamID,
	})
	return nil
}

func (w *WeCom) sendReplyMediaMessages(ctx context.Context, conn wecomJSONWriter, msg *models.OutgoingMessage) error {
	for _, media := range msg.Media {
		payload, err := w.buildOutgoingMediaPayload(ctx, conn, media)
		if err != nil {
			return err
		}
		respCmd := map[string]interface{}{
			"cmd": "aibot_respond_msg",
			"headers": map[string]interface{}{
				"req_id": msg.ContextToken,
			},
			"body": map[string]interface{}{
				"msgtype":       payload.MsgType,
				payload.MsgType: payload.Content,
			},
		}
		if err := conn.WriteJSON(respCmd); err != nil {
			return err
		}
	}
	if strings.TrimSpace(msg.Content) == "" {
		return nil
	}
	return w.sendReplyText(conn, msg)
}

func (w *WeCom) sendReplyText(conn wecomJSONWriter, msg *models.OutgoingMessage) error {
	respCmd := map[string]interface{}{
		"cmd": "aibot_respond_msg",
		"headers": map[string]interface{}{
			"req_id": msg.ContextToken,
		},
		"body": map[string]interface{}{
			"msgtype": "text",
			"text": map[string]interface{}{
				"content": msg.Content,
			},
		},
	}
	return conn.WriteJSON(respCmd)
}

func (w *WeCom) sendProactiveMessage(conn wecomJSONWriter, msg *models.OutgoingMessage) error {
	if len(msg.Media) > 0 {
		return w.sendProactiveMediaMessages(context.Background(), conn, msg)
	}
	body := map[string]interface{}{
		"msgtype": "text",
		"text": map[string]interface{}{
			"content": msg.Content,
		},
	}
	if msg.ThreadID != "" {
		body["chat_id"] = msg.ThreadID
	} else if msg.TargetID != "" {
		body["user_id"] = msg.TargetID
	}

	respCmd := map[string]interface{}{
		"cmd":  "aibot_send_msg",
		"body": body,
	}

	logger.Info("WeCom sending proactive message", logger.Fields{
		"channel_id":  w.channelID,
		"target_id":   msg.TargetID,
		"chat_id":     msg.ThreadID,
		"content_len": len(msg.Content),
	})

	return conn.WriteJSON(respCmd)
}

func (w *WeCom) sendProactiveMediaMessages(ctx context.Context, conn wecomJSONWriter, msg *models.OutgoingMessage) error {
	chatID := strings.TrimSpace(msg.ThreadID)
	if chatID == "" {
		chatID = strings.TrimSpace(msg.TargetID)
	}
	if chatID == "" {
		return fmt.Errorf("wecom: chat target is required for media send")
	}
	logger.Info("WeCom sending proactive media batch", logger.Fields{
		"channel_id":  w.channelID,
		"target_id":   msg.TargetID,
		"chat_id":     chatID,
		"media_count": len(msg.Media),
		"content_len": len(strings.TrimSpace(msg.Content)),
	})
	for _, media := range msg.Media {
		payload, err := w.buildOutgoingMediaPayload(ctx, conn, media)
		if err != nil {
			return err
		}
		body := map[string]interface{}{
			"chat_id": chatID,
			"msgtype": payload.MsgType,
		}
		if payload.Content != nil {
			body[payload.MsgType] = payload.Content
		}
		respCmd := map[string]interface{}{
			"cmd":  "aibot_send_msg",
			"body": body,
		}
		logger.Info("WeCom sending proactive media frame", logger.Fields{
			"channel_id":   w.channelID,
			"chat_id":      chatID,
			"msgtype":      payload.MsgType,
			"media_name":   strings.TrimSpace(media.Name),
			"media_path":   strings.TrimSpace(media.Path),
			"mime_type":    strings.TrimSpace(media.MimeType),
			"content_keys": getMapKeys(payload.Content),
		})
		if err := conn.WriteJSON(respCmd); err != nil {
			return err
		}
	}
	if strings.TrimSpace(msg.Content) == "" {
		return nil
	}
	return w.sendProactiveMessageText(conn, msg)
}

func (w *WeCom) sendProactiveMessageText(conn wecomJSONWriter, msg *models.OutgoingMessage) error {
	body := map[string]interface{}{
		"msgtype": "text",
		"text": map[string]interface{}{
			"content": msg.Content,
		},
	}
	if msg.ThreadID != "" {
		body["chat_id"] = msg.ThreadID
	} else if msg.TargetID != "" {
		body["user_id"] = msg.TargetID
	}
	respCmd := map[string]interface{}{
		"cmd":  "aibot_send_msg",
		"body": body,
	}
	return conn.WriteJSON(respCmd)
}

type wecomOutgoingMediaPayload struct {
	MsgType string
	Content map[string]interface{}
}

func (w *WeCom) buildOutgoingMediaPayload(ctx context.Context, conn wecomJSONWriter, media models.OutgoingMedia) (*wecomOutgoingMediaPayload, error) {
	mediaType := strings.ToLower(strings.TrimSpace(media.Type))
	if mediaType == "audio" {
		mediaType = "voice"
	}
	if mediaType != "image" && mediaType != "file" && mediaType != "voice" && mediaType != "video" {
		return nil, fmt.Errorf("wecom: unsupported media type %q", media.Type)
	}
	logger.Info("WeCom building media payload", logger.Fields{
		"channel_id": w.channelID,
		"media_type": mediaType,
		"media_name": strings.TrimSpace(media.Name),
		"media_path": strings.TrimSpace(media.Path),
		"has_url":    strings.TrimSpace(media.URL) != "",
		"has_data":   strings.TrimSpace(media.Data) != "",
		"mime_type":  strings.TrimSpace(media.MimeType),
	})
	mediaID, err := w.resolveWeComMediaID(ctx, conn, mediaType, media)
	if err != nil {
		return nil, err
	}
	content := map[string]interface{}{"media_id": mediaID}
	if mediaType == "video" {
		if title := strings.TrimSpace(media.Name); title != "" {
			content["title"] = title
		}
		if desc := strings.TrimSpace(media.MimeType); desc != "" {
			content["description"] = desc
		}
	}
	return &wecomOutgoingMediaPayload{MsgType: mediaType, Content: content}, nil
}

func (w *WeCom) resolveWeComMediaID(ctx context.Context, conn wecomJSONWriter, mediaType string, media models.OutgoingMedia) (string, error) {
	if existing := strings.TrimSpace(media.URL); existing != "" && !strings.HasPrefix(existing, "/media/") {
		logger.Info("WeCom using existing media id", logger.Fields{
			"channel_id": w.channelID,
			"media_type": mediaType,
			"media_name": strings.TrimSpace(media.Name),
			"media_id":   existing,
		})
		return existing, nil
	}
	fileName, data, err := resolveWeComMediaBytes(media)
	if err != nil {
		return "", err
	}
	logger.Info("WeCom resolved media bytes", logger.Fields{
		"channel_id": w.channelID,
		"media_type": mediaType,
		"file_name":  fileName,
		"file_size":  len(data),
	})
	return w.uploadMedia(ctx, conn, mediaType, fileName, data)
}

func resolveWeComMediaBytes(media models.OutgoingMedia) (string, []byte, error) {
	if strings.TrimSpace(media.Path) != "" {
		data, err := os.ReadFile(media.Path)
		if err != nil {
			return "", nil, fmt.Errorf("read media file %q: %w", media.Path, err)
		}
		name := strings.TrimSpace(media.Name)
		if name == "" {
			name = filepath.Base(media.Path)
		}
		return name, data, nil
	}
	if strings.TrimSpace(media.Data) != "" {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(media.Data))
		if err != nil {
			return "", nil, fmt.Errorf("decode media data as base64: %w", err)
		}
		name := strings.TrimSpace(media.Name)
		if name == "" {
			name = "media"
		}
		return name, decoded, nil
	}
	return "", nil, fmt.Errorf("wecom media %q requires either media_id in url or path/data", media.Type)
}

func (w *WeCom) uploadMedia(ctx context.Context, conn wecomJSONWriter, mediaType, fileName string, data []byte) (string, error) {
	const chunkSize = 512 * 1024
	chunkCount := (len(data) + chunkSize - 1) / chunkSize
	if chunkCount == 0 {
		chunkCount = 1
	}
	md5sum := fmt.Sprintf("%x", md5.Sum(data))
	logger.Info("WeCom upload media start", logger.Fields{
		"channel_id":  w.channelID,
		"media_type":  mediaType,
		"file_name":   fileName,
		"file_size":   len(data),
		"chunk_count": chunkCount,
		"chunk_size":  chunkSize,
		"content_md5": md5sum,
	})
	initResp, err := w.sendCommandAwait(ctx, conn, "aibot_upload_media_init", map[string]interface{}{
		"type":         mediaType,
		"filename":     fileName,
		"total_size":   len(data),
		"total_chunks": chunkCount,
		"md5":          md5sum,
	})
	if err != nil {
		return "", err
	}
	uploadBody, _ := initResp["body"].(map[string]interface{})
	uploadID, _ := uploadBody["upload_id"].(string)
	if strings.TrimSpace(uploadID) == "" {
		return "", fmt.Errorf("wecom upload init missing upload_id")
	}
	logger.Info("WeCom upload media init complete", logger.Fields{
		"channel_id": w.channelID,
		"media_type": mediaType,
		"file_name":  fileName,
		"upload_id":  uploadID,
	})
	for i := 0; i < chunkCount; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(data) {
			end = len(data)
		}
		chunk := data[start:end]
		logger.Debug("WeCom upload media chunk", logger.Fields{
			"channel_id":  w.channelID,
			"upload_id":   uploadID,
			"chunk_index": i,
			"chunk_size":  len(chunk),
		})
		if _, err := w.sendCommandAwait(ctx, conn, "aibot_upload_media_chunk", map[string]interface{}{
			"upload_id":   uploadID,
			"chunk_index": i,
			"base64_data": base64.StdEncoding.EncodeToString(chunk),
		}); err != nil {
			return "", err
		}
	}
	finishResp, err := w.sendCommandAwait(ctx, conn, "aibot_upload_media_finish", map[string]interface{}{
		"upload_id": uploadID,
	})
	if err != nil {
		return "", err
	}
	finishBody, _ := finishResp["body"].(map[string]interface{})
	mediaID, _ := finishBody["media_id"].(string)
	if strings.TrimSpace(mediaID) == "" {
		return "", fmt.Errorf("wecom upload finish missing media_id")
	}
	logger.Info("WeCom upload media finished", logger.Fields{
		"channel_id": w.channelID,
		"media_type": mediaType,
		"file_name":  fileName,
		"media_id":   mediaID,
	})
	return mediaID, nil
}

func (w *WeCom) sendCommandAwait(ctx context.Context, conn wecomJSONWriter, cmd string, body map[string]interface{}) (map[string]interface{}, error) {
	reqID := fmt.Sprintf("evoduck-wecom-%d", time.Now().UnixNano())
	respCh := make(chan map[string]interface{}, 1)
	w.pendingMu.Lock()
	w.pending[reqID] = respCh
	w.pendingMu.Unlock()
	defer func() {
		w.pendingMu.Lock()
		delete(w.pending, reqID)
		w.pendingMu.Unlock()
	}()
	frame := map[string]interface{}{
		"cmd": cmd,
		"headers": map[string]interface{}{
			"req_id": reqID,
		},
		"body": body,
	}
	logBody := body
	if cmd == "aibot_upload_media_chunk" {
		logBody = map[string]interface{}{
			"upload_id":   body["upload_id"],
			"chunk_index": body["chunk_index"],
			"base64_len":  len(fmt.Sprintf("%v", body["base64_data"])),
		}
	}
	logger.Debug("WeCom command send", logger.Fields{
		"channel_id": w.channelID,
		"cmd":        cmd,
		"req_id":     reqID,
		"body":       logBody,
	})
	if err := conn.WriteJSON(frame); err != nil {
		return nil, err
	}
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	select {
	case resp := <-respCh:
		logger.Debug("WeCom command response", logger.Fields{
			"channel_id": w.channelID,
			"cmd":        cmd,
			"req_id":     reqID,
			"resp":       resp,
		})
		if errCode, ok := resp["errcode"].(float64); ok && int(errCode) != 0 {
			return nil, fmt.Errorf("wecom command %s failed: errcode=%d errmsg=%v", cmd, int(errCode), resp["errmsg"])
		}
		if cmdName, _ := resp["cmd"].(string); cmdName == "error" {
			return nil, fmt.Errorf("wecom command %s returned error: %v", cmd, resp)
		}
		return resp, nil
	case <-waitCtx.Done():
		return nil, fmt.Errorf("wecom command %s timed out: %w", cmd, waitCtx.Err())
	}
}

func extractWeComReplyTo(msg map[string]interface{}, headers, body map[string]interface{}) string {
	if headers != nil {
		if replyTo, _ := headers["reply_to"].(string); strings.TrimSpace(replyTo) != "" {
			return replyTo
		}
		if reqID, _ := headers["req_id"].(string); strings.TrimSpace(reqID) != "" {
			return reqID
		}
	}
	if body != nil {
		if replyTo, _ := body["reply_to"].(string); strings.TrimSpace(replyTo) != "" {
			return replyTo
		}
		if reqID, _ := body["req_id"].(string); strings.TrimSpace(reqID) != "" {
			return reqID
		}
	}
	if replyTo, _ := msg["reply_to"].(string); strings.TrimSpace(replyTo) != "" {
		return replyTo
	}
	return ""
}

func (w *WeCom) resolvePending(replyTo string, msg map[string]interface{}) {
	w.pendingMu.Lock()
	respCh, ok := w.pending[replyTo]
	w.pendingMu.Unlock()
	if !ok {
		return
	}
	select {
	case respCh <- msg:
	default:
	}
}

func (w *WeCom) Broadcast(ctx context.Context, content string, excludeTarget string) error {
	return nil
}

func (w *WeCom) heartbeatLoop() {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	logger.Debug("WeCom heartbeat loop started", logger.Fields{
		"channel_id":   w.channelID,
		"interval_sec": heartbeatInterval.Seconds(),
	})

	for {
		select {
		case <-w.ctx.Done():
			logger.Debug("WeCom heartbeat loop exiting", logger.Fields{
				"channel_id": w.channelID,
			})
			return
		case <-ticker.C:
			w.mu.RLock()
			conn := w.conn
			w.mu.RUnlock()

			if conn == nil {
				logger.Debug("WeCom heartbeat skipped (no connection)", logger.Fields{
					"channel_id": w.channelID,
				})
				continue
			}

			logger.Debug("WeCom sending heartbeat", logger.Fields{
				"channel_id": w.channelID,
			})

			if err := conn.WriteJSON(map[string]interface{}{"cmd": "heartbeat"}); err != nil {
				logger.Error("WeCom heartbeat failed", logger.Fields{
					"error":      err.Error(),
					"channel_id": w.channelID,
				})
			}
		}
	}
}

func (w *WeCom) handleDisconnect() {
	w.mu.Lock()
	if w.conn != nil {
		w.conn.Close()
		w.conn = nil
	}
	w.connected = false
	w.mu.Unlock()
}

func (w *WeCom) reconnect() {
	delay := reconnectDelay

	for {
		select {
		case <-w.ctx.Done():
			return
		default:
		}

		logger.Info("WeCom reconnecting", logger.Fields{
			"channel_id": w.channelID,
			"delay":      delay.String(),
		})

		time.Sleep(delay)

		if err := w.connectAndSubscribe(); err != nil {
			logger.Error("WeCom reconnect failed", logger.Fields{
				"error": err.Error(),
			})
			delay = delay * 2
			if delay > maxReconnectDelay {
				delay = maxReconnectDelay
			}
			continue
		}

		logger.Info("WeCom reconnected", logger.Fields{
			"channel_id": w.channelID,
		})
		return
	}
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func generateStreamID() string {
	return fmt.Sprintf("stream-%d-%d", time.Now().UnixNano(), wecomStreamCounter.Add(1))
}

func getMapKeys(m map[string]interface{}) []string {
	if m == nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func mustMarshal(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

var _ channels.Bridge = (*WeCom)(nil)
